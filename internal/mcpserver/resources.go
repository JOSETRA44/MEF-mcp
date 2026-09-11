package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/amigable"
	"github.com/JOSETRA44/mef-mcp/internal/modules/datosabiertos"
)

// Guide is the budget glossary served as mef://guia.
const Guide = `# Guía del presupuesto público peruano (MEF)

Montos en soles (PEN). Fuente: glosario de Consulta Amigable, MEF.

| Término | Significado |
|---|---|
| PIA | Presupuesto Institucional de Apertura: presupuesto inicial aprobado por la Ley Anual de Presupuesto. |
| PIM | Presupuesto Institucional Modificado: el PIA más las modificaciones del año. Es la base para medir avance. |
| Certificación | Garantía de que existe crédito presupuestario disponible para comprometer un gasto. |
| Compromiso anual | Monto comprometido para todo el año (contratos, planillas) tras la certificación. |
| Compromiso mensual (atención) | Parte del compromiso anual que se atiende en el mes. |
| Devengado | Obligación de pago reconocida tras la entrega del bien o servicio. **Es la cifra estándar de "ejecución".** |
| Girado | Monto efectivamente pagado (giro de cheque/transferencia). |
| Avance % | Devengado ÷ PIM × 100. |

## Niveles de gobierno
- **E** Gobierno Nacional · **R** Gobiernos Regionales · **M** Gobiernos Locales (municipalidades y mancomunidades).

## Clasificadores
- **Categoría presupuestal**: programa presupuestal (PP), acciones centrales o APNOP.
- **Función → División funcional → Grupo funcional**: para qué se gasta (salud, educación, transporte…).
- **Genérica → Subgenérica → Específica**: en qué se gasta (personal, bienes y servicios, adquisición de activos…).
- **Fuente de financiamiento → Rubro → Tipo de recurso**: con qué se paga (recursos ordinarios, canon, endeudamiento…).

## Inversión pública (Invierte.pe)
- **CUI**: código único de inversiones (7 dígitos). **SNIP**: código del sistema anterior.
- **Monto viable**: costo al declararse viable. **Costo actualizado**: costo vigente tras modificaciones.
- **Devengado acumulado**: todo lo ejecutado desde el inicio del proyecto.

## Buenas prácticas
- Compara avance usando devengado/PIM, no PIA.
- Para un año en curso, la ejecución crece mes a mes: indica la fecha de corte (meta.last_updated).
`

func addResources(srv *mcp.Server, reg *core.Registry) {
	text := func(uri, mime, body string) *mcp.ReadResourceResult {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Text: body}}}
	}
	srv.AddResource(&mcp.Resource{URI: "mef://guia", Name: "guia", Title: "Glosario del presupuesto público", MIMEType: "text/markdown",
		Description: "PIA, PIM, certificación, compromiso, devengado, girado; niveles de gobierno y clasificadores del gasto."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return text("mef://guia", "text/markdown", Guide), nil
		})

	srv.AddResource(&mcp.Resource{URI: "mef://dimensiones", Name: "dimensiones", Title: "Dimensiones de Consulta Amigable", MIMEType: "application/json",
		Description: "Aliases and navigator codes accepted by mef_gasto's by/where parameters."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			b, _ := json.MarshalIndent(amigable.Dimensions, "", "  ")
			return text("mef://dimensiones", "application/json", string(b)), nil
		})

	srv.AddResource(&mcp.Resource{URI: "mef://catalogo", Name: "catalogo", Title: "Categorías y herramientas", MIMEType: "text/markdown",
		Description: "Open-data categories and a map of every mef tool to the question it answers."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			var sb strings.Builder
			sb.WriteString("# Catálogo mef\n\n## Categorías de Datos Abiertos\n\n")
			for _, c := range datosabiertos.Categories {
				fmt.Fprintf(&sb, "- `%s` — %s\n", c.Slug, c.Title)
			}
			sb.WriteString("\n## Herramientas\n\n| Tool | Resumen |\n|---|---|\n")
			for _, c := range reg.Tools() {
				fmt.Fprintf(&sb, "| `%s` | %s |\n", c.Tool, c.Summary)
			}
			return text("mef://catalogo", "text/markdown", sb.String()), nil
		})
}

