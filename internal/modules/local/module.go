// Package local holds the commands that work on the local SQLite store:
// sync (download into it), sql (query it) and tablas (inspect it).
package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/datosabiertos"
	"github.com/JOSETRA44/mef-mcp/internal/store"
)

// Module implements core.Module.
type Module struct{ DA *datosabiertos.Module }

// New returns the module; it borrows the datastore client for sync.
func New(da *datosabiertos.Module) *Module { return &Module{DA: da} }

func (m *Module) ID() string      { return "local" }
func (m *Module) Title() string   { return "Base de datos local (SQLite)" }
func (m *Module) BaseURL() string { return "file://mef.db" }

func (m *Module) Commands() []*core.Command {
	return []*core.Command{m.sync(), m.sql(), m.tablas()}
}

func (m *Module) sync() *core.Command {
	return &core.Command{
		Path:    []string{"sync"},
		Tool:    "mef_sync",
		Title:   "Cargar un recurso del MEF a SQL local",
		Summary: "Download an open-data resource into the local SQLite database as a table, with provenance",
		Description: `Downloads rows of an open-data resource (datastore API) into a table of the local SQLite database, so they can be joined and aggregated with mef_sql without downloading again. Column types are inferred (INTEGER/REAL/TEXT) with codes that carry leading zeros ("0040", "01") kept as TEXT so joins stay correct. Each sync is recorded in _meta_downloads (resource, filters, time, row count) so answers can cite their source.

Use it when a question needs more than a quick look — totals over thousands of rows, joins across resources, repeated queries. Get resource_id from mef_datos_dataset. Behaviour: pages of 5000 rows until exhausted or max_rows — 2–20 s per page unfiltered, but ~90 s per page with filters because MEF's server scans the whole table, so an unfiltered sync plus a SQL WHERE is usually much faster; the table is replaced atomically, so a failed sync leaves the previous copy intact. Writes only to the local database file (path in meta.db); nothing is sent anywhere. Fails with not_found for resources without a datastore.`,
		Params: []core.Param{
			{Name: "resource_id", Type: core.TypeString, Positional: true, Required: true, Help: `Resource UUID from mef_datos_dataset, e.g. "5f3b3cbe-3955-41cc-8662-1757ebb5cf53".`},
			{Name: "table", Type: core.TypeString, Required: true, Short: "t", Help: `Destination table name: letters, digits, underscore, e.g. "gasto_2012_2016".`},
			{Name: "filter", Type: core.TypeStringList, Short: "F", Help: `Exact-match filters COLUMN=VALUE applied upstream, e.g. ["SECTOR=13"].`},
			{Name: "max_rows", Type: core.TypeInt, Default: 200000, Min: core.IntPtr(1), Max: core.IntPtr(20_000_000), Help: "Stop after this many rows, e.g. 50000."},
		},
		RowFields: []string{"table", "rows", "columns", "resource_id", "seconds", "db"},
		Network:   true,
		Examples:  []string{"mef sync 5f3b3cbe-3955-41cc-8662-1757ebb5cf53 --table gasto_2012_2016 --max-rows 20000"},
		Run:       m.runSync,
	}
}

