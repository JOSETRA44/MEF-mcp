package invierte

import (
	"context"
	"errors"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

func fake(t *testing.T) *testutil.Fake {
	return &testutil.Fake{Routes: []testutil.Route{
		{Match: "traeDetInvSSI", Body: "id=2222116", Reply: testutil.Static(testutil.Fixture(t, "ficha_2222116.json"))},
		{Match: "traeDetInvSSI", Reply: testutil.Static([]byte("[]"))},
		{Match: "traeDevengSSI", Body: "tipo=ANIO", Reply: testutil.Static(testutil.Fixture(t, "anual_2222116.json"))},
		{Match: "busInvNombreSSI", Reply: testutil.Static(testutil.Fixture(t, "buscar_carretera.json"))},
		{Match: "traeDatosObraCGR", Reply: testutil.Static([]byte("<html>error</html>"))},
	}}
}

func run(t *testing.T, c *core.Command, f *testutil.Fake, raw map[string]any) (*testutil.Sink, *core.Result, error) {
	sink := &testutil.Sink{}
	res, err := c.Run(context.Background(), testutil.Env(t, f), testutil.Input(t, c, raw), sink)
	return sink, res, err
}

func TestFicha(t *testing.T) {
	m := &Module{Base: "https://fake"}
	sink, res, err := run(t, m.ficha(), fake(t), map[string]any{"cui": "2222116"})
	if err != nil {
		t.Fatal(err)
	}
	r := sink.Rows[0]
	if r["cui"] != "2222116" || r["estado"] != "ACTIVO" {
		t.Fatalf("row %v", r)
	}
	if d, ok := r["fecha_registro"].(string); !ok || len(d) != 10 {
		t.Fatalf("/Date(ms)/ not converted: %v", r["fecha_registro"])
	}
	data := res.Data.(map[string]any)
	for k, v := range data {
		if v == nil || v == "" || v == 0.0 {
			t.Fatalf("compact kept empty %s=%v", k, v)
		}
	}
}

func TestFichaUnknownIsNotFound(t *testing.T) {
	_, _, err := run(t, (&Module{Base: "https://fake"}).ficha(), fake(t), map[string]any{"cui": "1234567"})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidCUI(t *testing.T) {
	for _, cui := range []string{"abc", "0", "-5", "1234567890"} {
		_, _, err := run(t, (&Module{Base: "https://fake"}).ficha(), fake(t), map[string]any{"cui": cui})
		var e *core.Error
		if !errors.As(err, &e) || e.Code != core.CodeUsage {
			t.Errorf("%q: got %v", cui, err)
		}
	}
}

func TestDetalleAnualIsChronological(t *testing.T) {
	sink, _, err := run(t, (&Module{Base: "https://fake"}).detalle(), fake(t), map[string]any{"cui": "2222116", "view": "anual"})
	if err != nil || len(sink.Rows) < 2 {
		t.Fatalf("rows=%d err=%v", len(sink.Rows), err)
	}
	for i := 1; i < len(sink.Rows); i++ {
		if sink.Rows[i-1]["NUM_ANIO"].(string) > sink.Rows[i]["NUM_ANIO"].(string) {
			t.Fatalf("not chronological at %d", i)
		}
	}
	if sink.Rows[0]["CODIGO_UNICO"] != "2222116" {
		t.Fatalf("id not normalised: %v", sink.Rows[0]["CODIGO_UNICO"])
	}
	if len(sink.Columns) == 0 || sink.Columns[0] != "CODIGO_UNICO" {
		t.Fatalf("columns %v", sink.Columns)
	}
}

func TestHTMLErrorPageIsAPIError(t *testing.T) {
	_, _, err := run(t, (&Module{Base: "https://fake"}).detalle(), fake(t), map[string]any{"cui": "2222116", "view": "obras"})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeAPI {
		t.Fatalf("got %v", err)
	}
}

func TestBuscar(t *testing.T) {
	sink, _, err := run(t, (&Module{Base: "https://fake"}).buscar(), fake(t), map[string]any{"text": "carretera"})
	if err != nil || len(sink.Rows) == 0 {
		t.Fatalf("rows=%d err=%v", len(sink.Rows), err)
	}
	if _, ok := sink.Rows[0]["cui"].(string); !ok {
		t.Fatal("cui must be a string code")
	}
	if _, _, err := run(t, (&Module{Base: "https://fake"}).buscar(), fake(t), map[string]any{"text": "ab"}); err == nil {
		t.Fatal("2-char search must be rejected")
	}
}
