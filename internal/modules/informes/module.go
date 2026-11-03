// Package informes turns a MEF query into conclusions and into a file.
//
// It owns no queries of its own: it runs the existing commands through a
// capturing sink and works on their rows. A report of spending is therefore
// always the output of mef_gasto, never a second implementation that could
// disagree with it.
package informes

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/amigable"
	"github.com/JOSETRA44/mef-mcp/internal/modules/local"
	"github.com/JOSETRA44/mef-mcp/internal/paths"
	"github.com/JOSETRA44/mef-mcp/internal/report"
)

// Module implements core.Module.
type Module struct {
	A *amigable.Module
	L *local.Module
}

// New wires the modules whose commands reports are built from.
func New(a *amigable.Module, l *local.Module) *Module { return &Module{A: a, L: l} }

func (m *Module) ID() string      { return "informes" }
func (m *Module) Title() string   { return "Análisis y reportes" }
func (m *Module) BaseURL() string { return m.A.BaseURL() }

func (m *Module) Commands() []*core.Command {
	return []*core.Command{m.analizar(), m.reporte()}
}

// capture is a core.Sink that keeps rows in memory, so one command can run
// another and work with its output.
type capture struct {
	rows []core.Row
	cols []string
	max  int
}

func (c *capture) SetColumns(cols []string) { c.cols = cols }

func (c *capture) Emit(rows ...core.Row) error {
	for _, r := range rows {
		if c.max > 0 && len(c.rows) >= c.max {
			return nil
		}
		c.rows = append(c.rows, r)
	}
	return nil
}

func (c *capture) Wanted() int {
	if c.max <= 0 {
		return -1
	}
	if left := c.max - len(c.rows); left > 0 {
		return left
	}
	return 0
}

// runSub executes another module's command with explicit arguments.
func runSub(ctx context.Context, env *core.Env, cmd *core.Command, args map[string]any, max int) (*capture, *core.Result, error) {
	if cmd == nil {
		return nil, nil, core.NewError(core.CodeInternal, "", "command not available")
	}
	in, err := core.NewInput(cmd.Params, args, nil)
	if err != nil {
		return nil, nil, err
	}
	sink := &capture{max: max}
	res, err := cmd.Run(ctx, env, in, sink)
	if err != nil {
		return nil, nil, err
	}
	if res != nil && len(res.Columns) > 0 {
		sink.cols = res.Columns
	}
	return sink, res, nil
}

func queryParams() []core.Param {
	return []core.Param{
		{Name: "by", Type: core.TypeString, Default: "nivel", Short: "b",
			Help: `Dimension to group by, e.g. "sector", "pliego", "departamento-meta". Same aliases as mef_gasto.`},
		{Name: "where", Type: core.TypeStringList, Short: "w",
			Help: `Drill-down filters DIMENSION=KEY, in order, e.g. ["nivel=E","sector=11"]. Get keys from mef_entidades.`},
		{Name: "year", Type: core.TypeInt, Default: 0, Short: "y", Min: core.IntPtr(0), Max: core.IntPtr(2100),
			Help: "Fiscal year, e.g. 2026. 0 = current year."},
		{Name: "type", Type: core.TypeString, Default: "todo", Enum: []string{"todo", "actividad", "proyecto"},
			Help: `Spending type: "todo", "actividad" (gasto corriente) or "proyecto" (inversión).`},
	}
}

var metricEnum = []string{"devengado", "pim", "pia", "certificacion", "compromiso_anual", "girado"}

func (m *Module) analizar() *core.Command {
	return &core.Command{
		Path:    []string{"analizar"},
		Tool:    "mef_analizar",
		Title:   "Análisis automático de la ejecución",
		Summary: "Computed findings for a grouping: totals, ranking, concentration, laggards and idle budget",
		Description: `Runs the same query as mef_gasto and returns the conclusions instead of the raw table: total executed against PIM with the overall avance, the ranking with each row's share, the share held by the top five, the rows with the lowest avance that do have budget, and the rows with PIM but zero execution — plus a written summary in Spanish.

Use it whenever the question is "who executes least", "where is the money concentrated", "what is not being spent", or any ask for an analysis, diagnosis or summary rather than a listing. Every figure is computed here, which is the point: totals and percentages over hundreds of rows are what to avoid doing by hand. Each finding carries its own sentence, ready to quote.

"metric" chooses what to rank by (devengado by default; PIM to analyse budget allocation instead of execution). "top" sets the ranking length. The full detail — top, rezagados, sin_ejecucion, estadísticas — is in data; rows carry one finding each.

Behaviour: fetches the whole grouping (up to "max" rows, several pages, 1–2 s per page, cached). For a file version of the same thing use mef_reporte; for several years use mef_serie.`,
		Params: append(queryParams(),
			core.Param{Name: "metric", Type: core.TypeString, Default: "devengado", Enum: metricEnum,
				Help: `Column to rank and total by, e.g. "devengado" (ejecución) or "pim" (presupuesto).`},
			core.Param{Name: "top", Type: core.TypeInt, Default: 10, Min: core.IntPtr(1), Max: core.IntPtr(100),
				Help: "Length of the rankings, e.g. 5."},
			core.Param{Name: "max", Type: core.TypeInt, Default: 2000, Min: core.IntPtr(1), Max: core.IntPtr(20000),
				Help: "Maximum rows to analyse, e.g. 500."},
		),
		RowFields: []string{"tipo", "titulo", "detalle", "valor"},
		Network:   true,
		ReadOnly:  true,
		Examples: []string{
			"mef analizar --by pliego --where nivel=E",
			"mef analizar --by departamento-meta --type proyecto --top 5",
			"mef analizar --by ejecutora --where nivel=M --metric pim -f json",
		},
		Run: m.runAnalizar,
	}
}

