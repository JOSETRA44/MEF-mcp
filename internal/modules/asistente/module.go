// Package asistente answers "which tool do I use for this question?".
//
// Sixteen tools is enough that a model arriving cold can pick the wrong one,
// and the costly mistake here is drilling blindly through dimensions when
// mef_entidades would have resolved a name in one call. This module encodes
// the playbooks as data, locally and instantly.
package asistente

import (
	"context"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Module implements core.Module.
type Module struct{}

// New returns the module.
func New() *Module { return &Module{} }

func (m *Module) ID() string      { return "asistente" }
func (m *Module) Title() string   { return "Guía de uso para agentes" }
func (m *Module) BaseURL() string { return "local://guia" }

func (m *Module) Commands() []*core.Command { return []*core.Command{m.ayuda()} }

type step struct {
	Tool string
	Args string
	Why  string
}

type playbook struct {
	Name     string
	Keywords []string
	Steps    []step
}

// playbooks are ordered by specificity: the first match wins.
var playbooks = []playbook{
	{
		Name: "Una inversión u obra concreta",
		// "inversión" alone is deliberately absent: it is just as likely to
		// mean aggregate investment spending, which mef_gasto answers.
		Keywords: []string{"obra", "proyecto", "cui", "hospital", "carretera", "colegio",
			"puente", "saneamiento", "expediente", "contrato", "seace", "paralizada", "viable", "snip"},
		Steps: []step{
			{"mef_inversion_buscar", `{"text":"hospital de ayacucho"}`, "Convierte el nombre en un CUI; el SSI devuelve hasta 20 coincidencias."},
			{"mef_inversion_ficha", `{"cui":"2081467"}`, "Estado, costo actualizado vs monto viable, devengado acumulado y avance físico."},
			{"mef_inversion_detalle", `{"cui":"2081467","view":"anual"}`, "Historia de ejecución; view=contratos para SEACE y view=obras para InfoObras."},
		},
	},
	{
		Name: "Reporte, Excel o documento para compartir",
		Keywords: []string{"reporte", "informe", "excel", "xlsx", "hoja", "hojas", "planilla de calculo", "documento",
			"pdf", "html", "entregable", "exporta", "exportar", "descargable", "compartir", "presentacion", "presentación"},
		Steps: []step{
			{"mef_entidades", `{"text":"ministerio de salud"}`, "Si la pregunta nombra una entidad, obtén primero su filtro."},
			{"mef_reporte", `{"source":"gasto","by":"generica","where":["pliego=011"],"format":"xlsx","nombre":"ejecucion-minsa"}`, "Genera el archivo; devuelve la ruta absoluta para abrirlo."},
		},
	},
	{
		Name: "Evolución, comparación entre años o tendencia",
		Keywords: []string{"evolucion", "evolución", "historico", "histórico", "tendencia", "desde", "serie",
			"compara", "comparar", "años", "anual", "variacion", "variación", "creci", "cayo", "cayó"},
		Steps: []step{
			{"mef_serie", `{"by":"departamento-meta","from":2019,"to":2026,"type":"proyecto"}`, "Un año por fila y grupo, con variación interanual y totales por año ya calculados."},
			{"mef_reporte", `{"source":"serie","by":"departamento-meta","from":2019,"to":2026,"format":"html"}`, "Si quieren verlo o compartirlo."},
		},
	},
	{
		Name: "Análisis, ranking o diagnóstico",
		Keywords: []string{"analiza", "analizar", "analisis", "análisis", "ranking", "top", "peor", "mejor", "menos",
			"mas", "más", "concentra", "rezag", "diagnostico", "diagnóstico", "resumen", "sin ejecutar", "sin gastar", "eficien"},
		Steps: []step{
			{"mef_analizar", `{"by":"pliego","where":["nivel=E"],"top":10}`, "Totales, ranking con participación, concentración del top 5, rezagados y PIM sin ejecutar, ya calculados."},
			{"mef_gasto", `{"by":"pliego","where":["nivel=E"],"sort":"avance","desc":false,"max":10}`, "Si solo quieres la tabla ordenada por avance ascendente."},
		},
	},
	{
		Name: "Una entidad nombrada (ministerio, municipalidad, región)",
		Keywords: []string{"ministerio", "municipalidad", "municipio", "gobierno regional", "universidad", "pliego",
			"entidad", "institucion", "institución", "minsa", "mtc", "midagri", "essalud", "sector", "ejecutora", "codigo de"},
		Steps: []step{
			{"mef_entidades", `{"text":"municipalidad de arequipa","where":["nivel=M"]}`, "Devuelve el código y el campo filtro listo para usar; evita adivinar o drillear a ciegas."},
			{"mef_gasto", `{"where":["ejecutora=301234"],"by":"generica"}`, "Pega el filtro y agrupa por lo que interese: generica, fuente, mes, proyecto."},
			{"mef_analizar", `{"where":["ejecutora=301234"],"by":"generica"}`, "Para conclusiones en vez de tabla."},
		},
	},
	{
		Name:     "Ejecución por mes o trimestre",
		Keywords: []string{"mes", "mensual", "meses", "trimestre", "trimestral", "estacional", "ritmo", "cuando", "cuándo"},
		Steps: []step{
			{"mef_gasto", `{"by":"mes","where":["sector=11"],"year":2026}`, "Agrupar por mes o trimestre. Ojo: PIA y PIM llegan nulos porque el presupuesto es anual; usa devengado."},
		},
	},
	{
		Name: "Regiones, departamentos o dónde se gasta",
		Keywords: []string{"region", "región", "regiones", "departamento", "departamentos", "provincia", "distrito",
			"donde se gasta", "dónde se gasta", "territorial", "geografic", "geográfic"},
		Steps: []step{
			{"mef_gasto", `{"by":"departamento-meta","type":"proyecto","sort":"devengado"}`, "departamento-meta es dónde se ejecuta el gasto; departamento es dónde está la entidad."},
			{"mef_analizar", `{"by":"departamento-meta","type":"proyecto"}`, "Ranking y rezagados entre regiones."},
		},
	},
	{
		Name: "Datos abiertos, archivos y análisis con SQL",
		Keywords: []string{"dataset", "datos abiertos", "csv", "descarga", "deuda", "planilla", "airhsp", "personal",
			"contabilidad", "historico de datos", "base de datos", "sql", "cruzar", "sincroniza"},
		Steps: []step{
			{"mef_datos_buscar", `{"text":"deuda"}`, "Encuentra el dataset y su slug."},
			{"mef_datos_dataset", `{"slug":"instrumentos-de-deuda-de-largo-plazo"}`, "Lista recursos con su resource_id y la URL del CSV."},
			{"mef_sync", `{"resource_id":"1aaf9147-0649-483d-b3ac-c3a801df80bd","table":"deuda_lp"}`, "Carga a SQLite local; sin filtros es mucho más rápido."},
			{"mef_sql", `{"query":"SELECT * FROM deuda_lp LIMIT 10"}`, "Agrega, cruza y filtra localmente; mef_tablas muestra las columnas."},
		},
	},
	{
		Name: "Ejecución del gasto en general",
		Keywords: []string{"gasto", "ejecucion", "ejecución", "presupuesto", "pim", "pia", "devengado", "avance",
			"cuanto gasto", "cuánto gastó", "cuanto ejecuto", "cuánto ejecutó", "girado", "certificacion"},
		Steps: []step{
			{"mef_gasto", `{"by":"nivel","year":2026}`, "Empieza alto (nivel, sector) y baja con where usando el código de la fila."},
			{"mef_gasto_dimensiones", `{}`, "Si dudas de qué alias usar para agrupar."},
		},
	},
}

func normalize(s string) string {
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")
	return r.Replace(strings.ToLower(strings.TrimSpace(s)))
}

// match scores a playbook by how many of its keywords appear in the question.
func match(question string) []playbook {
	q := normalize(question)
	if q == "" {
		return playbooks
	}
	type scored struct {
		p playbook
		n int
	}
	var hits []scored
	for _, p := range playbooks {
		// Count distinct normalised keywords: "ejecución" and "ejecucion" are
		// the same signal, and double-counting them used to outvote a more
		// specific playbook.
		seen := map[string]bool{}
		n := 0
		for _, k := range p.Keywords {
			nk := normalize(k)
			if seen[nk] {
				continue
			}
			seen[nk] = true
			if strings.Contains(q, nk) {
				n++
			}
		}
		if n > 0 {
			hits = append(hits, scored{p, n})
		}
	}
	if len(hits) == 0 {
		return nil
	}
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].n > hits[j-1].n; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]playbook, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.p)
	}
	return out
}

