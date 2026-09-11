package cache

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissWriteHitExpire(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	d := &Disk{Dir: t.TempDir(), Now: func() time.Time { return now }}
	k := Key("GET", "https://x/y", url.Values{"a": {"1"}}, nil)
	if d.Get(k) != nil {
		t.Fatal("expected miss on empty cache")
	}
	if err := d.Put(k, 200, "https://x/y", []byte("Perú"), time.Hour); err != nil {
		t.Fatal(err)
	}
	e := d.Get(k)
	if e == nil || string(e.Body) != "Perú" {
		t.Fatalf("expected hit, got %+v", e)
	}
	now = now.Add(2 * time.Hour)
	if d.Get(k) != nil {
		t.Fatal("expired entry must miss")
	}
	st, _ := d.Stats()
	if st.Entries != 1 || st.Expired != 1 {
		t.Fatalf("stats: %+v", st)
	}
	n, err := d.Clear()
	if err != nil || n != 1 {
		t.Fatalf("clear: %d %v", n, err)
	}
	if _, err := os.Stat(d.Dir); !os.IsNotExist(err) {
		t.Fatal("clear must remove the directory")
	}
}

func TestNonSuccessIsNeverCached(t *testing.T) {
	d := New(t.TempDir())
	for _, status := range []int{301, 404, 429, 500, 503} {
		k := Key("GET", "https://x/"+string(rune('a'+status%26)), nil, nil)
		_ = d.Put(k, status, "u", []byte("err"), time.Hour)
		if d.Get(k) != nil {
			t.Errorf("HTTP %d was cached", status)
		}
	}
	k := Key("GET", "https://x/zero-ttl", nil, nil)
	_ = d.Put(k, 200, "u", []byte("ok"), 0)
	if d.Get(k) != nil {
		t.Error("zero TTL must not cache")
	}
}

func TestKeyIsContentDerived(t *testing.T) {
	a := Key("get", "https://x", url.Values{"b": {"2"}, "a": {"1"}}, nil)
	b := Key("GET", "https://x", url.Values{"a": {"1"}, "b": {"2"}}, nil)
	if a != b {
		t.Fatal("map order must not change the key")
	}
	if Key("POST", "https://x", nil, []byte(`{"id":1}`)) == Key("POST", "https://x", nil, []byte(`{"id":2}`)) {
		t.Fatal("body must be part of the key")
	}
	// Repeated values keep their order: it is meaningful for drill-down paths.
	if Key("GET", "u", url.Values{"k": {"1", "2"}}, nil) == Key("GET", "u", url.Values{"k": {"2", "1"}}, nil) {
		t.Fatal("repeated value order must matter")
	}
}

func TestEntriesAreShardedFiles(t *testing.T) {
	d := New(t.TempDir())
	k := Key("GET", "https://x", nil, nil)
	_ = d.Put(k, 200, "https://x", []byte("{}"), time.Hour)
	if _, err := os.Stat(filepath.Join(d.Dir, k[:2], k+".json")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(d.Dir, k[:2], k+".json"))
	if !strings.Contains(string(raw), `"expires_at"`) {
		t.Fatal("entry must record expiry")
	}
}