func (m *Module) runAnalizar(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	rows, res, err := m.gastoRows(ctx, env, in, in.Int("max"))
	if err != nil {
		return nil, err
	}
	a := analysis.Analyze(rows.rows, analysis.Options{Metric: in.Str("metric"), TopN: in.Int("top")})
	sink.SetColumns([]string{"tipo", "titulo", "detalle", "valor"})
	if err := sink.Emit(analysis.FindingRows(a)...); err != nil {
		return nil, err
	}
	meta := map[string]any{"metric": a.Metric, "top": in.Int("top"), "filas_analizadas": a.Rows}
	for _, k := range []string{"year", "group_by", "group_header", "filters", "type", "last_updated", "has_more", "source_url", "upstream_cached"} {
		if v, ok := res.Meta[k]; ok {
			meta[k] = v
		}
	}
	data := map[string]any{
		"resumen": a.Summary,
		"totales": map[string]any{"presupuesto": a.TotalBudget, "ejecutado": a.Total, "avance_pct": a.AvancePct},
		"top":     a.Top, "rezagados": a.Bottom, "sin_ejecucion": a.Idle,
		"estadisticas": map[string]any{
			"promedio": a.Mean, "mediana": a.Median,
			"concentracion_top5_pct": a.Top5Share, "presupuesto_sin_ejecutar": a.IdleBudget,
		},
	}
	out := &core.Result{Columns: []string{"tipo", "titulo", "detalle", "valor"}, Data: data, Meta: meta}
	if res.Meta["has_more"] == true {
		out.Notes = append(out.Notes, "the grouping was larger than --max: the analysis covers only the rows fetched")
	}
	return out, nil
}

// gastoRows runs mef_gasto sorted by the chosen metric.
func (m *Module) gastoRows(ctx context.Context, env *core.Env, in core.Input, max int) (*capture, *core.Result, error) {
	metric := in.Str("metric")
	if metric == "" {
		metric = "devengado"
	}
	args := map[string]any{
		"by": in.Str("by"), "type": in.Str("type"), "all": true, "max": max,
		"sort": metric, "desc": true,
	}
	if w := in.List("where"); len(w) > 0 {
		args["where"] = w
	}
	if y := in.Int("year"); y != 0 {
		args["year"] = y
	}
	return runSub(ctx, env, m.A.Command("gasto"), args, max)
}

