package informes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/amigable"
	"github.com/JOSETRA44/mef-mcp/internal/modules/datosabiertos"
	"github.com/JOSETRA44/mef-mcp/internal/modules/local"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

func line(i int, code, name string, pim, dev float64) string {
	return fmt.Sprintf(
		`<tr id="tr%d"><td><input type="radio" name="grp1" value="%s/0/%.2f/0/0/%.2f/0/0"></td>`+
			`<td align="left">%s: %s</td></tr>`, i, code, pim, dev, code, name)
}

func setup(t *testing.T) (*Module, *core.Env) {
	t.Helper()
	t.Setenv("MEF_HOME", t.TempDir())
	body := []byte(line(0, "01", "SALUD", 1000, 900) + line(1, "02", "EDUCACIÓN", 2000, 1000) + line(2, "03", "CULTURA", 500, 0))
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: testutil.Static(body)}}}
	am := &amigable.Module{Endpoint: "https://fake/Navegar_7.aspx"}
	lo := local.New(&datosabiertos.Module{EP: datosabiertos.Endpoints{Datastore: "https://fake/datastore"}})
	return New(am, lo), testutil.Env(t, fake)
}

func run(t *testing.T, env *core.Env, cmd *core.Command, args map[string]any) (*testutil.Sink, *core.Result, error) {
	t.Helper()
	sink := &testutil.Sink{}
	in, err := core.NewInput(cmd.Params, args, nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := cmd.Run(context.Background(), env, in, sink)
	return sink, res, err
}

func codeOf(err error) core.ErrorCode {
	var e *core.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestAnalizarReturnsFindingsNotRows(t *testing.T) {
	m, env := setup(t)
	sink, res, err := run(t, env, m.analizar(), map[string]any{"by": "sector", "year": 2026})
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) < 4 {
		t.Fatalf("want one row per finding, got %d", len(sink.Rows))
	}
	if sink.Rows[0]["tipo"] != "total" || sink.Rows[0]["detalle"] == "" {
		t.Fatalf("first finding: %v", sink.Rows[0])
	}
	data := res.Data.(map[string]any)
	totals := data["totales"].(map[string]any)
	if totals["ejecutado"] != 1900.0 || totals["presupuesto"] != 3500.0 {
		t.Fatalf("totals: %v", totals)
	}
	if !strings.Contains(data["resumen"].(string), "S/") {
		t.Fatalf("summary: %v", data["resumen"])
	}
	// CULTURA has PIM and zero execution, so it must surface as idle budget.
	if !strings.Contains(fmt.Sprint(data["sin_ejecucion"]), "CULTURA") {
		t.Fatalf("idle budget: %v", data["sin_ejecucion"])
	}
	if res.Meta["metric"] != "devengado" || res.Meta["group_by"] != "sector" {
		t.Fatalf("meta: %v", res.Meta)
	}
}

func TestReporteWritesEveryFormat(t *testing.T) {
	m, env := setup(t)
	for _, format := range []string{"xlsx", "html", "md"} {
		sink, res, err := run(t, env, m.reporte(), map[string]any{
			"by": "sector", "year": 2026, "format": format, "nombre": "informe-" + format,
		})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		row := sink.Rows[0]
		path, _ := row["archivo"].(string)
		st, statErr := os.Stat(path)
		if statErr != nil || st.Size() == 0 {
			t.Fatalf("%s: file missing (%v)", format, statErr)
		}
		if filepath.Ext(path) != "."+format || row["filas"] != 3 {
			t.Fatalf("%s: row %v", format, row)
		}
		if format == "xlsx" && row["hojas"] != "Resumen, Datos, Fuente" {
			t.Fatalf("sheets: %v", row["hojas"])
		}
		data := res.Data.(map[string]any)
		if _, ok := data["resumen"]; !ok {
			t.Fatalf("%s: report must carry its summary", format)
		}
		if res.Meta["directorio"] == "" {
			t.Fatal("meta must say where the file went")
		}
	}
}

func TestReporteNamesAndTitlesDefaultSensibly(t *testing.T) {
	m, env := setup(t)
	sink, _, err := run(t, env, m.reporte(), map[string]any{"by": "sector", "year": 2026, "format": "md"})
	if err != nil {
		t.Fatal(err)
	}
	path := sink.Rows[0]["archivo"].(string)
	if base := filepath.Base(path); base != "gasto-sector-2026.md" {
		t.Fatalf("default name: %s", base)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "Ejecución del gasto por") {
		t.Fatalf("default title missing:\n%s", body)
	}
}

func TestReporteRejectsBadRequests(t *testing.T) {
	m, env := setup(t)
	// source=sql without a query is a usage error, not an empty report.
	if _, _, err := run(t, env, m.reporte(), map[string]any{"source": "sql"}); codeOf(err) != core.CodeUsage {
		t.Errorf("sql without query: %v", err)
	}
	// Nothing to report is not_found, so an agent does not announce an empty file.
	empty := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: testutil.Static([]byte("<html></html>"))}}}
	t.Setenv("MEF_HOME", t.TempDir())
	if _, _, err := run(t, testutil.Env(t, empty), m.reporte(), map[string]any{"by": "sector", "year": 2026}); codeOf(err) != core.CodeNotFound {
		t.Errorf("empty query: %v", err)
	}
}

func TestCaptureSinkHonoursMax(t *testing.T) {
	c := &capture{max: 2}
	_ = c.Emit(core.Row{"a": 1}, core.Row{"a": 2}, core.Row{"a": 3})
	if len(c.rows) != 2 || c.Wanted() != 0 {
		t.Fatalf("rows=%d wanted=%d", len(c.rows), c.Wanted())
	}
	unbounded := &capture{}
	if unbounded.Wanted() != -1 {
		t.Fatal("an unbounded capture must report -1")
	}
}