func (m *Module) ayuda() *core.Command {
	return &core.Command{
		Path:    []string{"ayuda"},
		Tool:    "mef_ayuda",
		Title:   "Qué herramienta usar para esta pregunta",
		Summary: "Route a question in Spanish to the right mef tools, with the exact arguments to pass",
		Description: `Maps a question about Peru's public finances to a sequence of mef tool calls, each with example arguments and the reason it comes in that order.

Use it when unsure where to start, or to check there is not a cheaper path — for instance that a named institution should go through mef_entidades first instead of drilling through dimensions, that several years are one mef_serie call rather than many mef_gasto calls, and that an Excel or report is mef_reporte. Called without a question it returns all the playbooks, which doubles as a map of the server.

Answered locally, with no network call and no cost. It suggests; it does not fetch: run the suggested tools afterwards. The glossary of budget terms is in the mef://guia resource and the dimension list in mef_gasto_dimensiones.`,
		Params: []core.Param{
			{Name: "question", Type: core.TypeString, Positional: true,
				Help: `The question in your own words, e.g. "¿cuánto ejecutó el Minsa este año?". Empty returns every playbook.`},
		},
		RowFields: []string{"caso", "paso", "tool", "argumentos", "por_que"},
		ReadOnly:  true,
		Examples: []string{
			`mef ayuda "¿qué región ejecuta menos inversión?"`,
			`mef ayuda "quiero un excel del gasto del ministerio de salud"`,
			"mef ayuda",
		},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			q := in.Str("question")
			books := match(q)
			cols := []string{"caso", "paso", "tool", "argumentos", "por_que"}
			sink.SetColumns(cols)
			for _, p := range books {
				for i, s := range p.Steps {
					if err := sink.Emit(core.Row{
						"caso": p.Name, "paso": i + 1, "tool": s.Tool, "argumentos": s.Args, "por_que": s.Why,
					}); err != nil {
						return nil, err
					}
				}
			}
			res := &core.Result{Columns: cols, Meta: map[string]any{"question": q, "casos": len(books)}}
			if len(books) == 0 {
				res.Notes = append(res.Notes, "ninguna receta coincide: prueba `mef ayuda` sin texto para ver todas, o `mef describe --json`")
			}
			res.Data = map[string]any{
				"recordatorios": []string{
					"El avance se calcula devengado ÷ PIM, nunca sobre el PIA.",
					"Cita siempre la fecha de corte que viene en meta.last_updated.",
					"Un resultado vacío no es un error: significa que no hay gasto que coincida.",
					"Los montos están en soles (PEN).",
					"Con filtros, el datastore de Datos Abiertos tarda ~90 s por página: sincroniza sin filtro y filtra en SQL.",
				},
			}
			return res, nil
		},
	}
}
