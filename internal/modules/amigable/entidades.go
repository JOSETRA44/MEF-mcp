package amigable

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// autoDimensions are searched when "kind" is left at auto: the four that
// answer "who spends" questions. Ordered cheapest first.
var autoDimensions = []string{"sector", "pliego", "ejecutora", "municipalidad"}

func (m *Module) entidades() *core.Command {
	return &core.Command{
		Path:    []string{"amigable", "entidades"},
		Tool:    "mef_entidades",
		Title:   "Buscar entidades y códigos por nombre",
		Summary: "Find the code of a sector, pliego, unidad ejecutora or municipalidad by name, ready to use as a filter",
		Description: `Searches the names Consulta Amigable uses and returns, for each match, its dimension, its code, its PIM and devengado for the year, and a ready-to-paste filter string such as "pliego=011".

This is the tool that turns a name into a query. Reach for it first whenever a question names an institution rather than a code — "Ministerio de Salud", "Municipalidad de Arequipa", "Gobierno Regional de Puno", "UNMSM" — and then pass the "filtro" value straight into mef_gasto, mef_serie, mef_analizar or mef_reporte as "where". Without it an agent has to guess codes or drill down blindly.

Matching is accent- and case-insensitive, every word must appear in any order, connectors (de, del, la…) are ignored, and MEF's abbreviations are handled — the navigator writes "M. DE SALUD" in the same list as "MINISTERIO DE TRANSPORTES Y COMUNICACIONES", so "ministerio de salud" finds both styles. A pure number is matched against codes too. "kind" picks where to look: auto searches sector, pliego, ejecutora and municipalidad; any single dimension alias (e.g. "departamento-meta", "funcion", "categoria") restricts it. "where" scopes the search, e.g. ["nivel=M"] for local governments only.

Behaviour: the dimension listings come from Consulta Amigable and are cached on disk, so the first auto search costs about 4–8 upstream requests (5–15 s) and later ones are instant. Rows are sorted by devengado, so the biggest match comes first. An empty result means no name contains those words — try fewer or use mef_gasto with by=<dimension> to list everything.`,
		Params: []core.Param{
			{Name: "text", Type: core.TypeString, Positional: true, Required: true,
				Help: `Words of the name, or a code, e.g. "ministerio de salud" or "011".`},
			{Name: "kind", Type: core.TypeString, Default: "auto",
				Help: `Where to search: "auto" (sector, pliego, ejecutora, municipalidad) or one dimension alias, e.g. "departamento-meta".`},
			{Name: "where", Type: core.TypeStringList, Short: "w",
				Help: `Scope filters DIMENSION=KEY, e.g. ["nivel=M"] to search only local governments.`},
			{Name: "year", Type: core.TypeInt, Default: 0, Short: "y", Min: core.IntPtr(0), Max: core.IntPtr(2100),
				Help: "Fiscal year whose listing to search, e.g. 2026. 0 = current year."},
			{Name: "max", Type: core.TypeInt, Default: 50, Min: core.IntPtr(1), Max: core.IntPtr(5000),
				Help: "Maximum matches returned, e.g. 10."},
		},
		RowFields: []string{"dimension", "codigo", "nombre", "filtro", "pim", "devengado", "avance_pct"},
		Network:   true,
		ReadOnly:  true,
		Examples: []string{
			`mef amigable entidades "ministerio de salud"`,
			`mef amigable entidades "municipalidad de arequipa" --where nivel=M --max 5`,
			`mef amigable entidades puno --kind departamento-meta -f json`,
		},
		Run: m.runEntidades,
	}
}

type match struct {
	dim  Dimension
	line Line
}

