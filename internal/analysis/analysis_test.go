package analysis

import (
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// rows mimic one mef_gasto grouping: two big spenders, one laggard, one with
// budget and no execution at all, and one with no budget (not a laggard).
func rows() []core.Row {
	return []core.Row{
		{"codigo": "01", "nombre": "SALUD", "pim": 1000.0, "devengado": 900.0, "avance_pct": 90.0},
		{"codigo": "02", "nombre": "EDUCACIÓN", "pim": 2000.0, "devengado": 1000.0, "avance_pct": 50.0},
		{"codigo": "03", "nombre": "CULTURA", "pim": 500.0, "devengado": 50.0, "avance_pct": 10.0},
		{"codigo": "04", "nombre": "FICTICIO", "pim": 300.0, "devengado": 0.0, "avance_pct": 0.0},
		{"codigo": "05", "nombre": "SIN PIM", "pim": 0.0, "devengado": 0.0, "avance_pct": nil},
	}
}

func TestAnalyzeTotalsAndRankings(t *testing.T) {
	r := Analyze(rows(), Options{})
	if r.Total != 1950 || r.TotalBudget != 3800 {
		t.Fatalf("totals: %v / %v", r.Total, r.TotalBudget)
	}
	if r.AvancePct != 51.3 {
		t.Fatalf("avance %v, want 51.3", r.AvancePct)
	}
	if r.Top[0].Name != "EDUCACIÓN" || r.Top[0].Share != 51.3 {
		t.Fatalf("top: %+v", r.Top[0])
	}
	// The laggard is the lowest avance *with* budget; a row with PIM 0 is not
	// behind, and a row with budget and zero execution is reported apart.
	if r.Bottom[0].Name != "CULTURA" {
		t.Fatalf("bottom: %+v", r.Bottom)
	}
	if len(r.Idle) != 1 || r.Idle[0].Name != "FICTICIO" || r.IdleBudget != 300 {
		t.Fatalf("idle: %+v (%v)", r.Idle, r.IdleBudget)
	}
	if r.Median != 50 {
		t.Fatalf("median %v", r.Median)
	}
	if r.Top5Share != 100 {
		t.Fatalf("top5 share %v", r.Top5Share)
	}
}

func TestAnalyzeRespectsMetricAndTopN(t *testing.T) {
	r := Analyze(rows(), Options{Metric: "pim", TopN: 2})
	if r.Total != 3800 || len(r.Top) != 2 || r.Top[0].Name != "EDUCACIÓN" {
		t.Fatalf("by pim: total=%v top=%+v", r.Total, r.Top)
	}
}

func TestFindingsAndSummaryAreQuotable(t *testing.T) {
	r := Analyze(rows(), Options{})
	kinds := map[string]bool{}
	for _, f := range r.Findings {
		kinds[f.Kind] = true
		if f.Title == "" || f.Detail == "" {
			t.Errorf("finding without text: %+v", f)
		}
	}
	for _, want := range []string{"total", "mayor_monto", "menor_avance", "sin_ejecucion", "distribucion"} {
		if !kinds[want] {
			t.Errorf("missing finding %q", want)
		}
	}
	if !strings.Contains(r.Summary, "S/") || !strings.Contains(r.Summary, "avance") {
		t.Fatalf("summary: %q", r.Summary)
	}
	if got := FindingRows(r); len(got) != len(r.Findings) || got[0]["tipo"] != "total" {
		t.Fatalf("finding rows: %v", got)
	}
}

func TestAnalyzeEmptyAndMissingMetric(t *testing.T) {
	if r := Analyze(nil, Options{}); r.Rows != 0 || !strings.Contains(r.Summary, "Sin filas") {
		t.Fatalf("empty: %+v", r)
	}
	// A SQL report with its own columns must not produce invented figures.
	r := Analyze([]core.Row{{"a": "x"}, {"a": "y"}}, Options{})
	if r.Total != 0 || len(r.Top) != 0 {
		t.Fatalf("missing metric: %+v", r)
	}
}

func TestNumAcceptsDatastoreStrings(t *testing.T) {
	cases := map[any]float64{
		"1234.5":     1234.5,
		"1,234.5":    1234.5,
		" 42 ":       42,
		float64(7):   7,
		int(7):       7,
		int64(7):     7,
		float32(0.5): 0.5,
	}
	for in, want := range cases {
		got, ok := Num(in)
		if !ok || got != want {
			t.Errorf("Num(%v) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []any{nil, "", "   ", "n/a", true, []string{"1"}} {
		if _, ok := Num(in); ok {
			t.Errorf("Num(%v) should fail", in)
		}
	}
}

func TestPctAndSoles(t *testing.T) {
	if Pct(1, 3) != 33.3 || Pct(1, 0) != 0 {
		t.Fatalf("pct: %v %v", Pct(1, 3), Pct(1, 0))
	}
	if Soles(1234567.5) != "S/ 1,234,567.50" {
		t.Fatalf("soles: %q", Soles(1234567.5))
	}
}

func TestTrendDescribesGrowthAndFall(t *testing.T) {
	f := Trend([]string{"2024", "2025", "2026"}, []float64{100, 150, 75})
	if len(f) != 2 {
		t.Fatalf("want 2 comparisons, got %d", len(f))
	}
	if f[0].Value != 50 || !strings.Contains(f[0].Detail, "crece") || f[0].Title != "2024 → 2025" {
		t.Fatalf("growth: %+v", f[0])
	}
	if f[1].Value != -50 || !strings.Contains(f[1].Detail, "cae 50.0") {
		t.Fatalf("fall: %+v", f[1])
	}
	// A zero base cannot yield a percentage; it is skipped, not reported as 0.
	if got := Trend([]string{"a", "b"}, []float64{0, 10}); len(got) != 0 {
		t.Fatalf("zero base: %+v", got)
	}
}
