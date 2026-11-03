// Package analysis turns rows of budget data into the conclusions a reader
// actually wants: totals, rankings, concentration, laggards and idle budget.
//
// It exists because arithmetic over hundreds of rows is exactly what language
// models get wrong. Computing it here means an agent reports figures it never
// had to add up, and a report always agrees with its own table.
package analysis

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/render"
)

// Options selects the columns to analyse. Zero values fall back to the shape
// mef_gasto returns.
type Options struct {
	Metric string // executed amount, default "devengado"
	Budget string // reference budget, default "pim"
	Label  string // display name, default "nombre"
	Code   string // identifier, default "codigo"
	TopN   int    // ranking length, default 10
}

func (o Options) withDefaults() Options {
	if o.Metric == "" {
		o.Metric = "devengado"
	}
	if o.Budget == "" {
		o.Budget = "pim"
	}
	if o.Label == "" {
		o.Label = "nombre"
	}
	if o.Code == "" {
		o.Code = "codigo"
	}
	if o.TopN <= 0 {
		o.TopN = 10
	}
	return o
}

// Entry is one ranked row.
type Entry struct {
	Code   string  `json:"codigo"`
	Name   string  `json:"nombre"`
	Value  float64 `json:"valor"`
	Budget float64 `json:"presupuesto"`
	Avance float64 `json:"avance_pct"`
	Share  float64 `json:"participacion_pct"`
}

// Finding is one conclusion, phrased for a human and machine-readable.
type Finding struct {
	Kind   string  `json:"tipo"`
	Title  string  `json:"titulo"`
	Detail string  `json:"detalle"`
	Value  float64 `json:"valor"`
}

// Result is the full analysis.
type Result struct {
	Rows        int       `json:"filas"`
	Metric      string    `json:"metrica"`
	Budget      string    `json:"presupuesto"`
	Total       float64   `json:"total_ejecutado"`
	TotalBudget float64   `json:"total_presupuesto"`
	AvancePct   float64   `json:"avance_pct"`
	Mean        float64   `json:"promedio"`
	Median      float64   `json:"mediana"`
	Top         []Entry   `json:"top"`
	Bottom      []Entry   `json:"rezagados"`
	Idle        []Entry   `json:"sin_ejecucion"`
	IdleBudget  float64   `json:"presupuesto_sin_ejecutar"`
	Top5Share   float64   `json:"concentracion_top5_pct"`
	Findings    []Finding `json:"hallazgos"`
	Summary     string    `json:"resumen"`
}

// Num reads a numeric cell, tolerating the nulls Consulta Amigable returns
// for time dimensions and the strings the datastore returns for every column.
func Num(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		s := strings.ReplaceAll(strings.TrimSpace(t), ",", "")
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Str renders a cell as text.
func Str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return render.Cell(v, false)
}

// Pct is a percentage rounded to one decimal.
func Pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return math.Round(part/whole*1000) / 10
}

// Soles formats an amount the way MEF publishes it.
func Soles(v float64) string { return "S/ " + render.Cell(v, true) }

// Analyze computes every statistic in one pass over the rows.
func Analyze(rows []core.Row, opts Options) *Result {
	o := opts.withDefaults()
	r := &Result{Rows: len(rows), Metric: o.Metric, Budget: o.Budget}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		value, okV := Num(row[o.Metric])
		budget, _ := Num(row[o.Budget])
		if !okV && budget == 0 {
			continue
		}
		e := Entry{Code: Str(row[o.Code]), Name: Str(row[o.Label]), Value: value, Budget: budget, Avance: Pct(value, budget)}
		if a, ok := Num(row["avance_pct"]); ok && o.Metric == "devengado" {
			e.Avance = a
		}
		entries = append(entries, e)
		r.Total += value
		r.TotalBudget += budget
	}
	if len(entries) == 0 {
		r.Summary = "Sin filas con montos para analizar."
		return r
	}
	r.AvancePct = Pct(r.Total, r.TotalBudget)
	r.Mean = math.Round(r.Total/float64(len(entries))*100) / 100

	values := make([]float64, len(entries))
	for i, e := range entries {
		values[i] = e.Value
	}
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		r.Median = values[mid]
	} else {
		r.Median = (values[mid-1] + values[mid]) / 2
	}

	byValue := append([]Entry(nil), entries...)
	sort.SliceStable(byValue, func(i, j int) bool { return byValue[i].Value > byValue[j].Value })
	for i := range byValue {
		byValue[i].Share = Pct(byValue[i].Value, r.Total)
	}
	r.Top = byValue[:min(o.TopN, len(byValue))]
	// Computed from the raw amounts, not by adding the rounded shares: five
	// values rounded to one decimal can add up to 100.1 %.
	var top5 float64
	for i := 0; i < min(5, len(byValue)); i++ {
		top5 += byValue[i].Value
	}
	r.Top5Share = Pct(top5, r.Total)

	// Laggards and idle budget only mean something where there is a budget to
	// execute: a row with PIM 0 is not behind, it simply has no money.
	var funded []Entry
	for _, e := range entries {
		if e.Budget <= 0 {
			continue
		}
		if e.Value <= 0 {
			r.Idle = append(r.Idle, e)
			r.IdleBudget += e.Budget
			continue
		}
		funded = append(funded, e)
	}
	sort.SliceStable(funded, func(i, j int) bool { return funded[i].Avance < funded[j].Avance })
	r.Bottom = funded[:min(o.TopN, len(funded))]
	sort.SliceStable(r.Idle, func(i, j int) bool { return r.Idle[i].Budget > r.Idle[j].Budget })
	if len(r.Idle) > o.TopN {
		r.Idle = r.Idle[:o.TopN]
	}

	r.Findings = findings(r, len(entries))
	r.Summary = summarize(r, len(entries))
	return r
}

