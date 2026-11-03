package amigable

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

// line renders one navigator row the way Consulta Amigable does: the exact
// amounts live in the radio value, ordered
// code/PIA/PIM/compAnual/compMensual/devengado/girado/certificación.
func line(i int, code, name string, pim, dev float64) string {
	return fmt.Sprintf(
		`<tr id="tr%d"><td><input type="radio" name="grp1" value="%s/0/%.2f/0/0/%.2f/0/0"></td>`+
			`<td align="left">%s: %s</td></tr>`, i, code, pim, dev, code, name)
}

func pageOf(rows ...string) []byte { return []byte(strings.Join(rows, "")) }

func codeOf(err error) core.ErrorCode {
	var e *core.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSerieBuildsOneRowPerYearWithVariation(t *testing.T) {
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: func(req core.Request, _ int) ([]byte, error) {
		switch testutil.Query(t, testutil.Target(req)).Get("y") {
		case "2024":
			return pageOf(line(0, "01", "SALUD", 1000, 100), line(1, "02", "EDUCACION", 500, 500)), nil
		case "2025":
			return pageOf(line(0, "01", "SALUD", 2000, 150)), nil
		}
		return pageOf(), nil
	}}}}
	cmd := (&Module{Endpoint: "https://fake/Navegar_7.aspx"}).serie()
	sink := &testutil.Sink{}
	res, err := cmd.Run(context.Background(), testutil.Env(t, fake),
		testutil.Input(t, cmd, map[string]any{"by": "sector", "from": 2024, "to": 2025}), sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) != 3 {
		t.Fatalf("want 3 rows (2 years for 01, 1 for 02), got %d: %v", len(sink.Rows), sink.Rows)
	}
	// Rows are grouped by name, years ascending, so EDUCACION comes first.
	if sink.Rows[0]["nombre"] != "EDUCACION" || sink.Rows[1]["codigo"] != "01" {
		t.Fatalf("order: %v", sink.Rows)
	}
	first, second := sink.Rows[1], sink.Rows[2]
	if first["anio"] != 2024 || first["variacion_pct"] != nil {
		t.Fatalf("first year must have no variation: %v", first)
	}
	if second["anio"] != 2025 || second["variacion_pct"] != 50.0 {
		t.Fatalf("variation: %v", second)
	}
	perYear, ok := res.Data.(map[string]any)["por_anio"].([]map[string]any)
	if !ok || len(perYear) != 2 || perYear[0]["devengado"] != 600.0 {
		t.Fatalf("per-year totals: %v", res.Data)
	}
	if res.Meta["from"] != 2024 || res.Meta["to"] != 2025 || res.Meta["years"] != 2 {
		t.Fatalf("meta: %v", res.Meta)
	}
	trend := res.Data.(map[string]any)["tendencia"]
	if fmt.Sprint(trend) == "[]" {
		t.Fatal("trend findings missing")
	}
}

func TestSerieRejectsImpossibleRanges(t *testing.T) {
	cmd := New().serie()
	env := testutil.Env(t, &testutil.Fake{})
	for _, args := range []map[string]any{
		{"from": 1990, "to": 2026},
		{"from": 2000, "to": 2030},
		{"from": 1999, "to": 2026}, // 28 years: beyond the span cap
	} {
		_, err := cmd.Run(context.Background(), env, testutil.Input(t, cmd, args), &testutil.Sink{})
		if codeOf(err) != core.CodeUsage {
			t.Errorf("%v: want usage error, got %v", args, err)
		}
	}
}

// Names as the navigator really prints them: it abbreviates some pliegos to
// "M. DE …" and spells others out, in the same listing.
func entidadesFake(t *testing.T) *testutil.Fake {
	return &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: testutil.Static(pageOf(
		line(0, "011", "M. DE SALUD", 9000, 5000),
		line(1, "010", "MINISTERIO DE EDUCACIÓN", 8000, 7000),
		line(2, "458", "GOBIERNO REGIONAL DE PUNO", 3000, 1000),
		line(3, "300001", "MUNICIPALIDAD PROVINCIAL DE CHACHAPOYAS", 2000, 800),
	))}}}
}

func TestEntidadesReturnsReadyToUseFilter(t *testing.T) {
	cmd := (&Module{Endpoint: "https://fake/Navegar_7.aspx"}).entidades()
	sink := &testutil.Sink{}
	res, err := cmd.Run(context.Background(), testutil.Env(t, entidadesFake(t)),
		testutil.Input(t, cmd, map[string]any{"text": "ministerio salud", "kind": "pliego", "year": 2026}), sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) != 1 {
		t.Fatalf("rows: %v", sink.Rows)
	}
	row := sink.Rows[0]
	if row["filtro"] != "pliego=011" || row["dimension"] != "pliego" || row["devengado"] != 5000.0 {
		t.Fatalf("row: %v", row)
	}
	if row["nombre"] != "M. DE SALUD" {
		t.Fatalf("the abbreviated form must be matched: %v", row["nombre"])
	}
	if res.Data == nil || !strings.Contains(fmt.Sprint(res.Data), "pliego=011") {
		t.Fatalf("data should show how to use the filter: %v", res.Data)
	}
}

