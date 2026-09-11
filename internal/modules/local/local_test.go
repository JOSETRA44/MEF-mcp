package local

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/datosabiertos"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

const rid = "5f3b3cbe-3955-41cc-8662-1757ebb5cf53"

func records(recs ...map[string]string) func(core.Request, int) ([]byte, error) {
	return func(core.Request, int) ([]byte, error) {
		return json.Marshal(map[string]any{"records": recs, "sucess": "true"})
	}
}

func setup(t *testing.T, reply func(core.Request, int) ([]byte, error)) (*Module, *core.Env) {
	t.Helper()
	da := &datosabiertos.Module{EP: datosabiertos.Endpoints{Datastore: "https://fake/datastore"}}
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "datastore", Reply: reply}}}
	return New(da), testutil.Env(t, fake)
}

func call(t *testing.T, env *core.Env, c *core.Command, raw map[string]any) (*testutil.Sink, error) {
	t.Helper()
	sink := &testutil.Sink{}
	in, err := core.NewInput(c.Params, raw, nil)
	if err != nil {
		return nil, err
	}
	_, err = c.Run(context.Background(), env, in, sink)
	return sink, err
}

func code(err error) core.ErrorCode {
	var e *core.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSyncThenSQLEndToEnd(t *testing.T) {
	m, env := setup(t, records(
		map[string]string{"SECTOR": "01", "MONTO": "12.5", "NOMBRE": "Perú"},
		map[string]string{"SECTOR": "13", "MONTO": "", "NOMBRE": "Ñuñoa"},
		map[string]string{"SECTOR": "13", "MONTO": "7", "NOMBRE": "Cusco"},
	))
	sink, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "gasto"})
	if err != nil {
		t.Fatal(err)
	}
	if sink.Rows[0]["rows"] != 3 {
		t.Fatalf("sync row: %v", sink.Rows[0])
	}

	out, err := call(t, env, m.sql(), map[string]any{"query": "SELECT typeof(SECTOR) ts, SUM(MONTO) total, COUNT(MONTO) n, MAX(NOMBRE) mx FROM gasto"})
	if err != nil {
		t.Fatal(err)
	}
	r := out.Rows[0]
	// Leading-zero codes stay TEXT; empty cells become NULL, not "".
	if r["ts"] != "text" || r["total"] != 19.5 || r["n"] != int64(2) || r["mx"] != "Ñuñoa" {
		t.Fatalf("sql row: %v", r)
	}

	tables, err := call(t, env, m.tablas(), map[string]any{})
	if err != nil || len(tables.Rows) != 1 || tables.Rows[0]["resource_id"] != rid {
		t.Fatalf("tablas: %v %v", tables.Rows, err)
	}
	prof, err := call(t, env, m.tablas(), map[string]any{"table": "gasto"})
	if err != nil || len(prof.Rows) != 3 {
		t.Fatalf("profile: %v %v", prof.Rows, err)
	}
}

func TestSyncHonoursMaxRows(t *testing.T) {
	m, env := setup(t, records(
		map[string]string{"A": "1"}, map[string]string{"A": "2"}, map[string]string{"A": "3"},
	))
	if _, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "t", "max_rows": 2}); err != nil {
		t.Fatal(err)
	}
	out, _ := call(t, env, m.sql(), map[string]any{"query": "SELECT COUNT(*) n FROM t"})
	if out.Rows[0]["n"] != int64(2) {
		t.Fatalf("max_rows ignored: %v", out.Rows[0])
	}
}

func TestFailedResyncKeepsPreviousTable(t *testing.T) {
	m, env := setup(t, records(map[string]string{"A": "1"}))
	if _, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "t"}); err != nil {
		t.Fatal(err)
	}
	// Same database, but an upstream that now matches nothing.
	emptyEnv := *env
	emptyEnv.HTTP = &testutil.Fake{Routes: []testutil.Route{{Match: "datastore", Reply: records()}}}
	_, err := call(t, &emptyEnv, m.sync(), map[string]any{"resource_id": rid, "table": "t", "filter": []string{"A=9"}})
	if code(err) != core.CodeNotFound {
		t.Fatalf("want not_found, got %v", err)
	}
	out, err := call(t, env, m.sql(), map[string]any{"query": "SELECT COUNT(*) n FROM t"})
	if err != nil || out.Rows[0]["n"] != int64(1) {
		t.Fatalf("previous table lost: %v %v", out, err)
	}
}

func TestSQLIsReadOnly(t *testing.T) {
	m, env := setup(t, records(map[string]string{"A": "1"}))
	if _, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "t"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DELETE FROM t",
		"DROP TABLE t",
		"SELECT 1; DELETE FROM t",
		"PRAGMA query_only=0",
		"WITH x AS (SELECT 1) DELETE FROM t", // passes the prefix check; SQLite's query_only must stop it
	} {
		if _, err := call(t, env, m.sql(), map[string]any{"query": q}); code(err) != core.CodeUsage {
			t.Errorf("%q: want usage error, got %v", q, err)
		}
	}
	out, _ := call(t, env, m.sql(), map[string]any{"query": "SELECT COUNT(*) n FROM t"})
	if out.Rows[0]["n"] != int64(1) {
		t.Fatal("a write slipped through")
	}
	if _, err := call(t, env, m.sql(), map[string]any{"query": "PRAGMA table_info(t)"}); err != nil {
		t.Fatalf("table_info must be allowed: %v", err)
	}
}

func TestSQLBeforeSyncIsNotFound(t *testing.T) {
	m, env := setup(t, records())
	if _, err := call(t, env, m.sql(), map[string]any{"query": "SELECT 1"}); code(err) != core.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestSQLTruncationIsReported(t *testing.T) {
	m, env := setup(t, records(map[string]string{"A": "1"}, map[string]string{"A": "2"}, map[string]string{"A": "3"}))
	if _, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "t"}); err != nil {
		t.Fatal(err)
	}
	c := m.sql()
	in, _ := core.NewInput(c.Params, map[string]any{"query": "SELECT A FROM t"}, nil)
	sink := &testutil.Sink{Max: 2}
	res, err := c.Run(context.Background(), env, in, sink)
	if err != nil || len(sink.Rows) != 2 || res.Meta["truncated"] != true {
		t.Fatalf("rows=%d meta=%v err=%v", len(sink.Rows), res.Meta, err)
	}
}

func TestSyncRejectsBadTableName(t *testing.T) {
	m, env := setup(t, records())
	if _, err := call(t, env, m.sync(), map[string]any{"resource_id": rid, "table": "x; DROP"}); code(err) != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
}
