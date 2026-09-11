package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/cache"
	"github.com/JOSETRA44/mef-mcp/internal/core"
)

func client(t *testing.T, mode CacheMode) *Client {
	t.Helper()
	c := New(cache.New(t.TempDir()), mode)
	c.MinInterval = 0
	c.TTL = func(string) time.Duration { return time.Hour }
	c.sleepFn = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestCacheModes(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	req := core.Request{Method: "GET", URL: srv.URL, Query: url.Values{"a": {"1"}}}
	ctx := context.Background()

	c := client(t, CacheNormal)
	r1, _ := c.Do(ctx, req)
	r2, _ := c.Do(ctx, req)
	if r1.Cached || !r2.Cached || hits.Load() != 1 {
		t.Fatalf("normal: cached=%v,%v hits=%d", r1.Cached, r2.Cached, hits.Load())
	}

	// --refresh ignores the entry but rewrites it.
	c.Mode = CacheRefresh
	r3, _ := c.Do(ctx, req)
	if r3.Cached || hits.Load() != 2 {
		t.Fatalf("refresh must refetch: cached=%v hits=%d", r3.Cached, hits.Load())
	}

	// --no-cache neither reads nor writes.
	off := client(t, CacheOff)
	off.Cache = cache.New(t.TempDir())
	off.Do(ctx, req)
	off.Mode = CacheNormal
	r4, _ := off.Do(ctx, req)
	if r4.Cached {
		t.Fatal("no-cache must not have written an entry")
	}
}

func TestRetriesTransientThenSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	resp, err := client(t, CacheOff).Do(context.Background(), core.Request{Method: "GET", URL: srv.URL})
	if err != nil || string(resp.Body) != "ok" || n.Load() != 3 {
		t.Fatalf("resp=%v err=%v attempts=%d", resp, err, n.Load())
	}
}

func TestPersistentThrottleIsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := client(t, CacheOff).Do(context.Background(), core.Request{Method: "GET", URL: srv.URL})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeRateLimit || e.ExitCode() != 5 {
		t.Fatalf("got %v", err)
	}
}

func TestUnreachableIsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	_, err := client(t, CacheOff).Do(context.Background(), core.Request{Method: "GET", URL: u})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeNetwork || e.ExitCode() != 6 {
		t.Fatalf("got %v", err)
	}
}

func TestNotFoundStatusIsReturnedNotCached(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := client(t, CacheNormal)
	for i := 0; i < 2; i++ {
		resp, err := c.Do(context.Background(), core.Request{Method: "GET", URL: srv.URL})
		if err != nil || resp.Status != 404 || resp.Cached {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
	}
	if n.Load() != 2 {
		t.Fatal("a 404 must not be served from cache")
	}
}

func TestMixedCharsetIsRepaired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Categor\xc3\xada / Funci\xf3n"))
	}))
	defer srv.Close()
	resp, err := client(t, CacheOff).Do(context.Background(), core.Request{Method: "GET", URL: srv.URL, Charset: "mixed"})
	if err != nil || string(resp.Body) != "Categoría / Función" {
		t.Fatalf("got %q %v", resp.Body, err)
	}
}

func TestOrderedQueryKeepsDrillDownOrder(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
	}))
	defer srv.Close()
	raw := OrderedQuery([][2]string{{"_uhc", "yes"}, {"0", ""}, {"2", "01"}, {"1", ""}, {"y", "2026"}})
	client(t, CacheOff).Do(context.Background(), core.Request{Method: "GET", URL: srv.URL, Query: url.Values{"_raw": {raw}}})
	if got != "_uhc=yes&0=&2=01&1=&y=2026" {
		t.Fatalf("query reordered: %q", got)
	}
}

func TestSingleflightCollapsesConcurrentCalls(t *testing.T) {
	var n atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		<-release
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	c := client(t, CacheOff)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Do(context.Background(), core.Request{Method: "GET", URL: srv.URL})
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if n.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", n.Load())
	}
}

func TestPostFormAndJSONBodies(t *testing.T) {
	var ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ctype, body = r.Header.Get("Content-Type"), string(b)
	}))
	defer srv.Close()
	c := client(t, CacheOff)
	c.Do(context.Background(), core.Request{Method: "POST", URL: srv.URL, Form: url.Values{"id": {"2222116"}}})
	if ctype != "application/x-www-form-urlencoded" || body != "id=2222116" {
		t.Fatalf("form: %s %s", ctype, body)
	}
	c.Do(context.Background(), core.Request{Method: "POST", URL: srv.URL, JSON: map[string]string{"dataset": "x"}})
	if ctype != "application/json" || body != `{"dataset":"x"}` {
		t.Fatalf("json: %s %s", ctype, body)
	}
}
