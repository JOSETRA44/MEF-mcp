// Package app wires runtime dependencies shared by the CLI and the MCP server.
package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/cache"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
	"github.com/JOSETRA44/mef-mcp/internal/paths"
	"github.com/JOSETRA44/mef-mcp/internal/render"
	"github.com/JOSETRA44/mef-mcp/internal/store"
)

// Tracker counts upstream responses so the envelope's `cached` is truthful:
// true only when a network command was answered entirely from the disk cache.
type Tracker struct {
	Inner  core.HTTPDoer
	calls  atomic.Int64
	cached atomic.Int64
}

func (t *Tracker) Do(ctx context.Context, req core.Request) (*core.Response, error) {
	resp, err := t.Inner.Do(ctx, req)
	if err == nil {
		t.calls.Add(1)
		if resp.Cached {
			t.cached.Add(1)
		}
	}
	return resp, err
}

func (t *Tracker) Stream(ctx context.Context, req core.Request) (core.StreamBody, error) {
	t.calls.Add(1)
	return t.Inner.Stream(ctx, req)
}

// DoFresh bypasses the cache when the inner client supports it.
func (t *Tracker) DoFresh(ctx context.Context, req core.Request) (*core.Response, error) {
	type fresher interface {
		DoFresh(context.Context, core.Request) (*core.Response, error)
	}
	if f, ok := t.Inner.(fresher); ok {
		t.calls.Add(1)
		return f.DoFresh(ctx, req)
	}
	return t.Do(ctx, req)
}

// AllCached reports whether at least one call happened and all were cached.
func (t *Tracker) AllCached() bool {
	c := t.calls.Load()
	return c > 0 && t.cached.Load() == c
}

// Calls is the number of upstream responses observed.
func (t *Tracker) Calls() int64 { return t.calls.Load() }

// Shared holds long-lived dependencies, reused across commands in one process
// so connections stay warm (matters for the MCP server). Both fields are
// interfaces so tests can substitute a fake transport and a temp database.
type Shared struct {
	HTTP  core.HTTPDoer
	Store core.StoreOpener
}

// NewShared builds production dependencies for the given cache mode.
func NewShared(mode httpx.CacheMode) *Shared {
	var disk *cache.Disk
	if mode != httpx.CacheOff {
		disk = cache.New(paths.CacheDir())
	}
	return &Shared{HTTP: httpx.New(disk, mode), Store: store.New(paths.DBPath())}
}

// Env returns a per-invocation environment with its own tracker.
func (s *Shared) Env(interactive bool, progress func(string, ...any)) (*core.Env, *Tracker) {
	tr := &Tracker{Inner: s.HTTP}
	return &core.Env{HTTP: tr, Store: s.Store, Now: time.Now, Interactive: interactive, Progress: progress}, tr
}

// Envelope assembles the success document for a finished run.
func Envelope(cmd *core.Command, res *core.Result, tr *Tracker, w *render.Writer, now time.Time, module string) render.Envelope {
	meta := map[string]any{}
	var data any = map[string]any{}
	if res != nil {
		for k, v := range res.Meta {
			meta[k] = v
		}
		if res.Data != nil {
			data = res.Data
		}
	}
	delete(meta, "upstream_cached")
	meta["module"] = module
	if n := w.Truncated(); n > 0 {
		meta["truncated"] = true
		meta["truncated_rows"] = n
	}
	if w.Max >= 0 {
		meta["max"] = w.Max
	}
	return render.Envelope{
		OK:        true,
		Command:   cmd.Name(),
		Source:    "mef",
		FetchedAt: render.Now(now),
		Cached:    cmd.Network && tr != nil && tr.AllCached(),
		Meta:      meta,
		Data:      data,
	}
}
