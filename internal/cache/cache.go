// Package cache is the cross-process disk cache from ARSENAL-SPEC §3.
//
// A CLI exits after one call, so an in-memory cache would never be hit twice;
// entries live on disk as one small JSON file each, keyed by the SHA-256 of
// the request. Writes go through a temp file + rename, so a crash or two
// concurrent processes can never leave a torn entry behind.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is the on-disk record.
type Entry struct {
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    int       `json:"status"`
	URL       string    `json:"url"`
	Body      []byte    `json:"body"`
}

// Disk is a directory of entries.
type Disk struct {
	Dir string
	Now func() time.Time
}

// New returns a disk cache rooted at dir.
func New(dir string) *Disk { return &Disk{Dir: dir, Now: time.Now} }

// Key hashes method, URL, sorted query and body. No credential is ever part of
// a MEF request, but the key is content-derived so none could leak into it.
func Key(method, rawURL string, query url.Values, body []byte) string {
	h := sha256.New()
	h.Write([]byte(strings.ToUpper(method)))
	h.Write([]byte{0})
	h.Write([]byte(rawURL))
	h.Write([]byte{0})
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := append([]string(nil), query[k]...)
		// Order of repeated values is preserved: for Consulta Amigable the
		// parameter order is the drill-down path and therefore meaningful.
		h.Write([]byte(k + "=" + strings.Join(vals, "\x1f") + "&"))
	}
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func (d *Disk) path(key string) string {
	return filepath.Join(d.Dir, key[:2], key+".json")
}

// Get returns a fresh entry, or nil on a miss or expiry.
func (d *Disk) Get(key string) *Entry {
	raw, err := os.ReadFile(d.path(key))
	if err != nil {
		return nil
	}
	var e Entry
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	if !d.Now().Before(e.ExpiresAt) {
		return nil
	}
	return &e
}

// Put stores a successful response. Non-2xx statuses are refused: caching a
// failure would make an outage outlive itself.
func (d *Disk) Put(key string, status int, rawURL string, body []byte, ttl time.Duration) error {
	if status < 200 || status > 299 || ttl <= 0 {
		return nil
	}
	now := d.Now()
	raw, err := json.Marshal(Entry{FetchedAt: now.UTC(), ExpiresAt: now.Add(ttl).UTC(), Status: status, URL: rawURL, Body: body})
	if err != nil {
		return err
	}
	p := d.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), p)
}

// Stats summarises the cache directory.
type Stats struct {
	Path    string `json:"path"`
	Entries int    `json:"entries"`
	Expired int    `json:"expired"`
	Bytes   int64  `json:"bytes"`
}

// Stats walks the directory; expired entries are counted, not removed.
func (d *Disk) Stats() (Stats, error) {
	s := Stats{Path: d.Dir}
	err := filepath.WalkDir(d.Dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if de.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		s.Entries++
		s.Bytes += info.Size()
		if raw, err := os.ReadFile(p); err == nil {
			var e Entry
			if json.Unmarshal(raw, &e) == nil && !d.Now().Before(e.ExpiresAt) {
				s.Expired++
			}
		}
		return nil
	})
	return s, err
}

// Clear removes every entry and returns how many were deleted.
func (d *Disk) Clear() (int, error) {
	st, err := d.Stats()
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(d.Dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	return st.Entries, nil
}