func findings(r *Result, n int) []Finding {
	out := []Finding{{
		Kind:  "total",
		Title: "Ejecución total",
		Detail: fmt.Sprintf("%s ejecutados de %s de PIM en %d filas: %.1f %% de avance.",
			Soles(r.Total), Soles(r.TotalBudget), n, r.AvancePct),
		Value: r.AvancePct,
	}}
	if len(r.Top) > 0 {
		t := r.Top[0]
		out = append(out, Finding{
			Kind:   "mayor_monto",
			Title:  "Mayor ejecución",
			Detail: fmt.Sprintf("%s (%s) con %s, el %.1f %% del total.", t.Name, t.Code, Soles(t.Value), t.Share),
			Value:  t.Value,
		})
	}
	if r.Top5Share > 0 && n > 5 {
		out = append(out, Finding{
			Kind:   "concentracion",
			Title:  "Concentración",
			Detail: fmt.Sprintf("Los 5 primeros concentran el %.1f %% de la ejecución.", r.Top5Share),
			Value:  r.Top5Share,
		})
	}
	if len(r.Bottom) > 0 {
		b := r.Bottom[0]
		out = append(out, Finding{
			Kind:   "menor_avance",
			Title:  "Menor avance con presupuesto",
			Detail: fmt.Sprintf("%s (%s) ejecuta %.1f %% de un PIM de %s.", b.Name, b.Code, b.Avance, Soles(b.Budget)),
			Value:  b.Avance,
		})
	}
	if len(r.Idle) > 0 {
		out = append(out, Finding{
			Kind:   "sin_ejecucion",
			Title:  "Presupuesto sin ejecutar",
			Detail: fmt.Sprintf("%d filas tienen PIM pero cero ejecución, %s en total (la mayor: %s).", len(r.Idle), Soles(r.IdleBudget), r.Idle[0].Name),
			Value:  r.IdleBudget,
		})
	}
	if r.Median > 0 && r.Mean > 0 {
		skew := "simétrica"
		switch {
		case r.Mean > r.Median*2:
			skew = "muy concentrada en pocas filas"
		case r.Mean > r.Median*1.2:
			skew = "sesgada hacia las filas grandes"
		}
		out = append(out, Finding{
			Kind:   "distribucion",
			Title:  "Distribución",
			Detail: fmt.Sprintf("Promedio %s frente a mediana %s: distribución %s.", Soles(r.Mean), Soles(r.Median), skew),
			Value:  r.Mean,
		})
	}
	return out
}

func summarize(r *Result, n int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d filas analizadas. Ejecución de %s sobre un PIM de %s (%.1f %% de avance).",
		n, Soles(r.Total), Soles(r.TotalBudget), r.AvancePct)
	if len(r.Top) > 0 {
		fmt.Fprintf(&sb, " Encabeza %s con %s (%.1f %%).", r.Top[0].Name, Soles(r.Top[0].Value), r.Top[0].Share)
	}
	if r.Top5Share > 0 && n > 5 {
		fmt.Fprintf(&sb, " Los 5 primeros concentran el %.1f %%.", r.Top5Share)
	}
	if len(r.Bottom) > 0 {
		fmt.Fprintf(&sb, " El menor avance con presupuesto es %s (%.1f %%).", r.Bottom[0].Name, r.Bottom[0].Avance)
	}
	if len(r.Idle) > 0 {
		fmt.Fprintf(&sb, " %d filas no ejecutan nada de %s.", len(r.Idle), Soles(r.IdleBudget))
	}
	return sb.String()
}

// FindingRows projects the findings for a command's row output.
func FindingRows(r *Result) []core.Row {
	out := make([]core.Row, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, core.Row{"tipo": f.Kind, "titulo": f.Title, "detalle": f.Detail, "valor": f.Value})
	}
	return out
}

// Trend compares a metric across consecutive periods (used by mef_serie).
// Periods must already be sorted; the first one has nothing to compare to.
func Trend(periods []string, totals []float64) []Finding {
	var out []Finding
	for i := 1; i < len(periods) && i < len(totals); i++ {
		prev, cur := totals[i-1], totals[i]
		if prev == 0 {
			continue
		}
		delta := Pct(cur-prev, prev)
		word, shown := "crece", delta
		if delta < 0 {
			word, shown = "cae", -delta
		}
		out = append(out, Finding{
			Kind:   "variacion",
			Title:  periods[i-1] + " → " + periods[i],
			Detail: fmt.Sprintf("La ejecución %s %.1f %%: de %s a %s.", word, shown, Soles(prev), Soles(cur)),
			Value:  delta,
		})
	}
	return out
}
