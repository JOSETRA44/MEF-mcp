package datosabiertos

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Module implements core.Module.
type Module struct{ EP Endpoints }

// New returns the module pointed at production.
func New() *Module { return &Module{EP: Production} }

func (m *Module) ID() string      { return "datosabiertos" }
func (m *Module) Title() string   { return "Datos Abiertos del MEF" }
func (m *Module) BaseURL() string { return "https://datosabiertos.mef.gob.pe" }

func (m *Module) Commands() []*core.Command {
	return []*core.Command{m.buscar(), m.dataset(), m.consultar(), m.descargar(), m.categorias()}
}

func categorySlugs() []string {
	out := make([]string, len(Categories))
	for i, c := range Categories {
		out[i] = c.Slug
	}
	return out
}

func (m *Module) buscar() *core.Command {
	return &core.Command{
		Path:    []string{"datos", "buscar"},
		Tool:    "mef_datos_buscar",
		Title:   "Buscar datasets del MEF",
		Summary: "Search MEF's open-data catalog (74 datasets, 6 categories); optionally list each dataset's resources",
		Description: `Searches the catalog of datosabiertos.mef.gob.pe — about 74 datasets across budget (presupuesto), public investment, public accounting, debt and treasury, public-sector payroll (AIRHSP) and procurement — by a substring of title or description, and returns dataset slugs, titles, formats and tags.

Use it to discover what exists before reading data: pass the returned "dataset" slug to mef_datos_dataset for its resources, then a resource_id to mef_datos_consultar or mef_sync. Set "resources" true to resolve each hit's resources in the same call (one extra request per dataset, run in parallel), which yields resource_id rows directly.

Behaviour: case-insensitive substring match (use one or two words: "canon", "deuda", "planilla"); 10 datasets per upstream page, paged automatically with "all". Cached for hours. Empty text lists the whole catalog. An empty result means no title or description matches. Not for live budget execution figures — use mef_gasto.`,
		Params: append([]core.Param{
			{Name: "text", Type: core.TypeString, Positional: true, Default: "", Help: `Words to match in title/description, e.g. "canon minero". Empty lists everything.`},
			{Name: "category", Type: core.TypeString, Enum: categorySlugs(), Help: `Restrict to a category slug, e.g. "presupuesto-publico".`},
			{Name: "resources", Type: core.TypeBool, Default: false, Help: "Also resolve each dataset's resources (resource_id, format, url) — one row per resource."},
			{Name: "page", Type: core.TypeInt, Default: 1, Min: core.IntPtr(1), Help: "Catalog page when not using all, e.g. 2."},
		}, core.PagingParams()...),
		RowFields:   []string{"dataset", "titulo", "formatos", "etiquetas", "descripcion"},
		Network:     true,
		SupportsAll: true,
		ReadOnly:    true,
		Examples: []string{
			`mef datos buscar canon`,
			`mef datos buscar deuda --resources -f json`,
			`mef datos buscar --category presupuesto-publico --all -f csv -o catalogo.csv`,
		},
		Run: m.runBuscar,
	}
}

func (m *Module) runBuscar(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	page, all := in.Int("page"), in.Bool("all")
	withRes := in.Bool("resources")
	cached := true
	total, pages := "", 0
	for {
		if sink.Wanted() == 0 {
			break
		}
		sr, c, err := m.search(ctx, env, in.Str("text"), in.Str("category"), page)
		if err != nil {
			return nil, err
		}
		cached = cached && c
		total, pages = sr.CountTotal, sr.PagesCount
		if !withRes {
			for _, d := range sr.Datasets {
				if err := sink.Emit(core.Row{"dataset": d.Slug, "titulo": d.Title, "formatos": d.Formats, "etiquetas": d.Tags, "descripcion": d.Description}); err != nil {
					return nil, err
				}
			}
		} else {
			details := make([]*detailResponse, len(sr.Datasets))
			errs := make([]error, len(sr.Datasets))
			var wg sync.WaitGroup
			sem := make(chan struct{}, 4)
			for i, d := range sr.Datasets {
				wg.Add(1)
				go func(i int, slug string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					details[i], _, errs[i] = m.detail(ctx, env, slug)
				}(i, d.Slug)
			}
			wg.Wait()
			for i, d := range sr.Datasets {
				if errs[i] != nil {
					if env.Progress != nil {
						env.Progress("skipping %s: %v", d.Slug, errs[i])
					}
					continue
				}
				for _, r := range details[i].Resources {
					if err := sink.Emit(core.Row{"dataset": d.Slug, "titulo": d.Title, "resource_id": r.ID, "recurso": r.Title, "formato": strings.ToUpper(r.Format), "url": r.URL}); err != nil {
						return nil, err
					}
				}
			}
		}
		if !all || page >= sr.PagesCount || len(sr.Datasets) == 0 {
			break
		}
		page++
	}
	cols := []string{"dataset", "titulo", "formatos", "etiquetas", "descripcion"}
	if withRes {
		cols = []string{"dataset", "titulo", "resource_id", "recurso", "formato", "url"}
	}
	return &core.Result{Columns: cols, Meta: map[string]any{"total_datasets": total, "pages": pages, "last_page": page, "upstream_cached": cached}}, nil
}

