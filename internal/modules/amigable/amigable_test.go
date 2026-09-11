package amigable

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
	"github.com/JOSETRA44/mef-mcp/internal/textenc"
)

func page(t *testing.T, name string) Page {
	t.Helper()
	p, appErr := ParsePage(textenc.DecodeMixed(testutil.Fixture(t, name)))
	if appErr {
		t.Fatalf("%s flagged as error page", name)
	}
	return p
}

// Figures captured 2026-09-10 and cross-checked against the navigator's own
// TOTAL row: PIA 257,561,619,143 · PIM 287,487,003,991 · avance 62.2 %.
func TestParseNivelMatchesPublishedTotals(t *testing.T) {
	p := page(t, "nivel_2026.html")
	if len(p.Lines) != 3 {
		t.Fatalf("want 3 rows (E, M, R — including class=alt rows), got %d", len(p.Lines))
	}
	var pia, pim, dev float64
	for _, l := range p.Lines {
		pia, pim, dev = pia+l.PIA, pim+l.PIM, dev+l.Devengado
	}
	if pia != 257561619143 || pim != 287487003991 {
		t.Fatalf("totals PIA=%.0f PIM=%.0f", pia, pim)
	}
	if got := math.Round(dev/pim*1000) / 10; got != 62.2 {
		t.Fatalf("avance %.1f", got)
	}
	e := p.Lines[0]
	if e.Code != "E" || e.Name != "GOBIERNO NACIONAL" || e.Certificado != 151520534981.83 || e.AvancePct != 66 {
		t.Fatalf("row E: %+v", e)
	}
	if p.LastUpdated != "2026-09-09" || p.Dimension != "Nivel de Gobierno" {
		t.Fatalf("meta: updated=%q header=%q", p.LastUpdated, p.Dimension)
	}
}

func TestParseMesHasNoBudgetColumns(t *testing.T) {
	p := page(t, "mes_2026.html")
	if len(p.Lines) == 0 {
		t.Fatal("month rows were dropped")
	}
	first := p.Lines[0]
	if !first.NoBudget || first.Name != "Enero" || first.Devengado == 0 {
		t.Fatalf("enero: %+v", first)
	}
	row := lineRow(first)
	if row["pim"] != nil || row["avance_pct"] != nil {
		t.Fatal("absent PIM must be null, not zero")
	}
}

func TestParseRepairsMixedEncoding(t *testing.T) {
	p := page(t, "funcion_2026.html")
	if p.Dimension != "Función" {
		t.Fatalf("header %q — cp1252 bytes not repaired", p.Dimension)
	}
	for _, l := range p.Lines {
		if strings.ContainsRune(l.Name, '�') || strings.Contains(l.Name, "<img") {
			t.Fatalf("dirty name %q", l.Name)
		}
	}
}

func TestParseSectorDrillDown(t *testing.T) {
	p := page(t, "sector_E_2026.html")
	if len(p.Lines) < 20 {
		t.Fatalf("sectors under E: %d", len(p.Lines))
	}
	if p.Lines[0].Code != "01" || p.Lines[0].Name != "PRESIDENCIA CONSEJO MINISTROS" {
		t.Fatalf("first sector %+v", p.Lines[0])
	}
}

func TestBuildQueryOrderAndAliases(t *testing.T) {
	q, err := BuildQuery(2026, "proyecto", "Función", [][2]string{{"nivel", "nacional"}, {"sector", "01"}}, 2, 500)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, kv := range q.Pairs() {
		keys = append(keys, kv[0]+"="+kv[1])
	}
	want := "_uhc=yes 0= 1=E 2=01 8= y=2026 ap=Proyecto cpage=2 psize=500"
	if strings.Join(keys, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(keys, " "), want)
	}
}

func TestBuildQueryRejectsBadInput(t *testing.T) {
	cases := []struct {
		by    string
		where [][2]string
	}{
		{"nope", nil},
		{"sector", [][2]string{{"sector", "01"}}},              // group == filter
		{"pliego", [][2]string{{"sector", ""}}},                // empty key
		{"pliego", [][2]string{{"sector", "01"}, {"2", "02"}}}, // same dim twice via code
	}
	for _, c := range cases {
		_, err := BuildQuery(2026, "", c.by, c.where, 1, 10)
		var e *core.Error
		if !errors.As(err, &e) || e.Code != core.CodeUsage {
			t.Errorf("%+v: want usage error, got %v", c, err)
		}
	}
}

func TestEveryDimensionResolvesByAliasAndCode(t *testing.T) {
	codes := map[int]bool{}
	for _, d := range Dimensions {
		if codes[d.Code] {
			t.Fatalf("duplicate code %d", d.Code)
		}
		codes[d.Code] = true
		for _, name := range append([]string{d.Alias, fmt.Sprint(d.Code)}, d.Aliases...) {
			got, err := Lookup(name)
			if err != nil || got.Code != d.Code {
				t.Errorf("%q → %v %v", name, got.Code, err)
			}
		}
	}
}

// Bulk paging: terminates, honours max, never duplicates rows (ARSENAL-SPEC §6.6).
func TestGastoPagingHonoursMaxWithoutDuplicates(t *testing.T) {
	body := func(offset, n int) []byte {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			code := fmt.Sprintf("%04d", offset+i)
			fmt.Fprintf(&sb, `<tr id="tr%d"><td><input type="radio" name="grp1" value="%s/10/20/5/4/3/2/6"></td><td align="left">%s: ROW</td></tr>`, i, code, code)
		}
		return []byte(sb.String())
	}
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: func(req core.Request, call int) ([]byte, error) {
		q := testutil.Query(t, testutil.Target(req))
		page := 0
		fmt.Sscan(q.Get("cpage"), &page)
		if page > 3 {
			return body(0, 0), nil
		}
		n := 4
		if page == 3 {
			n = 2 // short last page ends the harvest
		}
		return body((page-1)*4, n), nil
	}}}}
	m := &Module{Endpoint: "https://fake/Navegar_7.aspx"}
	cmd := m.gasto()
	env := testutil.Env(t, fake)

	sink := &testutil.Sink{}
	res, err := cmd.Run(context.Background(), env, testutil.Input(t, cmd, map[string]any{"by": "sector", "all": true, "page_size": 4}), sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) != 10 || fake.CallCount() != 3 || res.Meta["has_more"] != false {
		t.Fatalf("rows=%d calls=%d meta=%v", len(sink.Rows), fake.CallCount(), res.Meta)
	}
	seen := map[string]bool{}
	for _, r := range sink.Rows {
		c := r["codigo"].(string)
		if seen[c] {
			t.Fatalf("duplicate row %s", c)
		}
		seen[c] = true
	}

	fake2 := &testutil.Fake{Routes: fake.Routes}
	capped := &testutil.Sink{Max: 5}
	_, err = cmd.Run(context.Background(), testutil.Env(t, fake2), testutil.Input(t, cmd, map[string]any{"by": "sector", "all": true, "page_size": 4}), capped)
	if err != nil || len(capped.Rows) != 5 || fake2.CallCount() != 2 {
		t.Fatalf("max: rows=%d calls=%d err=%v", len(capped.Rows), fake2.CallCount(), err)
	}
}

func TestGastoRejectsFutureYear(t *testing.T) {
	cmd := New().gasto()
	_, err := cmd.Run(context.Background(), testutil.Env(t, &testutil.Fake{}), testutil.Input(t, cmd, map[string]any{"year": 2030}), &testutil.Sink{})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
}