func (m *Module) runSync(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	rid, table := in.Str("resource_id"), in.Str("table")
	if err := store.ValidTable(table); err != nil {
		return nil, err
	}
	filters, err := in.Pairs("filter")
	if err != nil {
		return nil, err
	}
	maxRows := in.Int("max_rows")
	const pageSize = 5000
	started := env.Now()

	first, err := m.DA.Datastore(ctx, env, rid, filters, "", min(pageSize, maxRows), 0)
	if err != nil {
		return nil, err
	}
	if first.Null || len(first.Rows) == 0 {
		if len(filters) == 0 {
			return nil, core.NotFoundf("This resource has no datastore loaded; download its file with `mef datos descargar <url>`.", "resource %s returned no records", rid)
		}
		return nil, core.NotFoundf("Relax the filters; nothing matched upstream.", "no rows match the filters for %s", rid)
	}

	cols := first.Columns
	types := make([]string, len(cols))
	for i, c := range cols {
		sample := make([]string, 0, len(first.Rows))
		for _, r := range first.Rows {
			if s, ok := r[c].(string); ok {
				sample = append(sample, s)
			}
		}
		types[i] = store.ColumnType(sample)
	}

	db, err := env.Store.Open(ctx, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := store.EnsureMeta(ctx, db); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "creating metadata table: %v", err)
	}

	tmp := table + "__sync"
	defs := make([]string, len(cols))
	for i, c := range cols {
		defs[i] = store.Quote(c) + " " + types[i]
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+store.Quote(tmp)); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+store.Quote(tmp)+" ("+strings.Join(defs, ", ")+")"); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "creating table: %v", err)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	insertSQL := "INSERT INTO " + store.Quote(tmp) + " VALUES (" + placeholders + ")"

	insert := func(rows []core.Row) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			tx.Rollback()
			return err
		}
		vals := make([]any, len(cols))
		for _, r := range rows {
			for i, c := range cols {
				vals[i] = convert(r[c], types[i])
			}
			if _, err := stmt.ExecContext(ctx, vals...); err != nil {
				stmt.Close()
				tx.Rollback()
				return err
			}
		}
		stmt.Close()
		return tx.Commit()
	}

	loaded := 0
	page := first
	offset := 0
	for {
		rows := page.Rows
		if loaded+len(rows) > maxRows {
			rows = rows[:maxRows-loaded]
		}
		if err := insert(rows); err != nil {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+store.Quote(tmp))
			return nil, core.Wrap(core.CodeInternal, err, "", "inserting rows: %v", err)
		}
		loaded += len(rows)
		offset += len(page.Rows)
		if env.Progress != nil {
			env.Progress("%s: %d rows loaded…", table, loaded)
		}
		if loaded >= maxRows || len(page.Rows) < pageSize {
			break
		}
		next, err := m.DA.Datastore(ctx, env, rid, filters, "", min(pageSize, maxRows-loaded), offset)
		if err != nil {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+store.Quote(tmp))
			return nil, err
		}
		if next.Null || len(next.Rows) == 0 {
			break
		}
		page = next
	}

	filterJSON, _ := json.Marshal(filters)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
	}
	steps := []struct {
		q    string
		args []any
	}{
		{"DROP TABLE IF EXISTS " + store.Quote(table), nil},
		{"ALTER TABLE " + store.Quote(tmp) + " RENAME TO " + store.Quote(table), nil},
		{`INSERT INTO _meta_downloads (table_name, source, resource_id, source_url, filters, fetched_at, row_count)
		  VALUES (?, 'datosabiertos', ?, ?, ?, ?, ?)
		  ON CONFLICT(table_name) DO UPDATE SET source=excluded.source, resource_id=excluded.resource_id,
		  source_url=excluded.source_url, filters=excluded.filters, fetched_at=excluded.fetched_at, row_count=excluded.row_count`,
			[]any{table, rid, first.URL, string(filterJSON), env.Now().UTC().Format(time.RFC3339), loaded}},
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			tx.Rollback()
			return nil, core.Wrap(core.CodeInternal, err, "", "finalising table: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
	}

	secs := env.Now().Sub(started).Seconds()
	if err := sink.Emit(core.Row{"table": table, "rows": loaded, "columns": len(cols), "resource_id": rid, "seconds": float64(int(secs*10)) / 10, "db": env.Store.Path()}); err != nil {
		return nil, err
	}
	schema := make([]map[string]string, len(cols))
	for i, c := range cols {
		schema[i] = map[string]string{"column": c, "type": types[i]}
	}
	return &core.Result{
		Columns: []string{"table", "rows", "columns", "resource_id", "seconds", "db"},
		Data:    map[string]any{"schema": schema},
		Meta:    map[string]any{"db": env.Store.Path(), "next": fmt.Sprintf(`mef sql "SELECT * FROM %s LIMIT 10"`, table)},
	}, nil
}

func convert(v any, typ string) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	// Empty cells are absent values; stored as "" they would sort above every
	// number and poison MIN/MAX/AVG.
	if strings.TrimSpace(s) == "" {
		return nil
	}
	switch typ {
	case "INTEGER":
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return n
		}
	case "REAL":
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f
		}
	}
	return s
}