func (m *Module) reporte() *core.Command {
	return &core.Command{
		Path:    []string{"reporte"},
		Tool:    "mef_reporte",
		Title:   "Generar reporte (Excel, HTML o Markdown)",
		Summary: "Write a ready-to-share report file — xlsx workbook, self-contained HTML page or Markdown memo",
		Description: `Produces a file on the user's machine from a MEF query: an Excel workbook (sheet "Resumen" with indicators, findings and a native bar chart; "Datos" with the full table, frozen header and autofilter; "Fuente" with provenance), a self-contained HTML page (indicators, inline SVG chart, findings, table — no internet needed to open it, prints to PDF), or a Markdown memo.

Use it whenever someone asks for a report, an Excel, a spreadsheet, a summary sheet, a document or something to send: this is the deliverable. The figures and conclusions are computed here, so the table and the narrative always agree.

"source" picks the query: "gasto" (one year, grouped by "by" with "where" filters), "serie" ("from"/"to" across years) or "sql" (a read-only SELECT over tables loaded with mef_sync, via "query"). "format" is xlsx, html or md. "nombre" names the file, "titulo" its heading.

Behaviour: the file is written under the output directory (meta.directorio; MEF_OUTPUT or <MEF_HOME>/output) and the row returned carries its absolute path, size and row count — report the path so the user can open it. An existing file with the same name is replaced. Reports take as long as the underlying query (1–2 s per page of gasto, one request per year for serie) and reuse the disk cache. Nothing is uploaded anywhere.`,
		Params: append(queryParams(),
			core.Param{Name: "source", Type: core.TypeString, Default: "gasto", Enum: []string{"gasto", "serie", "sql"},
				Help: `Where the data comes from: "gasto" (one year), "serie" (several years) or "sql" (local database).`},
			core.Param{Name: "format", Type: core.TypeString, Default: "xlsx", Enum: report.Formats,
				Help: `File format: "xlsx" (Excel workbook), "html" (self-contained page) or "md" (Markdown memo).`},
			core.Param{Name: "nombre", Type: core.TypeString,
				Help: `File name without extension, e.g. "ejecucion-salud-2026". Defaults to a name derived from the query.`},
			core.Param{Name: "titulo", Type: core.TypeString,
				Help: `Heading of the report, e.g. "Ejecución del gasto por pliego, 2026".`},
			core.Param{Name: "query", Type: core.TypeString,
				Help: `For source=sql: one read-only SELECT, e.g. "SELECT SECTOR_NOMBRE, SUM(DEVENGADO_2016) FROM gasto GROUP BY 1".`},
			core.Param{Name: "from", Type: core.TypeInt, Default: 0, Min: core.IntPtr(0), Max: core.IntPtr(2100),
				Help: "For source=serie: first fiscal year, e.g. 2019."},
			core.Param{Name: "to", Type: core.TypeInt, Default: 0, Min: core.IntPtr(0), Max: core.IntPtr(2100),
				Help: "For source=serie: last fiscal year, e.g. 2026."},
			core.Param{Name: "metric", Type: core.TypeString, Default: "devengado", Enum: metricEnum,
				Help: `Column to rank, chart and total by, e.g. "devengado".`},
			core.Param{Name: "top", Type: core.TypeInt, Default: 10, Min: core.IntPtr(1), Max: core.IntPtr(50),
				Help: "Rows in the chart and rankings, e.g. 15."},
			core.Param{Name: "max", Type: core.TypeInt, Default: 1000, Min: core.IntPtr(1), Max: core.IntPtr(20000),
				Help: "Maximum data rows in the report, e.g. 300."},
			core.Param{Name: "dest", Type: core.TypeString, Short: "d", CLIOnly: true,
				Help: "Full destination path (CLI only); overrides the output directory and nombre."},
		),
		RowFields: []string{"archivo", "formato", "filas", "bytes"},
		Network:   true,
		Examples: []string{
			"mef reporte --by pliego --where nivel=E --format xlsx",
			`mef reporte --source serie --by departamento-meta --from 2022 --to 2026 --format html --nombre inversion-regional`,
			`mef reporte --source sql --query "SELECT PLIEGO_NOMBRE, SUM(DEVENGADO_2016) devengado FROM gasto GROUP BY 1 ORDER BY 2 DESC" --format md`,
		},
		Run: m.runReporte,
	}
}

