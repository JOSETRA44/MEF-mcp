package asistente

import (
	"context"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

func ask(t *testing.T, question string) (*testutil.Sink, *core.Result) {
	t.Helper()
	cmd := New().ayuda()
	sink := &testutil.Sink{}
	args := map[string]any{}
	if question != "" {
		args["question"] = question
	}
	res, err := cmd.Run(context.Background(), testutil.Env(t, &testutil.Fake{}), testutil.Input(t, cmd, args), sink)
	if err != nil {
		t.Fatalf("%q: %v", question, err)
	}
	return sink, res
}

func TestRoutingPicksTheRightPlaybook(t *testing.T) {
	cases := []struct {
		question  string
		firstTool string
		caseWord  string
	}{
		{"¿en qué va la obra del hospital de Ayacucho?", "mef_inversion_buscar", "inversión"},
		{"quiero un excel del gasto", "mef_entidades", "Excel"},
		{"cómo ha evolucionado la inversión desde 2019", "mef_serie", "Evolución"},
		{"qué región ejecuta menos inversión", "mef_analizar", "Análisis"},
		{"cuánto ejecutó el Ministerio de Salud", "mef_entidades", "entidad"},
		{"ejecución por mes de educación", "mef_gasto", "mes"},
		{"dame el dataset de deuda pública", "mef_datos_buscar", "Datos abiertos"},
		{"cuál es el avance del devengado", "mef_gasto", "gasto"},
	}
	for _, c := range cases {
		sink, _ := ask(t, c.question)
		if len(sink.Rows) == 0 {
			t.Errorf("%q: no suggestion", c.question)
			continue
		}
		first := sink.Rows[0]
		if first["tool"] != c.firstTool {
			t.Errorf("%q: first tool %v, want %s", c.question, first["tool"], c.firstTool)
		}
		if !strings.Contains(strings.ToLower(first["caso"].(string)), strings.ToLower(c.caseWord)) {
			t.Errorf("%q: case %q should mention %q", c.question, first["caso"], c.caseWord)
		}
		if first["paso"] != 1 || first["por_que"] == "" || first["argumentos"] == "" {
			t.Errorf("%q: incomplete step %v", c.question, first)
		}
	}
}

func TestEmptyQuestionReturnsEveryPlaybook(t *testing.T) {
	sink, res := ask(t, "")
	if res.Meta["casos"] != len(playbooks) {
		t.Fatalf("casos = %v, want %d", res.Meta["casos"], len(playbooks))
	}
	steps := 0
	for _, p := range playbooks {
		steps += len(p.Steps)
	}
	if len(sink.Rows) != steps {
		t.Fatalf("rows = %d, want %d", len(sink.Rows), steps)
	}
	if rem, ok := res.Data.(map[string]any)["recordatorios"].([]string); !ok || len(rem) < 3 {
		t.Fatalf("reminders: %v", res.Data)
	}
}

func TestUnmatchedQuestionSaysSoInsteadOfGuessing(t *testing.T) {
	sink, res := ask(t, "cuál es la capital de Francia")
	if len(sink.Rows) != 0 {
		t.Fatalf("should not invent a playbook: %v", sink.Rows)
	}
	if len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "mef ayuda") {
		t.Fatalf("notes should point at the full list: %v", res.Notes)
	}
}

// Every suggested tool must exist, or the guide sends agents nowhere.
func TestEverySuggestedToolIsReal(t *testing.T) {
	known := map[string]bool{}
	for _, name := range []string{
		"mef_gasto", "mef_gasto_dimensiones", "mef_serie", "mef_entidades", "mef_analizar", "mef_reporte",
		"mef_datos_buscar", "mef_datos_dataset", "mef_datos_consultar", "mef_sync", "mef_sql", "mef_tablas",
		"mef_inversion_buscar", "mef_inversion_ficha", "mef_inversion_detalle", "mef_ayuda",
	} {
		known[name] = true
	}
	for _, p := range playbooks {
		if len(p.Steps) == 0 {
			t.Errorf("playbook %q has no steps", p.Name)
		}
		for _, s := range p.Steps {
			if !known[s.Tool] {
				t.Errorf("playbook %q suggests unknown tool %q", p.Name, s.Tool)
			}
			if !strings.HasPrefix(s.Args, "{") {
				t.Errorf("%s: arguments must be a JSON object, got %q", s.Tool, s.Args)
			}
		}
	}
}