func (m *Module) sql() *core.Command {
	return &core.Command{
		Path:    []string{"sql"},
		Tool:    "mef_sql",
		Title:   "Consultar la base local con SQL",
		Summary: "Run a read-only SQL query (SQLite dialect) over tables loaded with sync",
		Description: `Runs one read-only SQL statement (SQLite dialect: SELECT, WITH … SELECT, EXPLAIN, or PRAGMA table_info) against the local database filled by mef_sync, and returns the result rows.

Use it for aggregates, rankings and joins over data already synced — e.g. SELECT PLIEGO_NOMBRE, SUM(DEVENGADO_2016) FROM gasto GROUP BY 1 ORDER BY 2 DESC LIMIT 10. List tables and columns first with mef_tablas. Behaviour: the connection is opened with SQLite's query_only pragma, so INSERT/UPDATE/DELETE/DDL are rejected by the engine itself; a single statement only. Results are capped by "max" (MCP default 200, with meta.truncated when cut — aggregate in SQL instead of paging). Runs locally, instantly, never touching the network. Fails with not_found when nothing has been synced yet.`,
		Params: []core.Param{
			{Name: "query", Type: core.TypeString, Positional: true, Required: true, Help: `One SELECT statement, e.g. "SELECT SECTOR_NOMBRE, COUNT(*) FROM gasto GROUP BY 1".`},
			{Name: "max", Type: core.TypeInt, Default: core.DefaultMax, Min: core.IntPtr(1), Max: core.IntPtr(10_000_000), Help: "Maximum rows returned, e.g. 500."},
		},
		ReadOnly: true,
		Examples: []string{`mef sql "SELECT name FROM sqlite_master WHERE type='table'"`},
		Run:      m.runSQL,
	}
}

func checkReadOnly(q string) (string, error) {
	q = strings.TrimSpace(q)
	q = strings.TrimRight(q, "; \t\r\n")
	if strings.Contains(q, ";") {
		return "", core.Usagef("Send a single statement without ';' separators.", "multiple statements are not allowed")
	}
	lower := strings.ToLower(q)
	head := strings.Fields(lower)
	if len(head) == 0 {
		return "", core.Usagef("", "empty query")
	}
	switch head[0] {
	case "select", "with", "explain", "values":
		return q, nil
	case "pragma":
		for _, ok := range []string{"table_info", "table_xinfo", "index_list", "index_info"} {
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(lower, "pragma")), ok) {
				return q, nil
			}
		}
	}
	return "", core.Usagef("Only SELECT, WITH, EXPLAIN, VALUES and PRAGMA table_info are allowed; load data with `mef sync`.", "statement %q is not read-only", head[0])
}

func (m *Module) runSQL(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	q, err := checkReadOnly(in.Str("query"))
	if err != nil {
		return nil, err
	}
	db, err := env.Store.Open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		msg := err.Error()
		hint := "Check table and column names with `mef tablas`."
		if strings.Contains(msg, "readonly") || strings.Contains(msg, "query_only") {
			hint = "The SQL tool is read-only; use `mef sync` to load data."
		}
		return nil, core.Usagef(hint, "SQL error: %s", msg)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	sink.SetColumns(cols)
	n, truncated := 0, false
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
		}
		row := core.Row{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			row[c] = vals[i]
		}
		n++
		if err := sink.Emit(row); err != nil {
			return nil, err
		}
		if sink.Wanted() == 0 {
			truncated = rows.Next()
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, core.Usagef("", "SQL error: %v", err)
	}
	meta := map[string]any{"db": env.Store.Path(), "returned": n}
	if truncated {
		meta["truncated"] = true
	}
	return &core.Result{Columns: cols, Meta: meta}, nil
}