func (m *Module) runReporte(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	source, format := in.Str("source"), in.Str("format")
	maxRows := in.Int("max")
	metric := in.Str("metric")

	var (
		rows    *capture
		res     *core.Result
		err     error
		defName string
		defTtl  string
	)
	switch source {
	case "gasto":
		rows, res, err = m.gastoRows(ctx, env, in, maxRows)
		if err == nil {
			defName = fmt.Sprintf("gasto-%v-%v", res.Meta["group_by"], res.Meta["year"])
			defTtl = fmt.Sprintf("Ejecución del gasto por %v, %v", res.Meta["group_header"], res.Meta["year"])
		}
	case "serie":
		args := map[string]any{"by": in.Str("by"), "type": in.Str("type"), "max": maxRows}
		if w := in.List("where"); len(w) > 0 {
			args["where"] = w
		}
		if v := in.Int("from"); v != 0 {
			args["from"] = v
		}
		if v := in.Int("to"); v != 0 {
			args["to"] = v
		}
		rows, res, err = runSub(ctx, env, m.A.Command("serie"), args, maxRows)
		if err == nil {
			defName = fmt.Sprintf("serie-%v-%v-%v", res.Meta["group_by"], res.Meta["from"], res.Meta["to"])
			defTtl = fmt.Sprintf("Serie de ejecución por %v, %v–%v", res.Meta["group_by"], res.Meta["from"], res.Meta["to"])
		}
	case "sql":
		q := strings.TrimSpace(in.Str("query"))
		if q == "" {
			return nil, core.Usagef("Pass the SELECT in --query, or use --source gasto.", "source=sql needs a query")
		}
		rows, res, err = runSub(ctx, env, m.L.Command("sql"), map[string]any{"query": q, "max": maxRows}, maxRows)
		defName, defTtl = "consulta-sql", "Consulta SQL sobre datos del MEF"
	default:
		return nil, core.Usagef("", "unknown source %q", source)
	}
	if err != nil {
		return nil, err
	}
	if len(rows.rows) == 0 {
		return nil, core.NotFoundf("Check the filters: there is nothing to report.", "the query returned no rows")
	}

	cols := rows.cols
	if len(cols) == 0 {
		cols = renderColumns(rows.rows)
	}
	doc := &report.Doc{
		Title:     firstNonEmpty(in.Str("titulo"), defTtl, "Reporte MEF"),
		Source:    sourceLabel(source),
		Generated: env.Now(),
		Columns:   cols,
		Rows:      rows.rows,
		Meta:      map[string]any{"fuente_datos": source, "formato": format},
	}
	if res != nil {
		if v, ok := res.Meta["last_updated"].(string); ok {
			doc.CutOff = v
		}
		if v, ok := res.Meta["source_url"].(string); ok {
			doc.SourceURL = v
		}
		for _, k := range []string{"year", "from", "to", "group_by", "group_header", "type", "sort", "groups", "pages"} {
			if v, ok := res.Meta[k]; ok && v != nil && v != "" {
				doc.Meta[k] = v
			}
		}
		if fs, ok := res.Meta["filters"].([]map[string]any); ok && len(fs) > 0 {
			var parts []string
			for _, f := range fs {
				parts = append(parts, fmt.Sprintf("%v=%v", f["dimension"], f["valor"]))
			}
			doc.Meta["filtros"] = strings.Join(parts, ", ")
			doc.Subtitle = "Filtros: " + strings.Join(parts, ", ")
		}
		doc.Notes = append(doc.Notes, res.Notes...)
	}

	// Only analyse when the metric is actually present: a SQL report with its
	// own columns should not get invented percentages.
	if hasColumn(cols, metric) {
		doc.Analysis = analysis.Analyze(rows.rows, analysis.Options{Metric: metric, TopN: in.Int("top")})
		doc.Charts = []report.Chart{report.TopChart(doc.Analysis, fmt.Sprintf("Top %d por %s", in.Int("top"), metric), in.Int("top"))}
	}
	if res != nil {
		if t, ok := res.Data.(map[string]any); ok {
			if totals, ok := t["totales"]; ok {
				doc.Totals = totalsRow(totals, cols)
			}
		}
	}

	dir, name := paths.OutputDir(), report.SafeName(in.Str("nombre"), defName)
	if dest := in.Str("dest"); dest != "" {
		dir = filepath.Dir(dest)
		base := filepath.Base(dest)
		name = report.SafeName(strings.TrimSuffix(base, filepath.Ext(base)), defName)
	}
	path, size, err := report.Write(dir, name, format, doc)
	if err != nil {
		return nil, err
	}

	sheets := ""
	if format == "xlsx" {
		sheets = "Resumen, Datos, Fuente"
	}
	row := core.Row{"archivo": path, "formato": format, "filas": len(rows.rows), "bytes": size}
	if sheets != "" {
		row["hojas"] = sheets
	}
	sink.SetColumns([]string{"archivo", "formato", "filas", "bytes"})
	if err := sink.Emit(row); err != nil {
		return nil, err
	}
	data := map[string]any{"archivo": path, "columnas": cols}
	if doc.Analysis != nil {
		data["resumen"] = doc.Analysis.Summary
		data["hallazgos"] = doc.Analysis.Findings
	}
	out := &core.Result{
		Columns: []string{"archivo", "formato", "filas", "bytes"},
		Data:    data,
		Meta:    map[string]any{"directorio": dir, "fuente_datos": source, "titulo": doc.Title, "last_updated": doc.CutOff},
	}
	out.Notes = append(out.Notes, "reporte escrito en "+path)
	return out, nil
}

func sourceLabel(source string) string {
	switch source {
	case "serie":
		return "MEF — Consulta Amigable (serie anual)"
	case "sql":
		return "MEF — Datos Abiertos (base local sincronizada)"
	}
	return "MEF — Consulta Amigable"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func hasColumn(cols []string, name string) bool {
	for _, c := range cols {
		if c == name {
			return true
		}
	}
	return false
}

// renderColumns derives a column order when a command did not announce one.
func renderColumns(rows []core.Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		for k := range r {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// totalsRow adapts mef_gasto's totals struct to a table row.
func totalsRow(totals any, cols []string) core.Row {
	m, ok := totals.(amigable.Line)
	if !ok {
		return nil
	}
	row := core.Row{
		"codigo": "", "nombre": "TOTAL", "pia": m.PIA, "pim": m.PIM, "certificacion": m.Certificado,
		"compromiso_anual": m.CompAnual, "compromiso_mensual": m.CompMensual,
		"devengado": m.Devengado, "girado": m.Girado, "avance_pct": m.AvancePct,
	}
	out := core.Row{}
	for _, c := range cols {
		if v, ok := row[c]; ok {
			out[c] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
