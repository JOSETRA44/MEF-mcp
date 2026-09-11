// Package httpx is the one HTTP client every module shares.
//
// It owns the cross-cutting concerns so modules only describe requests:
// connection reuse, disk caching, retries with backoff, per-host politeness
// (MEF runs these services on modest government infrastructure) and charset
// repair. Concurrent identical requests collapse into one via singleflight.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/JOSETRA44/mef-mcp/internal/cache"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/textenc"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

// CacheMode mirrors the --refresh / --no-cache flags.
type CacheMode int

const (
	CacheNormal  CacheMode = iota // read and write
	CacheRefresh                  // skip read, write the fresh response
	CacheOff                      // neither read nor write
)

// Client implements core.HTTPDoer.
type Client struct {
	HTTP       *http.Client
	Cache      *cache.Disk // nil disables caching entirely
	Mode       CacheMode
	TTL        func(key string) time.Duration
	MaxRetries int
	// MinInterval spaces request starts to the same host.
	MinInterval time.Duration
	UserAgent   string

	group   singleflight.Group
	mu      sync.Mutex
	hosts   map[string]*hostGate
	sleepFn func(context.Context, time.Duration) error
}

type hostGate struct {
	sem  chan struct{}
	mu   sync.Mutex
	next time.Time
}

// New returns a client with production defaults.
func New(c *cache.Disk, mode CacheMode) *Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 150 * time.Second,
	}
	return &Client{
		HTTP:        &http.Client{Transport: tr},
		Cache:       c,
		Mode:        mode,
		TTL:         cache.TTLFor,
		MaxRetries:  2,
		MinInterval: 120 * time.Millisecond,
		UserAgent:   "mef-mcp/" + version.Get() + " (+https://github.com/JOSETRA44/mef-mcp)",
		hosts:       map[string]*hostGate{},
	}
}

func (c *Client) gate(host string) *hostGate {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hosts == nil {
		c.hosts = map[string]*hostGate{}
	}
	g, ok := c.hosts[host]
	if !ok {
		g = &hostGate{sem: make(chan struct{}, 4)}
		c.hosts[host] = g
	}
	return g
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.sleepFn != nil {
		return c.sleepFn(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func encodeBody(req core.Request) ([]byte, string, error) {
	switch {
	case req.JSON != nil:
		b, err := json.Marshal(req.JSON)
		return b, "application/json", err
	case req.Form != nil:
		return []byte(req.Form.Encode()), "application/x-www-form-urlencoded", nil
	}
	return nil, "", nil
}

// fullURL appends the query verbatim, preserving parameter order when the
// caller encoded it through OrderedQuery (see amigable).
func fullURL(req core.Request) string {
	if len(req.Query) == 0 {
		return req.URL
	}
	return req.URL + "?" + req.Query.Encode()
}

// Do performs a request, serving from and populating the disk cache.
func (c *Client) Do(ctx context.Context, req core.Request) (*core.Response, error) {
	body, ctype, err := encodeBody(req)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "encoding request body: %v", err)
	}
	target := req.URL
	if raw, ok := rawQuery(req); ok {
		target = req.URL + "?" + raw
	} else {
		target = fullURL(req)
	}
	key := cache.Key(req.Method, target, nil, body)

	if c.Cache != nil && c.Mode == CacheNormal {
		if e := c.Cache.Get(key); e != nil {
			return &core.Response{Status: e.Status, Body: e.Body, Cached: true, URL: target}, nil
		}
	}

	v, err, _ := c.group.Do(key, func() (any, error) {
		return c.fetch(ctx, req, target, body, ctype)
	})
	if err != nil {
		return nil, err
	}
	resp := v.(*core.Response)
	if c.Cache != nil && c.Mode != CacheOff && c.TTL != nil {
		_ = c.Cache.Put(key, resp.Status, target, resp.Body, c.TTL(req.TTLKey))
	}
	out := *resp
	return &out, nil
}

// DoFresh performs a request bypassing the cache entirely (read and write);
// `mef doctor` uses it to measure the live service.
func (c *Client) DoFresh(ctx context.Context, req core.Request) (*core.Response, error) {
	body, ctype, err := encodeBody(req)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "encoding request body: %v", err)
	}
	target := fullURL(req)
	if raw, ok := rawQuery(req); ok {
		target = req.URL + "?" + raw
	}
	return c.fetch(ctx, req, target, body, ctype)
}

