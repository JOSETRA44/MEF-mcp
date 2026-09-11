package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

var cols = []string{"codigo", "nombre", "pim"}

func sample() []core.Row {
	return []core.Row{
		{"codigo": "01", "nombre": "Perú | Ñuñoa", "pim": 1234567.5},
		{"codigo": "02", "nombre": "Côte d'Ivoire", "pim": float64(42)},
	}
}

func run(t *testing.T, format string, rows []core.Row, max int) (string, *Writer) {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(format, &buf, max)
	w.Columns = cols
	if err := w.Emit(rows...); err != nil {
		t.Fatal(err)
	}
	env := Envelope{OK: true, Command: "amigable gasto", Source: "mef", FetchedAt: "2026-09-10T00:00:00Z"}
	if err := w.Finish(env, &core.Result{Columns: cols}); err != nil {
		t.Fatal(err)
	}
	return buf.String(), w
}

// Every format renders the same rows, non-ASCII intact (ARSENAL-SPEC §6.1, §9).
func TestFormatsRenderSameRows(t *testing.T) {
	for _, f := range Formats {
		out, _ := run(t, f, sample(), -1)
		for _, want := range []string{"Ñuñoa", "Côte d'Ivoire"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in:\n%s", f, want, out)
			}
		}
	}
}

func TestEmptyRowsPerFormat(t *testing.T) {
	for _, f := range Formats {
		out, _ := run(t, f, nil, -1)
		switch f {
		case "jsonl":
			if out != "" {
				t.Errorf("jsonl empty result must print nothing, got %q", out)
			}
		case "csv":
			if strings.TrimSpace(out) != "codigo,nombre,pim" {
				t.Errorf("csv empty result must keep the header, got %q", out)
			}
		case "json":
			var env Envelope
			if err := json.Unmarshal([]byte(out), &env); err != nil || env.Count != 0 || env.Rows == nil {
				t.Errorf("json empty envelope wrong: %s", out)
			}
		}
	}
}

// Envelope shape: keys present, count == len(rows) (ARSENAL-SPEC §6.2).
func TestJSONEnvelopeShape(t *testing.T) {
	out, _ := run(t, "json", sample(), -1)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ok", "command", "source", "fetched_at", "cached", "count", "meta", "data", "rows"} {
		if _, ok := m[k]; !ok {
			t.Errorf("envelope missing %q", k)
		}
	}
	if int(m["count"].(float64)) != len(m["rows"].([]any)) {
		t.Error("count != len(rows)")
	}
}

func TestJSONLOneObjectPerLine(t *testing.T) {
	out, _ := run(t, "jsonl", sample(), -1)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	for _, l := range lines {
		var r map[string]any
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("line not JSON: %q", l)
		}
	}
}

func TestCSVQuotingAndRawNumbers(t *testing.T) {
	out, _ := run(t, "csv", sample(), -1)
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if recs[1][1] != "Perú | Ñuñoa" || recs[1][2] != "1234567.5" {
		t.Fatalf("csv values: %v", recs[1])
	}
}

func TestMarkdownEscapesPipes(t *testing.T) {
	out, _ := run(t, "md", sample(), -1)
	if !strings.Contains(out, `Perú \| Ñuñoa`) {
		t.Fatalf("pipe not escaped:\n%s", out)
	}
}

func TestTableGroupsThousands(t *testing.T) {
	out, _ := run(t, "table", sample(), -1)
	if !strings.Contains(out, "1,234,567.50") {
		t.Fatalf("table:\n%s", out)
	}
}

func TestMaxTruncatesAndCounts(t *testing.T) {
	out, w := run(t, "jsonl", sample(), 1)
	if strings.Count(out, "\n") != 1 || w.Truncated() != 1 || w.Wanted() != 0 {
		t.Fatalf("max not honoured: %q truncated=%d", out, w.Truncated())
	}
}

func TestStreamingCSVUsesAnnouncedColumns(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter("csv", &buf, -1)
	w.SetColumns([]string{"b", "a"})
	_ = w.Emit(core.Row{"a": "1", "b": "2"})
	if !strings.HasPrefix(buf.String(), "b,a\n2,1") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestCellFormatting(t *testing.T) {
	cases := map[string]string{
		Cell(nil, true):                "",
		Cell(float64(-1234567), true):  "-1,234,567",
		Cell(0.5, false):               "0.5",
		Cell(int64(1000), true):        "1,000",
		Cell([]string{"a", "b"}, true): "a, b",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