func (m *Module) dataset() *core.Command {
	return &core.Command{
		Path:    []string{"datos", "dataset"},
		Tool:    "mef_datos_dataset",
		Title:   "Detalle de un dataset del MEF",
		Summary: "Show a dataset's metadata and its downloadable resources (resource_id, format, CSV/ZIP URLs)",
		Description: `Returns one open-data dataset by slug: title, description, category, tags, creation/modification dates, and one row per resource with its resource_id, format, direct download URL (CSV) and ZIP URL, plus the resource description (often the years it covers).

Use it after mef_datos_buscar to pick a resource: resource_id feeds mef_datos_consultar (read rows with filters) and mef_sync (load into local SQL); the url column is the full file for bulk offline analysis. Cached for hours. Fails with not_found if the slug does not exist — slugs are the "dataset" column of mef_datos_buscar, e.g. "comparacion-de-presupuesto-ejecucion-gasto".`,
		Params: []core.Param{
			{Name: "slug", Type: core.TypeString, Positional: true, Required: true, Help: `Dataset slug, e.g. "presupuesto-y-ejecucion-de-gasto".`},
		},
		RowFields: []string{"resource_id", "recurso", "formato", "url", "url_zip", "descripcion"},
		Network:   true,
		ReadOnly:  true,
		Examples:  []string{"mef datos dataset comparacion-de-presupuesto-ejecucion-gasto", "mef datos dataset indices-soberanos -f json"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			dr, cached, err := m.detail(ctx, env, in.Str("slug"))
			if err != nil {
				return nil, err
			}
			for _, r := range dr.Resources {
				if err := sink.Emit(core.Row{"resource_id": r.ID, "recurso": r.Title, "formato": strings.ToUpper(r.Format), "url": r.URL, "url_zip": r.ZipURL, "descripcion": r.Description}); err != nil {
					return nil, err
				}
			}
			tags := make([]string, 0, len(dr.Tags))
			for _, t := range dr.Tags {
				tags = append(tags, t.Tag)
			}
			return &core.Result{
				Columns: []string{"resource_id", "recurso", "formato", "url", "url_zip", "descripcion"},
				Data: map[string]any{
					"dataset": dr.Dataset.Slug, "uuid": dr.Dataset.UUID, "titulo": dr.Dataset.Title, "descripcion": dr.Dataset.Description,
					"categoria": dr.Category.Slug, "categoria_titulo": dr.Category.Title, "etiquetas": tags,
					"creado": dr.Dataset.Created, "modificado": dr.Dataset.Modified,
				},
				Meta: map[string]any{"upstream_cached": cached, "portal_url": "https://datosabiertos.mef.gob.pe/dataset/" + dr.Dataset.Slug},
			}, nil
		},
	}
}

func (m *Module) consultar() *core.Command {
	return &core.Command{
		Path:    []string{"datos", "consultar"},
		Tool:    "mef_datos_consultar",
		Title:   "Consultar filas de un recurso del MEF",
		Summary: "Read rows of an open-data resource with exact-match column filters and full-text search, paginated",
		Description: `Reads records from one open-data resource through MEF's datastore API: every column of the source file (sorted by name), values as text so codes keep their leading zeros.

Use it to inspect or extract a slice of a resource: "filter" does exact, case-sensitive equality on column names (e.g. ["SECTOR=13","DEPARTAMENTO_EJECUTORA=04"]) and "q" is a full-text search across the row. For repeated analysis, joins or aggregates over many rows prefer mef_sync followed by mef_sql, which avoids re-downloading.

Behaviour: offset pagination, page_size up to 10000 rows per request, paged automatically with "all" up to "max". Latency is set by MEF's server: an unfiltered page takes 2–20 s, but any "filter" or "q" makes it scan the whole table — about 90 s per page regardless of how few rows match — so keep "max" small when filtering. Cached for hours. An unknown column fails with a usage error whose hint lists every valid column name. A resource with no datastore loaded (some very large files) fails with not_found and points to the file URL from mef_datos_dataset. Zero rows with filters is a valid empty result.`,
		Params: append([]core.Param{
			{Name: "resource_id", Type: core.TypeString, Positional: true, Required: true, Help: `Resource UUID from mef_datos_dataset, e.g. "5f3b3cbe-3955-41cc-8662-1757ebb5cf53".`},
			{Name: "filter", Type: core.TypeStringList, Short: "F", Help: `Exact-match filters COLUMN=VALUE, e.g. ["SECTOR=13"]. Column names are case-sensitive.`},
			{Name: "q", Type: core.TypeString, Help: `Full-text search across all columns, e.g. "AREQUIPA".`},
			{Name: "offset", Type: core.TypeInt, Default: 0, Min: core.IntPtr(0), Help: "Rows to skip, e.g. 1000."},
			{Name: "page_size", Type: core.TypeInt, Default: 1000, Min: core.IntPtr(1), Max: core.IntPtr(10000), Help: "Rows per upstream request, 1–10000."},
		}, core.PagingParams()...),
		Network:     true,
		SupportsAll: true,
		ReadOnly:    true,
		Examples: []string{
			"mef datos consultar 5f3b3cbe-3955-41cc-8662-1757ebb5cf53 --max 20",
			"mef datos consultar 5f3b3cbe-3955-41cc-8662-1757ebb5cf53 --filter SECTOR=13 --all --max 50000 -f jsonl",
		},
		Run: m.runConsultar,
	}
}

