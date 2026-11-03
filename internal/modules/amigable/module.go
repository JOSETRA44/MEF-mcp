// Package amigable reaches Consulta Amigable, MEF's daily-updated budget
// execution navigator (apps5.mineco.gob.pe/transparencia/Navegador).
//
// The navigator looks like a stateful ASP.NET WebForms app, but its export
// URL is fully stateless: every drill-down is encoded in the query string,
// where each parameter *name* is a dimension code, its *value* the selected
// key, the empty-valued parameter the dimension being grouped, and parameter
// *order* the drill-down path. One GET answers any combination.
package amigable

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
)

// BaseURL is the navigator endpoint.
const BaseURL = "https://apps5.mineco.gob.pe/transparencia/Navegador/Navegar_7.aspx"

// PageSize is the largest page the navigator honours.
const PageSize = 1000

var tipos = map[string]string{"todo": "ActProy", "actividad": "Actividad", "proyecto": "Proyecto"}

// Module implements core.Module.
type Module struct{ Endpoint string }

// New returns the module pointed at production.
func New() *Module { return &Module{Endpoint: BaseURL} }

func (m *Module) ID() string      { return "amigable" }
func (m *Module) Title() string   { return "Consulta Amigable (ejecución del gasto)" }
func (m *Module) BaseURL() string { return m.Endpoint }

// Query is a resolved navigator request.
type Query struct {
	Year     int
	Tipo     string // todo | actividad | proyecto
	Filters  []Filter
	GroupBy  Dimension
	Page     int
	PageSize int
}

// Filter is one fixed step of the drill-down path.
type Filter struct {
	Dim   Dimension `json:"dimension"`
	Value string    `json:"valor"`
}

// Pairs renders the ordered query string. The order is load-bearing.
func (q Query) Pairs() [][2]string {
	pairs := [][2]string{{"_uhc", "yes"}, {"0", ""}}
	for _, f := range q.Filters {
		pairs = append(pairs, [2]string{strconv.Itoa(f.Dim.Code), f.Value})
	}
	pairs = append(pairs,
		[2]string{strconv.Itoa(q.GroupBy.Code), ""},
		[2]string{"y", strconv.Itoa(q.Year)},
		[2]string{"ap", tipos[q.Tipo]},
		[2]string{"cpage", strconv.Itoa(q.Page)},
		[2]string{"psize", strconv.Itoa(q.PageSize)},
	)
	return pairs
}

// BuildQuery resolves user-facing names into a validated Query.
func BuildQuery(year int, tipo, by string, where [][2]string, page, size int) (Query, error) {
	q := Query{Year: year, Tipo: tipo, Page: page, PageSize: size}
	if q.Tipo == "" {
		q.Tipo = "todo"
	}
	g, err := Lookup(by)
	if err != nil {
		return q, err
	}
	q.GroupBy = g
	used := map[int]bool{}
	for _, kv := range where {
		d, err := Lookup(kv[0])
		if err != nil {
			return q, err
		}
		if kv[1] == "" {
			return q, core.Usagef("Give a key, e.g. sector=01. Find keys by grouping first: --by "+d.Alias, "filter %q has no value", kv[0])
		}
		if used[d.Code] {
			return q, core.Usagef("", "dimension %q filtered twice", d.Alias)
		}
		used[d.Code] = true
		q.Filters = append(q.Filters, Filter{Dim: d, Value: normalizeValue(d, kv[1])})
	}
	if used[g.Code] {
		return q, core.Usagef("Group by a different dimension than the ones you filter on.", "cannot group by %q while also filtering on it", g.Alias)
	}
	return q, nil
}

func (m *Module) Commands() []*core.Command {
	return []*core.Command{m.gasto(), m.serie(), m.entidades(), m.dimensiones()}
}

// Command exposes one command by its CLI leaf name so other modules can run
// the same query instead of reimplementing it. mef_reporte uses this: a report
// of spending is the output of mef_gasto, not a second copy of its logic.
func (m *Module) Command(name string) *core.Command {
	switch name {
	case "gasto":
		return m.gasto()
	case "serie":
		return m.serie()
	case "entidades":
		return m.entidades()
	case "dimensiones":
		return m.dimensiones()
	}
	return nil
}