func (m *Module) tablas() *core.Command {
	return &core.Command{
		Path:    []string{"tablas"},
		Tool:    "mef_tablas",
		Title:   "Tablas de la base local",
		Summary: "List local tables with row counts and provenance, or profile one table's columns",
		Description: `Without "table": lists every table loaded with mef_sync — row count, column count, the resource it came from, filters used and when it was fetched. With "table": profiles that table column by column — declared type, null count, distinct count, min, max and average for numeric columns.

Use it right after mef_sync and before writing mef_sql: it tells you which tables exist, the exact (case-sensitive) column names to query, which columns are numeric and can be summed, and which are mostly empty. Use mef_datos_consultar instead to inspect a resource you have not synced.

Behaviour: local only, no network, never cached; profiling scans the whole table (a few seconds for a million rows). Fails with not_found if nothing has been synced yet or the table does not exist.`,
		Params: []core.Param{
			{Name: "table", Type: core.TypeString, Positional: true, Help: `Table to profile, e.g. "gasto_2012_2016". Omit to list all tables.`},
		},
		ReadOnly: true,
		Examples: []string{"mef tablas", "mef tablas gasto_2012_2016 -f md"},
		Run:      m.runTablas,
	}
}

func (m *Module) runTablas(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	db, err := env.Store.Open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if t := in.Str("table"); t != "" {
		return profile(ctx, db, t, sink)
	}
	names, err := tableNames(ctx, db)
	if err != nil {
		return nil, err
	}
	prov := map[string][4]string{}
	if rows, err := db.QueryContext(ctx, `SELECT table_name, COALESCE(resource_id,''), COALESCE(filters,''), fetched_at FROM _meta_downloads`); err == nil {
		for rows.Next() {
			var t, r, f, at string
			if rows.Scan(&t, &r, &f, &at) == nil {
				prov[t] = [4]string{r, f, at}
			}
		}
		rows.Close()
	}
	for _, t := range names {
		var count int64
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+store.Quote(t)).Scan(&count)
		cols, _ := columnInfo(ctx, db, t)
		p := prov[t]
		if err := sink.Emit(core.Row{"table": t, "rows": count, "columns": len(cols), "resource_id": p[0], "filters": p[1], "fetched_at": p[2]}); err != nil {
			return nil, err
		}
	}
	return &core.Result{Columns: []string{"table", "rows", "columns", "resource_id", "filters", "fetched_at"}, Meta: map[string]any{"db": env.Store.Path()}}, nil
}

func tableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '\_meta%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out, nil
}

type colInfo struct{ Name, Type string }

func columnInfo(ctx context.Context, db *sql.DB, table string) ([]colInfo, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+store.Quote(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []colInfo
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
			out = append(out, colInfo{name, typ})
		}
	}
	return out, nil
}

func profile(ctx context.Context, db *sql.DB, table string, sink core.Sink) (*core.Result, error) {
	if err := store.ValidTable(table); err != nil {
		return nil, err
	}
	cols, err := columnInfo(ctx, db, table)
	if err != nil || len(cols) == 0 {
		return nil, core.NotFoundf("List tables with `mef tablas`.", "table %q not found", table)
	}
	var total int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+store.Quote(table)).Scan(&total)
	for _, c := range cols {
		q := store.Quote(c.Name)
		var nulls, distinct int64
		var mn, mx, avg any
		agg := "NULL"
		if c.Type == "INTEGER" || c.Type == "REAL" {
			agg = "AVG(" + q + ")"
		}
		err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT SUM(%[1]s IS NULL OR %[1]s = ''), COUNT(DISTINCT %[1]s), MIN(%[1]s), MAX(%[1]s), %[2]s FROM %[3]s`, q, agg, store.Quote(table))).Scan(&nulls, &distinct, &mn, &mx, &avg)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "", "profiling %s: %v", c.Name, err)
		}
		row := core.Row{"column": c.Name, "type": c.Type, "nulls": nulls, "distinct": distinct, "min": str(mn), "max": str(mx)}
		if f, ok := avg.(float64); ok {
			row["avg"] = float64(int64(f*100)) / 100
		}
		if err := sink.Emit(row); err != nil {
			return nil, err
		}
	}
	return &core.Result{Columns: []string{"column", "type", "nulls", "distinct", "min", "max", "avg"}, Meta: map[string]any{"table": table, "rows": total}}, nil
}

func str(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// Command exposes one command by its CLI leaf name so other modules can run
// it instead of reimplementing it; mef_reporte uses sql as a report source.
func (m *Module) Command(name string) *core.Command {
	switch name {
	case "sync":
		return m.sync()
	case "sql":
		return m.sql()
	case "tablas":
		return m.tablas()
	}
	return nil
}