func (m *Module) runEntidades(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	text := strings.TrimSpace(in.Str("text"))
	if len([]rune(text)) < 2 {
		return nil, core.Usagef("Use at least two characters.", "search text %q is too short", text)
	}
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

	var dims []string
	if kind := strings.TrimSpace(in.Str("kind")); kind == "" || normalize(kind) == "auto" {
		dims = autoDimensions
	} else {
		d, err := Lookup(kind)
		if err != nil {
			return nil, err
		}
		dims = []string{d.Alias}
	}

	words := searchWords(text)
	isCode := allDigits(text)

	if env.Progress != nil {
		env.Progress("buscando %q en %s (%d)…", text, strings.Join(dims, ", "), year)
	}

	var (
		mu        sync.Mutex
		found     []match
		searched  []string
		cachedAll = true
		truncated bool
		lastUpd   string
		wg        sync.WaitGroup
	)
	errs := make([]error, len(dims))
	sem := make(chan struct{}, 2)
	for i, alias := range dims {
		wg.Add(1)
		go func(i int, alias string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			q, err := BuildQuery(year, "todo", alias, where, 1, PageSize)
			if err != nil {
				// A scope filter that collides with this dimension (e.g.
				// where=sector=11 while searching sectors) is not fatal for
				// the whole search: skip that dimension.
				return
			}
			lines, meta, cached, trunc, err := m.fetchAll(ctx, env, q, 4)
			if err != nil {
				errs[i] = err
				return
			}
			mu.Lock()
			defer mu.Unlock()
			searched = append(searched, alias)
			cachedAll = cachedAll && cached
			truncated = truncated || trunc
			if meta.LastUpdated != "" {
				lastUpd = meta.LastUpdated
			}
			for _, l := range lines {
				if matches(l, words, isCode, text) {
					found = append(found, match{dim: q.GroupBy, line: l})
				}
			}
		}(i, alias)
	}
	wg.Wait()
	// Report an upstream failure only if every dimension failed; a partial
	// answer with a note beats no answer.
	failed := 0
	var firstErr error
	for _, err := range errs {
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if failed == len(dims) && firstErr != nil {
		return nil, firstErr
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].line.Devengado > found[j].line.Devengado })
	limit := in.Int("max")
	for i, f := range found {
		if i >= limit || sink.Wanted() == 0 {
			truncated = true
			break
		}
		row := core.Row{
			"dimension": f.dim.Alias, "codigo": f.line.Code, "nombre": f.line.Name,
			"filtro": f.dim.Alias + "=" + f.line.Code,
			"pim":    f.line.PIM, "devengado": f.line.Devengado, "avance_pct": f.line.AvancePct,
		}
		if err := sink.Emit(row); err != nil {
			return nil, err
		}
	}

	res := &core.Result{
		Columns: []string{"dimension", "codigo", "nombre", "filtro", "pim", "devengado", "avance_pct"},
		Meta: map[string]any{
			"text": text, "year": year, "dimensions": searched, "matches": len(found),
			"last_updated": lastUpdated(lastUpd), "truncated": truncated, "upstream_cached": cachedAll,
		},
	}
	switch {
	case len(found) == 0:
		res.Notes = append(res.Notes, "no match: try fewer words, or list a dimension with `mef amigable gasto --by <dimension>`")
	case failed > 0:
		res.Notes = append(res.Notes, fmt.Sprintf("%d of %d dimensions could not be searched (upstream error)", failed, len(dims)))
	}
	if len(found) > 0 {
		res.Data = map[string]any{"uso": fmt.Sprintf(`mef amigable gasto --where %s --by generica`, found[0].dim.Alias+"="+found[0].line.Code)}
	}
	return res, nil
}

func lastUpdated(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stopwords are the connectors MEF includes or drops at will
// ("M. DE SALUD" but "GOBIERNO REGIONAL DEL DEPARTAMENTO DE PUNO").
var stopwords = map[string]bool{
	"de": true, "del": true, "la": true, "las": true, "el": true, "los": true, "y": true, "en": true,
}

// abbreviations map what a person types to the forms the navigator actually
// prints. The pliego listing mixes both styles in the same page: "M. DE
// SALUD" sits two rows from "MINISTERIO DE TRANSPORTES Y COMUNICACIONES", so
// requiring the full word made every "ministerio …" search fail.
var abbreviations = map[string][]string{
	"ministerio":    {"ministerio", "m."},
	"municipalidad": {"municipalidad", "mun.", "muni"},
	"gobierno":      {"gobierno", "gob."},
	"regional":      {"regional", "region"},
	"universidad":   {"universidad", "u. nac", "u.n."},
	"instituto":     {"instituto", "inst."},
	"nacional":      {"nacional", "nac."},
	"direccion":     {"direccion", "dir."},
	"presidencia":   {"presidencia", "pcm"},
	"programa":      {"programa", "prog."},
}

// searchWords splits a query into the words that must appear, dropping
// connectors. An all-stopword query keeps its words so it still matches
// something rather than everything.
func searchWords(text string) []string {
	var out []string
	for _, w := range strings.Fields(normalizeText(text)) {
		if !stopwords[w] {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		out = strings.Fields(normalizeText(text))
	}
	return out
}

func matches(l Line, words []string, isCode bool, raw string) bool {
	if isCode && (l.Code == raw || strings.TrimLeft(l.Code, "0") == strings.TrimLeft(raw, "0")) {
		return true
	}
	hay := normalizeText(l.Name)
	for _, w := range words {
		variants := abbreviations[w]
		if variants == nil {
			variants = []string{w}
		}
		found := false
		for _, v := range variants {
			if strings.Contains(hay, v) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(words) > 0
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