func (m *Module) gasto() *core.Command {
	params := []core.Param{
		{Name: "by", Type: core.TypeString, Default: "nivel", Short: "b",
			Help: `Dimension to group by: alias or code, e.g. "sector", "pliego", "departamento-meta", "generica", "mes". See mef amigable dimensiones.`},
		{Name: "where", Type: core.TypeStringList, Short: "w",
			Help: `Drill-down filters as DIMENSION=KEY, applied in order, e.g. ["nivel=E","sector=01"]. Keys are the "codigo" column of a previous grouping.`},
		{Name: "year", Type: core.TypeInt, Default: 0, Short: "y", Min: core.IntPtr(0), Max: core.IntPtr(2100),
			Help: "Fiscal year 1999–current, e.g. 2026. 0 or omitted = current year."},
		{Name: "type", Type: core.TypeString, Default: "todo", Enum: []string{"todo", "actividad", "proyecto"},
			Help: `Spending type: "todo" (actividades y proyectos), "actividad" (gasto corriente/operativo) or "proyecto" (inversión).`},
		{Name: "sort", Type: core.TypeString, Default: "", Enum: []string{"", "devengado", "pim", "pia", "certificacion", "compromiso_anual", "compromiso_mensual", "girado", "avance", "nombre", "codigo"},
			Help: `Sort the rows server-side, e.g. "devengado" for the biggest spenders or "avance" with desc=false for the laggards. Empty keeps MEF's own order.`},
		{Name: "desc", Type: core.TypeBool, Default: true,
			Help: "Sort descending (default). Set false for ascending, e.g. lowest avance first."},
		{Name: "page", Type: core.TypeInt, Default: 1, Min: core.IntPtr(1), Help: "Page number when not using all, e.g. 2."},
		{Name: "page_size", Type: core.TypeInt, Default: PageSize, Min: core.IntPtr(1), Max: core.IntPtr(PageSize),
			Help: "Rows per upstream page, 1–1000."},
	}
	return &core.Command{
		Path:    []string{"amigable", "gasto"},
		Tool:    "mef_gasto",
		Title:   "Ejecución del gasto público (Consulta Amigable)",
		Summary: "Budget execution (PIA, PIM, certificado, compromiso, devengado, girado, avance %) grouped by any dimension",
		Description: `Returns Peru's public spending execution from MEF's Consulta Amigable, updated daily, for fiscal years 1999 to today: PIA (initial budget), PIM (modified budget), certificación, compromiso anual/mensual, devengado (accrued — the standard "execution" figure), girado (paid) and avance_pct (devengado / PIM × 100), all in soles.

Use it for "how much has X spent", "which regions/sectors execute least", "execution by month", or any drill-down: group with "by" (e.g. sector, pliego, ejecutora, departamento-meta, funcion, generica, fuente, mes) and narrow with ordered "where" filters whose keys come from the "codigo" column of a previous call — e.g. first by=sector, then where=["sector=11"] by=pliego. Nivel accepts E/R/M or nacional/regional/local.

With "sort" the ranking is computed here — "sort=devengado" for the biggest spenders, "sort=avance" with "desc=false" for the laggards — so a top-10 needs no arithmetic on your side; combine it with "max". For several years at once use mef_serie, for conclusions mef_analizar, for a file mef_reporte, and to turn an institution's name into a filter mef_entidades.

Behaviour: one upstream request per page (up to 1000 rows), about 1–2 s; grouping by meta, finalidad or actividad without filters takes 15–30 s upstream. Responses are cached on disk for hours (meta.last_updated shows MEF's own refresh date). An empty result means no spending matches, not an error. Invalid dimensions or a dimension both filtered and grouped fail with a usage error listing valid names.

Not for: investment project sheets by CUI (use mef_inversion_ficha) or open-data files (use mef_datos_buscar). Income/revenue (ingresos) is not covered by this tool.`,
		Params:      append(params, core.PagingParams()...),
		RowFields:   []string{"codigo", "nombre", "pia", "pim", "certificacion", "compromiso_anual", "compromiso_mensual", "devengado", "girado", "avance_pct"},
		Network:     true,
		SupportsAll: true,
		ReadOnly:    true,
		Examples: []string{
			"mef amigable gasto --year 2026 --by nivel",
			"mef amigable gasto --by pliego --where nivel=E --sort devengado --max 10",
			"mef amigable gasto --by sector --where nivel=E -f csv -o sectores.csv",
			"mef amigable gasto --by departamento-meta --type proyecto --year 2025",
			"mef amigable gasto --by mes --where sector=11,pliego=011 -f json",
		},
		Run: m.runGasto,
	}
}