func (m *Module) runConsultar(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	rid := in.Str("resource_id")
	filters, err := in.Pairs("filter")
	if err != nil {
		return nil, err
	}
	offset, size, all := in.Int("offset"), in.Int("page_size"), in.Bool("all")
	var cols []string
	cached, fetched, pages := true, 0, 0
	hasMore := false
	for {
		want := sink.Wanted()
		if want == 0 {
			hasMore = true
			break
		}
		limit := size
		if want > 0 && want < limit {
			limit = want
		}
		pg, err := m.Datastore(ctx, env, rid, filters, in.Str("q"), limit, offset)
		if err != nil {
			return nil, err
		}
		pages++
		cached = cached && pg.Cached
		if pg.Null {
			if fetched == 0 && offset == 0 && len(filters) == 0 && in.Str("q") == "" {
				return nil, core.NotFoundf(
					"This resource has no datastore loaded (common for multi-GB files). Get its CSV url from `mef datos dataset <slug>` and download it instead.",
					"resource %s returned no records", rid)
			}
			break
		}
		if cols == nil {
			cols = pg.Columns
			sink.SetColumns(cols)
		}
		if err := sink.Emit(pg.Rows...); err != nil {
			return nil, err
		}
		fetched += len(pg.Rows)
		offset += len(pg.Rows)
		if len(pg.Rows) < limit {
			break
		}
		hasMore = true
		if !all {
			break
		}
		if env.Progress != nil {
			env.Progress("fetched %d rows…", fetched)
		}
	}
	return &core.Result{Columns: cols, Meta: map[string]any{
		"resource_id": rid, "pages": pages, "next_offset": offset, "has_more": hasMore, "upstream_cached": cached,
	}}, nil
}

func (m *Module) descargar() *core.Command {
	return &core.Command{
		Path:    []string{"datos", "descargar"},
		Summary: "Download a resource file (CSV/ZIP) from fs.datosabiertos.mef.gob.pe to disk, streaming",
		Params: []core.Param{
			{Name: "url", Type: core.TypeString, Positional: true, Required: true, Help: "File URL from `mef datos dataset` (url or url_zip column)."},
			{Name: "dest", Type: core.TypeString, Short: "d", CLIOnly: true, Help: "Destination path; defaults to the file name in the current directory."},
		},
		RowFields: []string{"archivo", "bytes", "url"},
		Network:   true,
		Examples:  []string{"mef datos descargar https://fs.datosabiertos.mef.gob.pe/datastorefiles/comparativo_gastos_2012_2016.zip"},
		Run:       m.runDescargar,
	}
}

func (m *Module) categorias() *core.Command {
	return &core.Command{
		Path:      []string{"datos", "categorias"},
		Summary:   "List the six catalog categories (offline)",
		RowFields: []string{"categoria", "titulo"},
		ReadOnly:  true,
		Examples:  []string{"mef datos categorias"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			for _, c := range Categories {
				if err := sink.Emit(core.Row{"categoria": c.Slug, "titulo": c.Title}); err != nil {
					return nil, err
				}
			}
			return &core.Result{Columns: []string{"categoria", "titulo"}}, nil
		},
	}
}

// allowedDownload keeps the downloader from being pointed at arbitrary hosts.
func (m *Module) allowedDownload(u string) error {
	if strings.HasPrefix(u, m.EP.Files) {
		return nil
	}
	return core.Usagef(fmt.Sprintf("Only files under %s are downloadable; take the url column of `mef datos dataset`.", m.EP.Files), "refusing to download %q", u)
}
