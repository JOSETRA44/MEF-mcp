package core

import (
	"context"
	"database/sql"
	"net/url"
)

// Request is an upstream call, described so it can be cached by content.
type Request struct {
	Method  string     // GET or POST
	URL     string     // absolute URL without query
	Query   url.Values // encoded into the URL
	Form    url.Values // POST application/x-www-form-urlencoded body
	JSON    any        // POST application/json body (mutually exclusive with Form)
	TTLKey  string     // "<module>/<command>" used to pick the cache TTL
	Timeout int        // seconds; 0 = client default
	// Charset "mixed" decodes a body mixing UTF-8 and Windows-1252 bytes,
	// which Consulta Amigable does within a single response.
	Charset string
}

// Response is a decoded upstream answer.
type Response struct {
	Status int
	Body   []byte
	Cached bool
	URL    string // the full URL requested, for provenance
}

// HTTPDoer performs upstream requests with caching, retries and politeness.
type HTTPDoer interface {
	Do(ctx context.Context, req Request) (*Response, error)
	// Stream performs an uncached request and hands back the raw body for
	// large downloads; the caller must close it.
	Stream(ctx context.Context, req Request) (StreamBody, error)
}

// StreamBody is a streamed response body.
type StreamBody interface {
	Read(p []byte) (int, error)
	Close() error
	Size() int64 // -1 when unknown
}

// StoreOpener gives commands access to the local SQLite store.
type StoreOpener interface {
	Open(ctx context.Context, readOnly bool) (*sql.DB, error)
	Path() string
}
