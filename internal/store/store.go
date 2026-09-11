// Package store is the local SQLite database behind `mef sync` and `mef sql`.
//
// It uses modernc.org/sqlite, a pure-Go port: no CGO, so the binary still
// cross-compiles for every OS from one machine and installs with `go install`.
package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Store opens the database file at DBPath.
type Store struct{ DBPath string }

// New returns a store at path.
func New(path string) *Store { return &Store{DBPath: path} }

// Path implements core.StoreOpener.
func (s *Store) Path() string { return s.DBPath }

// Open implements core.StoreOpener. Read-only handles set query_only, which
// SQLite enforces for every statement — the guarantee behind mef_sql.
func (s *Store) Open(ctx context.Context, readOnly bool) (*sql.DB, error) {
	if readOnly {
		if _, err := os.Stat(s.DBPath); err != nil {
			return nil, core.NotFoundf("Load data first: `mef sync <resource_id> --table <name>`.", "no local database yet at %s", s.DBPath)
		}
	} else if err := os.MkdirAll(filepath.Dir(s.DBPath), 0o755); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "cannot create %s", filepath.Dir(s.DBPath))
	}
	dsn := "file:" + filepath.ToSlash(s.DBPath) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	if readOnly {
		dsn += "&_pragma=query_only(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "opening database: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, core.Wrap(core.CodeInternal, err, "", "opening database: %v", err)
	}
	return db, nil
}

var reIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// ValidTable enforces a safe identifier; table names are interpolated into DDL.
func ValidTable(name string) error {
	if !reIdent.MatchString(name) || strings.HasPrefix(strings.ToLower(name), "sqlite_") || strings.HasPrefix(name, "_meta") {
		return core.Usagef("Use letters, digits and underscores, starting with a letter, e.g. canon_2025.", "invalid table name %q", name)
	}
	return nil
}

// Quote quotes an identifier (column names come from upstream data).
func Quote(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }

const metaDDL = `CREATE TABLE IF NOT EXISTS _meta_downloads (
	table_name  TEXT PRIMARY KEY,
	source      TEXT NOT NULL,
	resource_id TEXT,
	source_url  TEXT,
	filters     TEXT,
	fetched_at  TEXT NOT NULL,
	row_count   INTEGER NOT NULL
)`

// EnsureMeta creates the provenance table.
func EnsureMeta(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, metaDDL)
	return err
}

// ColumnType infers a SQLite affinity from sample text values. Identifiers
// such as "0040" (programa) or "01" (sector) must stay TEXT: coercing them to
// INTEGER drops the leading zero and silently breaks every later join.
func ColumnType(values []string) string {
	kind := "INTEGER"
	seen := false
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		seen = true
		if len(v) > 1 && v[0] == '0' && v[1] != '.' {
			return "TEXT"
		}
		if isInt(v) {
			continue
		}
		// An all-digit value too long for int64 is an identifier, not a
		// measurement: as REAL it would silently lose its last digits.
		if allDigits(v) {
			return "TEXT"
		}
		if isFloat(v) {
			kind = "REAL"
			continue
		}
		return "TEXT"
	}
	if !seen {
		return "TEXT"
	}
	return kind
}

func isInt(s string) bool {
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	if s == "" || len(s) > 18 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func isFloat(s string) bool {
	if strings.ContainsAny(s, "xXpP_") { // reject hex floats and digit separators
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
