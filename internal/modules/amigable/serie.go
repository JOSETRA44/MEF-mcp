package amigable

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// maxSpan bounds a series: one request per year, and MEF is not fast.
const maxSpan = 20

func (m *Module) serie() *core.Command {
	return &core.Command{
		Path:    []string{"amigable", "serie"},
		Tool:    "mef_serie",
		Title:   "Serie anual de ejecución del gasto",
		Summary: "Budget execution for the same grouping across several years, with year-on-year variation",
		Description: `Returns one row per group and year — PIM, devengado, avance % and the year-on-year variation of devengado — for a range of fiscal years, plus per-year totals and a written trend in data.

Use it for any question about evolution, history or comparison over time ("¿cómo ha evolucionado la inversión de Cusco desde 2019?", "compara 2024 y 2025"). It replaces calling mef_gasto once per year: the years are fetched concurrently and the arithmetic (variación, totales por año) is already done, so the figures you report are not hand-added.

Parameters mirror mef_gasto: "by" is the dimension to group (sector, pliego, departamento-meta…), "where" the ordered drill-down filters, "type" todo/actividad/proyecto. "from" and "to" are fiscal years (1999–current, at most 20 years).

Behaviour: one upstream request per year (1–2 s each, concurrent, cached on disk); a grouping with more than 1000 rows per year is truncated and meta.truncated says so — narrow it with "where". The current year is partial by definition: meta.last_updated carries MEF's cut-off date. Rows come sorted by group and year, so a table reads as a series.`,
		Params: []core.Param{
			{Name: "by", Type: core.TypeString, Default: "nivel", Short: "b",
				Help: `Dimension to group by, e.g. "departamento-meta", "sector", "funcion". Same aliases as mef_gasto.`},
			{Name: "where", Type: core.TypeStringList, Short: "w",
				Help: `Drill-down filters DIMENSION=KEY, in order, e.g. ["nivel=R","departamento-meta=08"].`},
			{Name: "from", Type: core.TypeInt, Default: 0, Min: core.IntPtr(0), Max: core.IntPtr(2100),
				Help: "First fiscal year, e.g. 2019. 0 = five years before the current one."},
			{Name: "to", Type: core.TypeInt, Default: 0, Min: core.IntPtr(0), Max: core.IntPtr(2100),
				Help: "Last fiscal year, e.g. 2026. 0 = current year."},
			{Name: "type", Type: core.TypeString, Default: "todo", Enum: []string{"todo", "actividad", "proyecto"},
				Help: `Spending type: "todo", "actividad" (gasto corriente) or "proyecto" (inversión).`},
			{Name: "max", Type: core.TypeInt, Default: core.DefaultMax, Min: core.IntPtr(1), Max: core.IntPtr(100000),
				Help: "Maximum rows returned (groups × years), e.g. 200."},
		},
		RowFields:   []string{"anio", "codigo", "nombre", "pim", "devengado", "avance_pct", "variacion_pct"},
		Network:     true,
		SupportsAll: false,
		ReadOnly:    true,
		Examples: []string{
			"mef amigable serie --by nivel --from 2019 --to 2026",
			"mef amigable serie --by departamento-meta --type proyecto --from 2022 -f csv -o inversion_regional.csv",
			"mef amigable serie --by sector --where nivel=E --from 2024 --to 2026 -f json",
		},
		Run: m.runSerie,
	}
}

type yearLines struct {
	year  int
	lines []Line
	trunc bool
}

