---
name: mef
description: Get real data from Peru's Ministerio de Economía y Finanzas (MEF) — public budget execution (presupuesto, PIA, PIM, devengado, avance de ejecución) by sector, pliego, región, municipalidad, función or month from Consulta Amigable; public investment projects by CUI or name from Invierte.pe (estado, costo, avance, contratos SEACE, obras); and MEF open-data files (deuda pública, planillas AIRHSP, contabilidad). Use this skill whenever the user asks how much a Peruvian public entity spent or executed, "¿cuánto gastó el Minsa?", "avance de ejecución presupuestal", "¿qué región ejecuta menos?", "consulta amigable", "presupuesto de la municipalidad de X", a CUI or a public works project, "¿en qué va la obra…?", or MEF datasets — even if they do not say "MEF". Prefer it over web search or memory: MEF figures change daily and only these tools return the current official numbers.
---

# MEF Perú (mef-mcp)

Datos oficiales y en vivo del MEF. No necesita credenciales. Hay dos formas de acceso equivalentes:

- **Tools MCP** `mef_*`, si el servidor `mef` está conectado.
- **CLI** `mef`, desde una terminal. Descubre todo con `mef describe --json`.

## Elige la herramienta por la pregunta

| Pregunta | Tool | CLI |
|---|---|---|
| No sé por dónde empezar | `mef_ayuda` | `mef ayuda "<pregunta>"` |
| El nombre de una entidad (ministerio, municipalidad, región) | `mef_entidades` | `mef amigable entidades "<nombre>"` |
| Ejecución o gasto de una entidad, sector o región; avance % | `mef_gasto` | `mef amigable gasto` |
| Evolución, historia o comparación entre años | `mef_serie` | `mef amigable serie` |
| Ranking, concentración, rezagados, diagnóstico | `mef_analizar` | `mef analizar` |
| Excel, reporte, informe o documento para compartir | `mef_reporte` | `mef reporte` |
| Qué dimensiones existen y sus alias | `mef_gasto_dimensiones` | `mef amigable dimensiones` |
| Un proyecto de inversión por nombre | `mef_inversion_buscar` → `mef_inversion_ficha` | `mef invierte buscar` / `ficha` |
| Historia, contratos u obras de un CUI | `mef_inversion_detalle` | `mef invierte detalle --view anual\|contratos\|obras` |
| Datasets abiertos (deuda, planillas…) | `mef_datos_buscar` → `mef_datos_dataset` → `mef_datos_consultar` | `mef datos …` |
| Análisis con SQL sobre un dataset | `mef_sync` → `mef_tablas` → `mef_sql` | `mef sync` / `tablas` / `sql` |

## Cómo navegar `mef_gasto` (drill-down)

1. **Si la pregunta nombra una entidad, empieza por `mef_entidades`**: devuelve el código y el campo `filtro` listo para pegar en `where`. No adivines códigos ni bajes a ciegas. Ojo: el MEF abrevia sin criterio —escribe `M. DE SALUD` junto a `MINISTERIO DE TRANSPORTES Y COMUNICACIONES`—, así que si una búsqueda no encuentra nada, usa menos palabras (`salud`) o cambia de `kind` (`pliego`, `ejecutora`, `municipalidad`).
2. Si no, empieza agrupando alto: `by=nivel` (E nacional, R regional, M local) o `by=sector`.
3. Toma el `codigo` de la fila que interesa y fíjalo en `where`: `where=["nivel=E"]`, `by=sector`. Luego `where=["nivel=E","sector=11"]`, `by=pliego`, y así sucesivamente.
3. **Regiones:** `by=departamento-meta` indica dónde se ejecuta el gasto. `departamento` es dónde está la entidad.
4. **Inversión vs gasto corriente:** `type=proyecto` o `type=actividad`.
5. Agrupar por `mes` o `trimestre` da PIA y PIM **nulos**, porque el presupuesto es anual. Para esas dimensiones usa `devengado`.
6. `meta`, `finalidad` y `actividad` sin filtros tardan 15–30 s: filtra primero.

## No calcules a mano, ni maquetes a mano

- **Ranking o top-N:** `mef_gasto` con `sort=devengado` (o `sort=avance`, `desc=false` para los rezagados) más `max`. Ordena el servidor.
- **Totales, concentración, quién no ejecuta:** `mef_analizar`. Trae los números ya calculados y una frase por hallazgo que puedes citar tal cual.
- **Varios años:** `mef_serie` con `from`/`to`, no una llamada por año. Incluye la variación interanual.
- **Excel, informe o algo para enviar:** `mef_reporte` (`format=xlsx|html|md`). Devuelve la ruta absoluta del archivo; menciónala al usuario. El HTML abre sin internet y se imprime a PDF.

## Reglas para responder bien

- El **avance** es devengado ÷ PIM, no ÷ PIA. Cita siempre la fecha de corte `meta.last_updated`.
- Los montos están en **soles**. Da cifras redondeadas (millones o miles de millones) además de las exactas cuando ayude.
- Un resultado vacío no es un error: significa que no hay gasto que coincida.
- Si `meta.truncated` es true, no sumes a mano: pide los totales en `data.totales` (en `mef_gasto` sin truncar) o usa `mef_sync` + `mef_sql`.
- Los filtros de `mef_datos_consultar` y `mef_sync` hacen que el MEF tarde ~90 s por página. Para análisis, sincroniza sin filtro y filtra en SQL.
- Ante un error, lee `error.hint`: suele decir exactamente qué corregir (por ejemplo, la lista de columnas válidas).
- Termina citando la fuente: "Fuente: MEF, Consulta Amigable" (o Datos Abiertos / Invierte.pe) con la fecha.

Glosario completo en el resource `mef://guia`.
