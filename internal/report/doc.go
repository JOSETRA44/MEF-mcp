// Package report turns rows plus an analysis into a deliverable: a
// multi-sheet Excel workbook, a self-contained HTML page or a Markdown memo.
//
// Every format carries the same four things, because a figure without them is
// not usable in public-finance work: what was asked, the numbers, the
// conclusions, and where the data came from with its cut-off date.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Formats are the deliverable formats, in manifest order.
var Formats = []string{"xlsx", "html", "md"}

// Doc is the report model, independent of output format.
type Doc struct {
	Title     string
	Subtitle  string
	Source    string            // e.g. "MEF — Consulta Amigable"
	SourceURL string            // the exact upstream URL used
	CutOff    string            // MEF's own last-update date (ISO)
	Generated time.Time         // when this file was produced
	Columns   []string          // column order of Rows
	Rows      []core.Row        // the table
	Totals    core.Row          // optional totals row
	Analysis  *analysis.Result  // optional conclusions
	Meta      map[string]any    // query parameters and provenance
	Notes     []string          // caveats worth printing
	Charts    []Chart           // rendered as SVG (HTML) and native (XLSX)
	Labels    map[string]string // column → human header
}

// Chart is a simple bar chart; it is all a budget report needs.
type Chart struct {
	Title  string
	Label  string // metric name, e.g. "devengado"
	Names  []string
	Values []float64
}

// TopChart builds a chart from the analysis ranking.
func TopChart(r *analysis.Result, title string, n int) Chart {
	c := Chart{Title: title, Label: r.Metric}
	for i, e := range r.Top {
		if i >= n {
			break
		}
		name := e.Name
		if name == "" {
			name = e.Code
		}
		c.Names = append(c.Names, name)
		c.Values = append(c.Values, e.Value)
	}
	return c
}

// Header returns the human label for a column.
func (d *Doc) Header(col string) string {
	if h, ok := d.Labels[col]; ok {
		return h
	}
	return defaultHeader(col)
}

var headers = map[string]string{
	"codigo": "Código", "codigo_mostrado": "Código MEF", "nombre": "Concepto",
	"pia": "PIA", "pim": "PIM", "certificacion": "Certificación",
	"compromiso_anual": "Compromiso anual", "compromiso_mensual": "Compromiso mensual",
	"devengado": "Devengado", "girado": "Girado", "avance_pct": "Avance %",
	"anio": "Año", "variacion_pct": "Variación %", "dimension": "Dimensión",
	"filtro": "Filtro", "participacion_pct": "Participación %",
}

func defaultHeader(col string) string {
	if h, ok := headers[col]; ok {
		return h
	}
	s := strings.ReplaceAll(col, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// IsPercent reports whether a column holds percentages.
func IsPercent(col string) bool { return strings.HasSuffix(col, "_pct") }

// IsAmount reports whether a column holds soles.
func IsAmount(col string) bool {
	switch col {
	case "pia", "pim", "certificacion", "compromiso_anual", "compromiso_mensual",
		"devengado", "girado", "valor", "presupuesto", "monto_viable",
		"costo_actualizado", "devengado_acumulado":
		return true
	}
	return false
}

var reUnsafe = regexp.MustCompile(`[^\p{L}\p{N}_-]+`)

// SafeName turns a requested report name into a file name that is safe on
// every OS, keeping accents out of the way of shells and zip readers.
func SafeName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}
	repl := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n", " ", "-", ".", "-")
	name = reUnsafe.ReplaceAllString(repl.Replace(strings.ToLower(name)), "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = fallback
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

// Write renders the document in one format and returns the absolute path.
// dir is created if needed; an existing file with the same name is replaced.
func Write(dir, name, format string, d *Doc) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, core.Wrap(core.CodeUsage, err, "Pick a writable directory, or set MEF_OUTPUT.", "cannot create %s", dir)
	}
	path := filepath.Join(dir, name+"."+format)
	var err error
	switch format {
	case "xlsx":
		err = writeXLSX(path, d)
	case "html":
		err = writeHTML(path, d)
	case "md":
		err = writeMarkdown(path, d)
	default:
		return "", 0, core.Usagef("Valid formats: "+strings.Join(Formats, ", "), "unknown report format %q", format)
	}
	if err != nil {
		return "", 0, core.Wrap(core.CodeInternal, err, "", "writing %s: %v", path, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, core.Wrap(core.CodeInternal, err, "", "%v", err)
	}
	abs, _ := filepath.Abs(path)
	return abs, st.Size(), nil
}

// provenance is the footer every format prints.
func (d *Doc) provenance() []string {
	var out []string
	if d.Source != "" {
		out = append(out, "Fuente: "+d.Source)
	}
	if d.CutOff != "" {
		out = append(out, "Última actualización del MEF: "+d.CutOff)
	}
	if !d.Generated.IsZero() {
		out = append(out, "Generado: "+d.Generated.Format("2006-01-02 15:04"))
	}
	out = append(out, "Montos en soles (PEN). Avance % = devengado ÷ PIM.")
	return out
}

func (d *Doc) metaPairs() [][2]string {
	keys := make([]string, 0, len(d.Meta))
	for k := range d.Meta {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, fmt.Sprint(d.Meta[k])})
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