func (m *Module) runSerie(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	current := env.Now().Year()
	from, to := in.Int("from"), in.Int("to")
	if to == 0 {
		to = current
	}
	if from == 0 {
		from = max(1999, to-4)
	}
	if from > to {
		from, to = to, from
	}
	if from < 1999 || to > current {
		return nil, core.Usagef(fmt.Sprintf("Consulta Amigable covers 1999–%d.", current), "year range %d–%d is out of range", from, to)
	}
	if to-from+1 > maxSpan {
		return nil, core.Usagef(fmt.Sprintf("Ask for at most %d years at a time.", maxSpan), "range of %d years is too wide", to-from+1)
	}
	where, err := in.Pairs("where")
	if err != nil {
		return nil, err
	}
	base, err := BuildQuery(to, in.Str("type"), in.Str("by"), where, 1, PageSize)
	if err != nil {
		return nil, err
	}

	years := make([]int, 0, to-from+1)
	for y := from; y <= to; y++ {
		years = append(years, y)
	}
	if env.Progress != nil {
		env.Progress("consultando %d años por %s…", len(years), base.GroupBy.Alias)
	}

	results := make([]yearLines, len(years))
	errs := make([]error, len(years))
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		cachedAll   = true
		lastUpdated string
		header      string
	)
	// Three at a time: httpx already spaces requests per host, and MEF's
	// navigator degrades under load.
	sem := make(chan struct{}, 3)
	for i, y := range years {
		wg.Add(1)
		go func(i, y int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			q := base
			q.Year = y
			lines, meta, cached, trunc, err := m.fetchAll(ctx, env, q, 3)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = yearLines{year: y, lines: lines, trunc: trunc}
			mu.Lock()
			cachedAll = cachedAll && cached
			if meta.LastUpdated != "" {
				lastUpdated = meta.LastUpdated
			}
			if meta.Dimension != "" {
				header = meta.Dimension
			}
			mu.Unlock()
		}(i, y)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	type cell struct {
		line Line
		year int
	}
	byCode := map[string][]cell{}
	names := map[string]string{}
	order := []string{}
	truncated := false
	perYear := make([]map[string]any, 0, len(years))
	trendTotals := make([]float64, 0, len(years))
	trendLabels := make([]string, 0, len(years))

	for _, r := range results {
		truncated = truncated || r.trunc
		var pim, dev float64
		for _, l := range r.lines {
			if _, seen := byCode[l.Code]; !seen {
				order = append(order, l.Code)
				names[l.Code] = l.Name
			}
			byCode[l.Code] = append(byCode[l.Code], cell{line: l, year: r.year})
			pim += l.PIM
			dev += l.Devengado
		}
		perYear = append(perYear, map[string]any{
			"anio": r.year, "pim": pim, "devengado": dev, "avance_pct": analysis.Pct(dev, pim), "filas": len(r.lines),
		})
		trendLabels = append(trendLabels, strconv.Itoa(r.year))
		trendTotals = append(trendTotals, dev)
	}

	// Stable, readable order: group by name, years ascending inside it.
	sort.SliceStable(order, func(i, j int) bool {
		if names[order[i]] != names[order[j]] {
			return names[order[i]] < names[order[j]]
		}
		return order[i] < order[j]
	})

	emitted := 0
	for _, k := range order {
		cells := byCode[k]
		sort.SliceStable(cells, func(i, j int) bool { return cells[i].year < cells[j].year })
		var prev float64
		var havePrev bool
		for _, c := range cells {
			if sink.Wanted() == 0 {
				truncated = true
				break
			}
			row := core.Row{
				"anio": c.year, "codigo": c.line.Code, "nombre": c.line.Name,
				"pim": c.line.PIM, "devengado": c.line.Devengado, "avance_pct": c.line.AvancePct,
			}
			if c.line.NoBudget {
				row["pim"], row["avance_pct"] = nil, nil
			}
			if havePrev && prev != 0 {
				row["variacion_pct"] = analysis.Pct(c.line.Devengado-prev, prev)
			}
			prev, havePrev = c.line.Devengado, true
			if err := sink.Emit(row); err != nil {
				return nil, err
			}
			emitted++
		}
	}

	filters := make([]map[string]any, 0, len(base.Filters))
	for _, f := range base.Filters {
		filters = append(filters, map[string]any{"dimension": f.Dim.Alias, "codigo": f.Dim.Code, "valor": f.Value})
	}
	trend := analysis.Trend(trendLabels, trendTotals)
	res := &core.Result{
		Columns: []string{"anio", "codigo", "nombre", "pim", "devengado", "avance_pct", "variacion_pct"},
		Data: map[string]any{
			"por_anio":  perYear,
			"tendencia": trend,
		},
		Meta: map[string]any{
			"from": from, "to": to, "years": len(years), "group_by": base.GroupBy.Alias,
			"group_header": header, "filters": filters, "type": base.Tipo,
			"last_updated": lastUpdated, "currency": "PEN", "groups": len(order),
			"truncated": truncated, "upstream_cached": cachedAll,
		},
	}
	if truncated {
		res.Notes = append(res.Notes, "series truncated: narrow it with --where or raise --max")
	}
	if to == current {
		res.Notes = append(res.Notes, fmt.Sprintf("%d is still in progress: its execution is partial as of %s", current, lastUpdated))
	}
	return res, nil
}