// rawQuery lets a module pass a pre-encoded, order-preserving query string
// under the reserved key "_raw". url.Values.Encode sorts keys, which would
// scramble Consulta Amigable's drill-down path.
func rawQuery(req core.Request) (string, bool) {
	if raw := req.Query.Get("_raw"); raw != "" && len(req.Query) == 1 {
		return raw, true
	}
	return "", false
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func (c *Client) fetch(ctx context.Context, req core.Request, target string, body []byte, ctype string) (*core.Response, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "bad URL %q", target)
	}
	timeout := 45 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	g := c.gate(u.Host)
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(400*(1<<(attempt-1)))*time.Millisecond + time.Duration(rand.IntN(200))*time.Millisecond
			if err := c.sleep(ctx, backoff); err != nil {
				return nil, core.AsError(err)
			}
		}
		resp, err := c.once(ctx, g, req.Method, target, body, ctype, timeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil, core.AsError(ctx.Err())
			}
			lastErr = err
			continue
		}
		if retryable(resp.StatusCode) {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			lastErr = &statusError{status: resp.StatusCode}
			if wait > 0 && wait <= 5*time.Second {
				_ = c.sleep(ctx, wait)
			}
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if req.Charset == "mixed" {
			raw = []byte(textenc.DecodeMixed(raw))
		}
		return &core.Response{Status: resp.StatusCode, Body: raw, URL: target}, nil
	}
	var se *statusError
	if errors.As(lastErr, &se) {
		if se.status == http.StatusTooManyRequests || se.status == http.StatusServiceUnavailable {
			return nil, core.Wrap(core.CodeRateLimit, lastErr, "", "%s answered HTTP %d after %d attempts", u.Host, se.status, c.MaxRetries+1)
		}
		return nil, core.Wrap(core.CodeAPI, lastErr, "", "%s answered HTTP %d after %d attempts", u.Host, se.status, c.MaxRetries+1)
	}
	if lastErr != nil && (errors.Is(lastErr, context.DeadlineExceeded) || isTimeout(lastErr)) {
		return nil, core.Wrap(core.CodeNetwork, lastErr, "The MEF source is slow; some dimensions (meta, finalidad) take 30s+. Retry, or narrow the query.", "timed out after %s waiting for %s", timeout, u.Host)
	}
	return nil, core.Wrap(core.CodeNetwork, lastErr, "", "could not reach %s: %v", u.Host, lastErr)
}

type statusError struct{ status int }

func (e *statusError) Error() string { return "HTTP " + strconv.Itoa(e.status) }

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func (c *Client) once(ctx context.Context, g *hostGate, method, target string, body []byte, ctype string, timeout time.Duration) (*http.Response, error) {
	select {
	case g.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-g.sem }

	g.mu.Lock()
	wait := time.Until(g.next)
	if wait < 0 {
		wait = 0
	}
	g.next = time.Now().Add(wait + c.MinInterval)
	g.mu.Unlock()
	if wait > 0 {
		if err := c.sleep(ctx, wait); err != nil {
			release()
			return nil, err
		}
	}

	rctx, cancel := context.WithTimeout(ctx, timeout)
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	if method == "" {
		method = http.MethodGet
	}
	hreq, err := http.NewRequestWithContext(rctx, method, target, rd)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	hreq.Header.Set("User-Agent", c.UserAgent)
	hreq.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.5")
	if ctype != "" {
		hreq.Header.Set("Content-Type", ctype)
	}
	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	resp.Body = &releasingBody{ReadCloser: resp.Body, done: func() { cancel(); release() }}
	return resp, nil
}

type releasingBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.done)
	return err
}

// Stream performs an uncached request for large downloads.
func (c *Client) Stream(ctx context.Context, req core.Request) (core.StreamBody, error) {
	body, ctype, err := encodeBody(req)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "encoding request body: %v", err)
	}
	target := fullURL(req)
	u, err := url.Parse(target)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "bad URL %q", target)
	}
	timeout := 30 * time.Minute
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	resp, err := c.once(ctx, c.gate(u.Host), req.Method, target, body, ctype, timeout)
	if err != nil {
		return nil, core.AsError(err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, core.NotFoundf("", "%s answered 404 for %s", u.Host, target)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, core.NewError(core.CodeAPI, "", "%s answered HTTP %d", u.Host, resp.StatusCode)
	}
	return &streamBody{ReadCloser: resp.Body, size: resp.ContentLength}, nil
}

type streamBody struct {
	io.ReadCloser
	size int64
}

func (s *streamBody) Size() int64 { return s.size }

// OrderedQuery encodes key/value pairs without reordering them. Wrap the
// result as url.Values{"_raw": {q}} in a core.Request.
func OrderedQuery(pairs [][2]string) string {
	var sb strings.Builder
	for i, kv := range pairs {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(kv[0]))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(kv[1]))
	}
	return sb.String()
}

// Describe is used by doctor output.
func Describe(r *core.Response) string {
	return fmt.Sprintf("HTTP %d, %d bytes, cached=%v", r.Status, len(r.Body), r.Cached)
}