func (m *Module) runGasto(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	year := in.Int("year")
	current := env.Now().Year()
	if year == 0 {
		year = current
	}
	if year < 1999 || year > current {
		return nil, core.Usagef(fmt.Sprintf("Consulta Amigable covers 1999–%d.", current), "year %d is out of range", year)
	}
	where, err := in.Pairs("where")
	if err != nil {
		return nil, err
	}
	page, size := in.Int("page"), in.Int("page_size")
	all := in.Bool("all")
	q, err := BuildQuery(year, in.Str("type"), in.Str("by"), where, page, size)
	if err != nil {
		return nil, err
	}

	if q.GroupBy.Slow && env.Progress != nil {
		env.Progress("grouping by %s is slow upstream (15–30 s)…", q.GroupBy.Alias)
	}
	// Sorting has to see every row before emitting any, so those requests
	// buffer instead of streaming. sortCap keeps that bounded.
	sortKey := in.Str("sort")
	const sortCap = 20000
	var buf []Line

	var (
		lastUpdated, header string
		fetched             int
		pages               int
		totals              Line
		cachedAll           = true
		hasMore             bool
		budgetless          bool
	)
	for {
		if sortKey == "" {
			if w := sink.Wanted(); w == 0 {
				hasMore = true
				break
			}
		} else if len(buf) >= sortCap {
			hasMore = true
			break
		}
		pg, fromCache, err := m.fetchPage(ctx, env, q)
		if err != nil {
			return nil, err
		}
		cachedAll = cachedAll && fromCache
		pages++
		if pg.LastUpdated != "" {
			lastUpdated = pg.LastUpdated
		}
		if pg.Dimension != "" {
			header = pg.Dimension
		}
		for _, l := range pg.Lines {
			budgetless = budgetless || l.NoBudget
			totals.PIA += l.PIA
			totals.PIM += l.PIM
			totals.Certificado += l.Certificado
			totals.CompAnual += l.CompAnual
			totals.CompMensual += l.CompMensual
			totals.Devengado += l.Devengado
			totals.Girado += l.Girado
			if sortKey != "" {
				buf = append(buf, l)
				continue
			}
			if err := sink.Emit(lineRow(l)); err != nil {
				return nil, err
			}
		}
		fetched += len(pg.Lines)
		full := len(pg.Lines) >= q.PageSize
		if !full {
			hasMore = false
			break
		}
		hasMore = true
		if !all {
			break
		}
		q.Page++
		if env.Progress != nil {
			env.Progress("fetched %d rows, requesting page %d…", fetched, q.Page)
		}
	}

	if sortKey != "" {
		SortLines(buf, sortKey, in.Bool("desc"))
		for _, l := range buf {
			if sink.Wanted() == 0 {
				break
			}
			if err := sink.Emit(lineRow(l)); err != nil {
				return nil, err
			}
		}
	}

	filters := make([]map[string]any, 0, len(q.Filters))
	for _, f := range q.Filters {
		filters = append(filters, map[string]any{"dimension": f.Dim.Alias, "codigo": f.Dim.Code, "valor": f.Value})
	}
	meta := map[string]any{
		"year": q.Year, "type": q.Tipo, "group_by": q.GroupBy.Alias, "group_by_code": q.GroupBy.Code,
		"group_header": header, "filters": filters, "pages": pages, "page_size": q.PageSize, "sort": sortKey,
		"has_more": hasMore, "last_updated": lastUpdated, "currency": "PEN",
		"source_url":      m.Endpoint + "?" + httpx.OrderedQuery(q.Pairs()),
		"upstream_cached": cachedAll,
	}
	data := map[string]any{}
	if !hasMore && fetched > 0 && !budgetless {
		if totals.PIM != 0 {
			totals.AvancePct = float64(int(totals.Devengado/totals.PIM*1000+0.5)) / 10
		}
		totals.Code, totals.Name = "TOTAL", "TOTAL"
		data["totales"] = totals
	}
	res := &core.Result{Columns: []string{"codigo", "nombre", "pia", "pim", "certificacion", "compromiso_anual", "compromiso_mensual", "devengado", "girado", "avance_pct"}, Data: data, Meta: meta}
	if hasMore && !all {
		res.Notes = append(res.Notes, "more rows available: add --all (or raise --page)")
	}
	return res, nil
}