func TestEntidadesMatchingRules(t *testing.T) {
	cmd := (&Module{Endpoint: "https://fake/Navegar_7.aspx"}).entidades()
	run := func(text string) []core.Row {
		sink := &testutil.Sink{}
		if _, err := cmd.Run(context.Background(), testutil.Env(t, entidadesFake(t)),
			testutil.Input(t, cmd, map[string]any{"text": text, "kind": "pliego", "year": 2026}), sink); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		return sink.Rows
	}
	// Accents and case are folded; word order does not matter.
	if got := run("educacion"); len(got) != 1 || got[0]["codigo"] != "010" {
		t.Errorf("accent folding: %v", got)
	}
	if got := run("puno regional"); len(got) != 1 || got[0]["codigo"] != "458" {
		t.Errorf("word order: %v", got)
	}
	// Several matches come back sorted by devengado, biggest first; "M. DE
	// SALUD" counts as a ministerio.
	got := run("ministerio")
	if len(got) != 2 || got[0]["codigo"] != "010" || got[1]["codigo"] != "011" {
		t.Errorf("ranking: %v", got)
	}
	// Connectors are noise: MEF writes "DEL DEPARTAMENTO DE" or nothing.
	if got := run("gobierno regional de puno"); len(got) != 1 || got[0]["codigo"] != "458" {
		t.Errorf("stopwords: %v", got)
	}
	if got := run("municipalidad chachapoyas"); len(got) != 1 || got[0]["codigo"] != "300001" {
		t.Errorf("municipalidad: %v", got)
	}
	// A bare number is matched against codes, with or without padding.
	if got := run("011"); len(got) != 1 || got[0]["nombre"] != "M. DE SALUD" {
		t.Errorf("code search: %v", got)
	}
	if got := run("ministerio de hacienda"); len(got) != 0 {
		t.Errorf("no match expected: %v", got)
	}
}

func TestEntidadesRejectsBadInput(t *testing.T) {
	cmd := (&Module{Endpoint: "https://fake/Navegar_7.aspx"}).entidades()
	env := testutil.Env(t, entidadesFake(t))
	for _, args := range []map[string]any{
		{"text": "a"},
		{"text": "salud", "kind": "no-existe"},
		{"text": "salud", "year": 1990},
	} {
		_, err := cmd.Run(context.Background(), env, testutil.Input(t, cmd, args), &testutil.Sink{})
		if codeOf(err) != core.CodeUsage {
			t.Errorf("%v: want usage error, got %v", args, err)
		}
	}
}

func TestGastoSortsServerSide(t *testing.T) {
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: testutil.Static(pageOf(
		line(0, "01", "MEDIO", 1000, 500),
		line(1, "02", "ALTO", 1000, 900),
		line(2, "03", "BAJO", 1000, 100),
	))}}}
	cmd := (&Module{Endpoint: "https://fake/Navegar_7.aspx"}).gasto()
	order := func(args map[string]any) []string {
		sink := &testutil.Sink{}
		if _, err := cmd.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, cmd, args), sink); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range sink.Rows {
			out = append(out, r["codigo"].(string))
		}
		return out
	}
	if got := order(map[string]any{"by": "sector", "sort": "devengado"}); strings.Join(got, ",") != "02,01,03" {
		t.Errorf("desc by devengado: %v", got)
	}
	if got := order(map[string]any{"by": "sector", "sort": "avance", "desc": false}); strings.Join(got, ",") != "03,01,02" {
		t.Errorf("asc by avance: %v", got)
	}
	if got := order(map[string]any{"by": "sector", "sort": "nombre"}); strings.Join(got, ",") != "01,03,02" {
		t.Errorf("desc by name: %v", got)
	}
	// Sorting plus max is a true top-N, not the first N of MEF's own order.
	sink := &testutil.Sink{Max: 1}
	if _, err := cmd.Run(context.Background(), testutil.Env(t, fake),
		testutil.Input(t, cmd, map[string]any{"by": "sector", "sort": "devengado", "max": 1}), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) != 1 || sink.Rows[0]["codigo"] != "02" {
		t.Fatalf("top-1: %v", sink.Rows)
	}
}
