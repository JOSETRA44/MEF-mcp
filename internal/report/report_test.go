package report

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/core"
)

func doc() *Doc {
	rows := []core.Row{
		{"codigo": "01", "nombre": "SALUD | Ñuñoa", "pim": 1000.0, "devengado": 900.0, "avance_pct": 90.0},
		{"codigo": "02", "nombre": "EDUCACIÓN", "pim": 2000.0, "devengado": 1000.0, "avance_pct": 50.0},
	}
	a := analysis.Analyze(rows, analysis.Options{})
	return &Doc{
		Title:     "Ejecución del gasto por sector, 2026",
		Subtitle:  "Filtros: nivel=E",
		Source:    "MEF — Consulta Amigable",
		SourceURL: "https://apps5.mineco.gob.pe/transparencia/Navegador/Navegar_7.aspx?_uhc=yes",
		CutOff:    "2026-09-10",
		Generated: time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC),
		Columns:   []string{"codigo", "nombre", "pim", "devengado", "avance_pct"},
		Rows:      rows,
		Totals:    core.Row{"nombre": "TOTAL", "pim": 3000.0, "devengado": 1900.0, "avance_pct": 63.3},
		Analysis:  a,
		Charts:    []Chart{TopChart(a, "Top 10 por devengado", 10)},
		Meta:      map[string]any{"year": 2026, "group_by": "sector"},
		Notes:     []string{"2026 sigue en curso"},
	}
}

func write(t *testing.T, format string) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path, size, err := Write(dir, "informe", format, doc())
	if err != nil {
		t.Fatalf("%s: %v", format, err)
	}
	if size == 0 {
		t.Fatalf("%s: empty file", format)
	}
	if filepath.Ext(path) != "."+format {
		t.Fatalf("%s: path %s", format, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, b
}

func TestEveryFormatCarriesFiguresAndProvenance(t *testing.T) {
	for _, f := range Formats {
		_, b := write(t, f)
		body := string(b)
		if f == "xlsx" {
			continue // binary; checked below
		}
		for _, want := range []string{"Ejecución del gasto por sector, 2026", "Consulta Amigable", "2026-09-10", "Ñuñoa"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", f, want)
			}
		}
	}
}

func TestMarkdownUsesHumanHeadersAndEscapes(t *testing.T) {
	_, b := write(t, "md")
	body := string(b)
	if !strings.Contains(body, "| Código | Concepto | PIM | Devengado | Avance % |") {
		t.Fatalf("human headers missing:\n%s", body)
	}
	if !strings.Contains(body, `SALUD \| Ñuñoa`) {
		t.Error("pipe inside a value must be escaped")
	}
	if !strings.Contains(body, "## Hallazgos") || !strings.Contains(body, "TOTAL") {
		t.Error("findings or totals row missing")
	}
}

// The HTML page must open from a file share with no internet: no external
// script, stylesheet or image may sneak in.
func TestHTMLIsSelfContained(t *testing.T) {
	_, b := write(t, "html")
	body := string(b)
	for _, forbidden := range []string{"<script", "cdn.", "http://", "googleapis", "<img"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page is not self-contained: found %q", forbidden)
		}
	}
	if !strings.HasPrefix(body, "<!doctype html>") {
		t.Error("missing doctype")
	}
	if !strings.Contains(body, "<svg") || !strings.Contains(body, "class=\"bar\"") {
		t.Error("chart not rendered inline")
	}
	for _, want := range []string{`class="kpi"`, "Avance", "63.3", "prefers-color-scheme"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "https://apps5.mineco.gob.pe") == false {
		t.Error("source URL must be printed for traceability")
	}
}

func TestXLSXSheetsTypesAndFilters(t *testing.T) {
	path, _ := write(t, "xlsx")
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); len(got) != 3 || got[0] != sheetSummary || got[1] != sheetData || got[2] != sheetSource {
		t.Fatalf("sheets: %v", got)
	}
	head, _ := f.GetCellValue(sheetData, "A1")
	if head != "Código" {
		t.Fatalf("header A1 = %q", head)
	}
	// Amounts must be real numbers, or Excel cannot sum them. A numeric cell
	// carries no `t` attribute (number is the default), so excelize reports
	// either CellTypeNumber or CellTypeUnset — never a string.
	cellType, _ := f.GetCellType(sheetData, "C2")
	if cellType != excelize.CellTypeNumber && cellType != excelize.CellTypeUnset {
		t.Fatalf("PIM cell type %v, want a number", cellType)
	}
	raw, _ := f.GetCellValue(sheetData, "C2", excelize.Options{RawCellValue: true})
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		t.Fatalf("PIM cell %q is not numeric: %v", raw, err)
	}
	if v, _ := f.GetCellValue(sheetData, "B2"); v != "SALUD | Ñuñoa" {
		t.Fatalf("B2 = %q", v)
	}
	// The totals row sits right after the data.
	if v, _ := f.GetCellValue(sheetData, "B4"); v != "TOTAL" {
		t.Fatalf("totals row: %q", v)
	}
	title, _ := f.GetCellValue(sheetSummary, "A1")
	if title != "Ejecución del gasto por sector, 2026" {
		t.Fatalf("summary title %q", title)
	}
	if n, err := f.GetRows(sheetSource); err != nil || len(n) < 3 {
		t.Fatalf("source sheet rows: %v %v", len(n), err)
	}
}

func TestWriteRejectsUnknownFormat(t *testing.T) {
	if _, _, err := Write(t.TempDir(), "x", "pdf", doc()); err == nil {
		t.Fatal("expected a usage error for pdf")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"Ejecución Salud 2026":     "ejecucion-salud-2026",
		"  ../../etc/passwd  ":     "etc-passwd",
		"informe.final.v2":         "informe-final-v2",
		"":                         "fallback",
		"   ":                      "fallback",
		strings.Repeat("a", 120):   strings.Repeat("a", 80),
		"reporte/con\\separadores": "reporte-con-separadores",
	}
	for in, want := range cases {
		if got := SafeName(in, "fallback"); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestColumnClassification(t *testing.T) {
	if !IsAmount("devengado") || !IsAmount("pim") || IsAmount("codigo") {
		t.Error("amount columns")
	}
	if !IsPercent("avance_pct") || !IsPercent("variacion_pct") || IsPercent("pim") {
		t.Error("percent columns")
	}
	d := &Doc{Labels: map[string]string{"x": "Equis"}}
	if d.Header("x") != "Equis" || d.Header("devengado") != "Devengado" || d.Header("algo_raro") != "Algo raro" {
		t.Errorf("headers: %q %q %q", d.Header("x"), d.Header("devengado"), d.Header("algo_raro"))
	}
}

func TestEmptyChartRendersNothing(t *testing.T) {
	if barsSVG(Chart{Title: "vacío"}) != "" {
		t.Error("a chart with no values must render nothing, not an empty box")
	}
}
