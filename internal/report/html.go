package report

import (
	"fmt"
	"html"
	"os"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/render"
)

// style is deliberately small and self-contained: the page must open from a
// file share with no network, and print cleanly to PDF from the browser.
const style = `
:root{--bg:#ffffff;--fg:#14202c;--muted:#5b6b7c;--line:#dfe6ee;--accent:#1f4e79;--bar:#2e74b5;--good:#1b7f4d;--warn:#b45309;--bad:#a4262c}
@media (prefers-color-scheme:dark){:root{--bg:#10151b;--fg:#e8eef5;--muted:#9fb0c0;--line:#28323d;--accent:#8ab4f8;--bar:#4a90d9}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.55 -apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;padding:0 16px}
main{max-width:980px;margin:0 auto;padding:32px 0 56px}
h1{font-size:1.6rem;margin:0 0 4px;letter-spacing:-.01em}
h2{font-size:1.05rem;margin:32px 0 10px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}
.sub{color:var(--muted);margin:0 0 24px}
.summary{border-left:3px solid var(--accent);padding:10px 0 10px 14px;margin:0 0 24px}
.kpis{display:flex;flex-wrap:wrap;gap:12px;margin:0 0 8px}
.kpi{flex:1 1 180px;border:1px solid var(--line);border-radius:10px;padding:12px 14px}
.kpi .k{color:var(--muted);font-size:.78rem;text-transform:uppercase;letter-spacing:.05em}
.kpi .v{font-size:1.25rem;font-variant-numeric:tabular-nums;margin-top:2px}
ul.find{list-style:none;padding:0;margin:0}
ul.find li{border-bottom:1px solid var(--line);padding:9px 0}
ul.find li b{color:var(--accent)}
.tablewrap{overflow-x:auto;border:1px solid var(--line);border-radius:10px}
table{border-collapse:collapse;width:100%;font-size:.9rem}
th,td{padding:7px 10px;text-align:left;white-space:nowrap}
th{position:sticky;top:0;background:var(--bg);border-bottom:2px solid var(--line);font-size:.78rem;text-transform:uppercase;letter-spacing:.04em;color:var(--muted)}
td.num{text-align:right;font-variant-numeric:tabular-nums}
tbody tr:nth-child(even){background:color-mix(in srgb,var(--line) 28%,transparent)}
tr.total td{border-top:2px solid var(--line);font-weight:600}
.chart .cat{font-size:11px;fill:var(--fg);text-anchor:end}
.chart .val{font-size:11px;fill:var(--muted)}
.chart .bar{fill:var(--bar)}
footer{margin-top:36px;padding-top:14px;border-top:1px solid var(--line);color:var(--muted);font-size:.82rem}
footer a{color:var(--accent);word-break:break-all}
.pill{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:1px 8px;font-size:.78rem;color:var(--muted);margin:0 6px 6px 0}
@media print{body{padding:0}main{max-width:none}th{position:static}}
`

func writeHTML(path string, d *Doc) error {
	var sb strings.Builder
	esc := html.EscapeString
	sb.WriteString("<!doctype html>\n<html lang=\"es\">\n<head>\n<meta charset=\"utf-8\">\n")
	sb.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	fmt.Fprintf(&sb, "<title>%s</title>\n<style>%s</style>\n</head>\n<body>\n<main>\n", esc(d.Title), style)
	fmt.Fprintf(&sb, "<h1>%s</h1>\n", esc(d.Title))
	if d.Subtitle != "" {
		fmt.Fprintf(&sb, `<p class="sub">%s</p>`+"\n", esc(d.Subtitle))
	}

	if a := d.Analysis; a != nil {
		if a.Summary != "" {
			fmt.Fprintf(&sb, `<p class="summary">%s</p>`+"\n", esc(a.Summary))
		}
		sb.WriteString(`<div class="kpis">`)
		kpi := func(k, v string) {
			fmt.Fprintf(&sb, `<div class="kpi"><div class="k">%s</div><div class="v">%s</div></div>`, esc(k), esc(v))
		}
		kpi("PIM", analysis.Soles(a.TotalBudget))
		kpi("Devengado", analysis.Soles(a.Total))
		kpi("Avance", fmt.Sprintf("%.1f %%", a.AvancePct))
		kpi("Filas", render.Cell(a.Rows, true))
		if a.IdleBudget > 0 {
			kpi("Sin ejecutar", analysis.Soles(a.IdleBudget))
		}
		sb.WriteString("</div>\n")
	}

	for _, c := range d.Charts {
		if svg := barsSVG(c); svg != "" {
			fmt.Fprintf(&sb, "<h2>%s</h2>\n%s\n", esc(c.Title), svg)
		}
	}

	if a := d.Analysis; a != nil && len(a.Findings) > 0 {
		sb.WriteString("<h2>Hallazgos</h2>\n<ul class=\"find\">\n")
		for _, f := range a.Findings {
			fmt.Fprintf(&sb, "<li><b>%s.</b> %s</li>\n", esc(f.Title), esc(f.Detail))
		}
		sb.WriteString("</ul>\n")
	}

	if len(d.Rows) > 0 {
		fmt.Fprintf(&sb, "<h2>Datos (%s filas)</h2>\n<div class=\"tablewrap\">\n<table>\n<thead><tr>", render.Cell(len(d.Rows), true))
		for _, c := range d.Columns {
			fmt.Fprintf(&sb, "<th>%s</th>", esc(d.Header(c)))
		}
		sb.WriteString("</tr></thead>\n<tbody>\n")
		for _, row := range d.Rows {
			sb.WriteString("<tr>")
			for _, c := range d.Columns {
				sb.WriteString(cellHTML(row[c]))
			}
			sb.WriteString("</tr>\n")
		}
		if d.Totals != nil {
			sb.WriteString(`<tr class="total">`)
			for _, c := range d.Columns {
				sb.WriteString(cellHTML(d.Totals[c]))
			}
			sb.WriteString("</tr>\n")
		}
		sb.WriteString("</tbody>\n</table>\n</div>\n")
	}

	if len(d.Notes) > 0 {
		sb.WriteString("<h2>Notas</h2>\n<ul class=\"find\">\n")
		for _, n := range d.Notes {
			fmt.Fprintf(&sb, "<li>%s</li>\n", esc(n))
		}
		sb.WriteString("</ul>\n")
	}

	if pairs := d.metaPairs(); len(pairs) > 0 {
		sb.WriteString("<h2>Consulta</h2>\n<div>")
		for _, p := range pairs {
			fmt.Fprintf(&sb, `<span class="pill">%s: %s</span>`, esc(p[0]), esc(p[1]))
		}
		sb.WriteString("</div>\n")
	}

	sb.WriteString("<footer>\n")
	for _, p := range d.provenance() {
		fmt.Fprintf(&sb, "<div>%s</div>\n", esc(p))
	}
	if d.SourceURL != "" {
		fmt.Fprintf(&sb, `<div><a href="%s">%s</a></div>`+"\n", esc(d.SourceURL), esc(d.SourceURL))
	}
	sb.WriteString("</footer>\n</main>\n</body>\n</html>\n")
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func cellHTML(v any) string {
	if _, ok := analysis.Num(v); ok {
		if _, isStr := v.(string); !isStr {
			return `<td class="num">` + html.EscapeString(render.Cell(v, true)) + `</td>`
		}
	}
	return "<td>" + html.EscapeString(render.Cell(v, false)) + "</td>"
}