// SortLines orders rows by one of mef_gasto's columns. Names use Spanish
// collation loosely (accent-folded) so "Ñ" lands where a reader expects.
func SortLines(lines []Line, key string, desc bool) {
	value := func(l Line) float64 {
		switch key {
		case "devengado":
			return l.Devengado
		case "pim":
			return l.PIM
		case "pia":
			return l.PIA
		case "certificacion":
			return l.Certificado
		case "compromiso_anual":
			return l.CompAnual
		case "compromiso_mensual":
			return l.CompMensual
		case "girado":
			return l.Girado
		case "avance", "avance_pct":
			return l.AvancePct
		}
		return 0
	}
	less := func(i, j int) bool { return value(lines[i]) < value(lines[j]) }
	switch key {
	case "nombre":
		less = func(i, j int) bool { return normalize(lines[i].Name) < normalize(lines[j].Name) }
	case "codigo":
		less = func(i, j int) bool { return lines[i].Code < lines[j].Code }
	}
	if desc {
		inner := less
		less = func(i, j int) bool { return inner(j, i) }
	}
	sort.SliceStable(lines, less)
}

func lineRow(l Line) core.Row {
	r := core.Row{
		"codigo": l.Code, "nombre": l.Name, "pia": l.PIA, "pim": l.PIM, "certificacion": l.Certificado,
		"compromiso_anual": l.CompAnual, "compromiso_mensual": l.CompMensual,
		"devengado": l.Devengado, "girado": l.Girado, "avance_pct": l.AvancePct,
	}
	if l.ShownCode != "" {
		r["codigo_mostrado"] = l.ShownCode
	}
	if l.NoBudget {
		r["pia"], r["pim"], r["avance_pct"] = nil, nil, nil
	}
	return r
}

func (m *Module) dimensiones() *core.Command {
	return &core.Command{
		Path:    []string{"amigable", "dimensiones"},
		Tool:    "mef_gasto_dimensiones",
		Title:   "Dimensiones de Consulta Amigable",
		Summary: "List the dimensions mef_gasto can group and filter by (offline)",
		Description: `Lists every dimension accepted by mef_gasto's "by" and "where" parameters: alias, numeric navigator code, official label, the navigator question it belongs to (¿Quién gasta?, ¿En qué se gasta?, ¿Con qué se financian?, ¿Cómo se estructura?, ¿Dónde?, ¿Cuándo?), whether it is slow upstream, and usage notes.

Use it before mef_gasto when unsure which alias answers a question (e.g. departamento vs departamento-meta). Answered locally with no network call, instantly; the list is static and was verified against the live navigator. Optional "question" filters by keyword.`,
		Params: []core.Param{
			{Name: "question", Type: core.TypeString, Help: `Optional keyword to filter by label, alias or question, e.g. "quién" or "funcional".`},
		},
		RowFields: []string{"alias", "codigo", "nombre", "pregunta", "lento", "nota"},
		ReadOnly:  true,
		Examples:  []string{"mef amigable dimensiones", "mef amigable dimensiones --question funcional -f json"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			kw := normalize(in.Str("question"))
			for _, d := range Dimensions {
				hay := normalize(d.Alias + " " + d.Label + " " + d.Group + " " + strings.Join(d.Aliases, " "))
				if kw != "" && !strings.Contains(hay, kw) {
					continue
				}
				if err := sink.Emit(core.Row{"alias": d.Alias, "codigo": d.Code, "nombre": d.Label, "pregunta": d.Group, "lento": d.Slow, "nota": d.Note}); err != nil {
					return nil, err
				}
			}
			return &core.Result{Columns: []string{"alias", "codigo", "nombre", "pregunta", "lento", "nota"}}, nil
		},
	}
}