func addPrompts(srv *mcp.Server) {
	user := func(desc, body string) *mcp.GetPromptResult {
		return &mcp.GetPromptResult{Description: desc, Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: body}}}}
	}
	arg := func(req *mcp.GetPromptRequest, name, def string) string {
		if req.Params != nil {
			if v := strings.TrimSpace(req.Params.Arguments[name]); v != "" {
				return v
			}
		}
		return def
	}

	srv.AddPrompt(&mcp.Prompt{Name: "analisis_ejecucion_presupuestal", Title: "Análisis de ejecución presupuestal",
		Description: "Structured workflow to analyse budget execution of a sector, entity or region.",
		Arguments: []*mcp.PromptArgument{
			{Name: "ambito", Description: "Qué analizar, p. ej. 'Ministerio de Salud' o 'Gobierno Regional de Puno'", Required: true},
			{Name: "anio", Description: "Año fiscal, p. ej. 2026"},
		}},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			ambito, anio := arg(req, "ambito", "el sector público"), arg(req, "anio", "el año en curso")
			return user("Análisis de ejecución presupuestal", fmt.Sprintf(`Analiza la ejecución presupuestal de %s en %s con datos del MEF.

1. Lee mef://guia para usar bien PIA, PIM y devengado.
2. Ubica el ámbito con mef_gasto: agrupa por nivel, luego sector/pliego (o departamento-meta para regiones) hasta encontrar su código.
3. Con ese filtro, obtén: avance global (devengado/PIM), desglose por genérica de gasto, por fuente de financiamiento y por mes.
4. Compara con el mismo corte del año anterior (year - 1).
5. Reporta: cifras clave en soles, avance %%, qué rubros rezagan, tendencia mensual y la fecha de corte (meta.last_updated). Cita la fuente: Consulta Amigable, MEF.`, ambito, anio)), nil
		})

	srv.AddPrompt(&mcp.Prompt{Name: "ficha_inversion", Title: "Ficha de un proyecto de inversión",
		Description: "Build a complete profile of a public investment project from its name or CUI.",
		Arguments:   []*mcp.PromptArgument{{Name: "proyecto", Description: "Nombre o CUI del proyecto", Required: true}}},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			p := arg(req, "proyecto", "")
			return user("Ficha de inversión", fmt.Sprintf(`Elabora la ficha del proyecto de inversión pública "%s".

1. Si no es un CUI numérico, búscalo con mef_inversion_buscar (usa pocas palabras distintivas).
2. mef_inversion_ficha: estado, viabilidad, costo actualizado vs monto viable, devengado acumulado, avance físico.
3. mef_inversion_detalle view=anual (historia de ejecución) y view=ejecutora.
4. mef_inversion_detalle view=contratos y view=obras si es una obra.
5. Resume: qué es, quién lo ejecuta, cuánto cuesta y cuánto se ha ejecutado (%% del costo), estado actual y alertas (sobrecostos, paralización, baja ejecución).`, p)), nil
		})

	srv.AddPrompt(&mcp.Prompt{Name: "comparar_regiones", Title: "Comparar ejecución entre regiones",
		Description: "Rank Peru's departments by budget execution for a year and spending type.",
		Arguments: []*mcp.PromptArgument{
			{Name: "anio", Description: "Año fiscal, p. ej. 2026"},
			{Name: "tipo", Description: "todo, actividad o proyecto (inversión)"},
		}},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			anio, tipo := arg(req, "anio", "el año en curso"), arg(req, "tipo", "proyecto")
			return user("Comparar regiones", fmt.Sprintf(`Compara la ejecución presupuestal de los departamentos del Perú para %s (tipo de gasto: %s).

1. mef_gasto by=departamento-meta type=%s (y year si se indicó).
2. Ordena por avance_pct y también por devengado absoluto; señala los 5 mejores y los 5 peores.
3. Para el departamento con menor avance, desglosa con where=["departamento-meta=<codigo>"] by=nivel para ver si el rezago es nacional, regional o local.
4. Presenta una tabla y 3 hallazgos. Indica la fecha de corte (meta.last_updated).`, anio, tipo, tipo)), nil
		})
}
