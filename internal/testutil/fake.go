// Package testutil provides a fake upstream so every test runs offline.
package testutil

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/app"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/store"
)

// Route answers requests whose URL (with query) contains Match and, when set,
// whose form/JSON body contains Body.
type Route struct {
	Match  string
	Body   string
	Status int
	Reply  func(req core.Request, call int) ([]byte, error)
}

// Fake is a core.HTTPDoer driven by routes. It records every request.
type Fake struct {
	mu     sync.Mutex
	Routes []Route
	Calls  []core.Request
	Cached bool // report every response as cached
}

// Static returns a reply function serving fixed bytes.
func Static(b []byte) func(core.Request, int) ([]byte, error) {
	return func(core.Request, int) ([]byte, error) { return b, nil }
}

// Fixture reads testdata relative to the calling test's package directory.
func Fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// Target renders the request URL the way the real client does.
func Target(req core.Request) string {
	if raw := req.Query.Get("_raw"); raw != "" && len(req.Query) == 1 {
		return req.URL + "?" + raw
	}
	if len(req.Query) > 0 {
		return req.URL + "?" + req.Query.Encode()
	}
	return req.URL
}

func bodyOf(req core.Request) string {
	switch {
	case req.Form != nil:
		return req.Form.Encode()
	case req.JSON != nil:
		return fmt.Sprintf("%v", req.JSON)
	}
	return ""
}

func (f *Fake) Do(ctx context.Context, req core.Request) (*core.Response, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, req)
	n := len(f.Calls)
	f.mu.Unlock()
	target := Target(req)
	for _, r := range f.Routes {
		if !strings.Contains(target, r.Match) {
			continue
		}
		if r.Body != "" && !strings.Contains(bodyOf(req), r.Body) {
			continue
		}
		b, err := r.Reply(req, n)
		if err != nil {
			return nil, err
		}
		status := r.Status
		if status == 0 {
			status = 200
		}
		return &core.Response{Status: status, Body: b, Cached: f.Cached, URL: target}, nil
	}
	return nil, core.NewError(core.CodeNetwork, "", "fake: no route for %s %s body=%s", req.Method, target, bodyOf(req))
}

func (f *Fake) Stream(ctx context.Context, req core.Request) (core.StreamBody, error) {
	resp, err := f.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return &stream{Reader: strings.NewReader(string(resp.Body)), n: int64(len(resp.Body))}, nil
}

type stream struct {
	io.Reader
	n int64
}

func (s *stream) Close() error { return nil }
func (s *stream) Size() int64  { return s.n }

// CallCount returns the number of requests seen.
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// Env builds a command environment over the fake and a temp database.
func Env(t testing.TB, f *Fake) *core.Env {
	t.Helper()
	return &core.Env{
		HTTP:  f,
		Store: store.New(filepath.Join(t.TempDir(), "mef.db")),
		Now:   func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
	}
}

// Shared builds app.Shared over the fake, for CLI and MCP tests.
func Shared(t testing.TB, f *Fake) *app.Shared {
	t.Helper()
	return &app.Shared{HTTP: f, Store: store.New(filepath.Join(t.TempDir(), "mef.db"))}
}

// Sink is an in-memory core.Sink.
type Sink struct {
	Rows    []core.Row
	Columns []string
	Max     int // 0 = unlimited
}

func (s *Sink) SetColumns(c []string) { s.Columns = c }

func (s *Sink) Emit(rows ...core.Row) error {
	for _, r := range rows {
		if s.Max > 0 && len(s.Rows) >= s.Max {
			return nil
		}
		s.Rows = append(s.Rows, r)
	}
	return nil
}

func (s *Sink) Wanted() int {
	if s.Max <= 0 {
		return -1
	}
	return s.Max - len(s.Rows)
}

// Input builds a validated input or fails the test.
func Input(t testing.TB, c *core.Command, raw map[string]any) core.Input {
	t.Helper()
	in, err := core.NewInput(c.Params, raw, nil)
	if err != nil {
		t.Fatalf("input for %s: %v", c.Name(), err)
	}
	return in
}

// Query parses a URL's query for assertions.
func Query(t testing.TB, target string) url.Values {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// RepoRoot locates the module root from this file's location.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
