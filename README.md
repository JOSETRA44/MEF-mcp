<img width="1600" height="500" alt="MEF-MCP _ README banner _ 1600 × 500 (2)" src="https://github.com/user-attachments/assets/883a3817-d57f-4564-bcae-4f84138a14af" />



# mef-mcp — datos del MEF del Perú para agentes y personas

<!-- mcp-name: io.github.JOSETRA44/mef-mcp -->

CLI y servidor MCP para los datos públicos del **Ministerio de Economía y Finanzas del Perú**: la ejecución diaria del gasto público (Consulta Amigable, 1999 a hoy), el portal de Datos Abiertos y el seguimiento de inversiones de Invierte.pe. **16 herramientas** que no solo consultan: buscan entidades por nombre, calculan los agregados y escriben el entregable en Excel, HTML o Markdown.

Un binario Go, sin credenciales ni runtime, que arranca en ~37 ms.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-0.2.0-informational.svg)](CHANGELOG.md)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)
[![Tests](https://img.shields.io/badge/tests-115%20hermeticos-brightgreen.svg)](#desarrollo)

## Qué puedes preguntarle

| Pregunta | Camino |
|---|---|
| "No sé por dónde empezar" | `mef_ayuda` — enruta la pregunta y da los argumentos exactos |
| "¿Cuánto ejecutó el Ministerio de Salud y en qué?" | `mef_entidades` → `mef_gasto` |
| "¿Qué regiones ejecutan menos inversión?" | `mef_analizar` (`by=departamento-meta`, `type=proyecto`) |
| "Dame un Excel de eso" | `mef_reporte` (`format=xlsx`) |
| "¿Cómo ha evolucionado desde 2019?" | `mef_serie` (`from=2019`) |
| "¿En qué va la carretera Acos Vinchos – Urpay?" | `mef_inversion_buscar` → `mef_inversion_ficha` → `mef_inversion_detalle` |
| "¿Qué datos abiertos hay sobre deuda pública?" | `mef_datos_buscar` → `mef_datos_dataset` → `mef_sync` + `mef_sql` |

## Flujo típico

De un nombre a un entregable, en cuatro llamadas. Lo verificado el 2026-10-03 contra el servicio real:

```bash
# 1. El nombre que usa una persona → el código que exige el MEF
mef amigable entidades "ministerio de salud" --kind pliego
#   pliego=011 | M. DE SALUD | devengado 8,024.7 MM
#   (el MEF abrevia: escribe "M. DE SALUD", no "MINISTERIO DE SALUD")

# 2. La tabla, ordenada por el servidor
mef amigable gasto --where pliego=011 --by generica --sort devengado

# 3. Las conclusiones, ya calculadas
mef analizar --where pliego=011 --by generica --top 5

# 4. El archivo para enviar
mef reporte --where pliego=011 --by generica --format xlsx --nombre minsa-generica
#   → C:\...\mef-mcp\output\minsa-generica.xlsx (hojas: Resumen, Datos, Fuente)
```

Las mismas cuatro tools existen como `mef_entidades`, `mef_gasto`, `mef_analizar` y `mef_reporte` por MCP.

**Por qué el paso 3 y 4 importan:** sumar cientos de filas, calcular participaciones y maquetar un documento son justo las tareas en las que un modelo se equivoca. Aquí los números se calculan en el servidor, así que la narrativa y la tabla nunca se contradicen.

Comprobación de exactitud: `mef amigable gasto --by nivel` dio PIM 287 487 003 991 soles y 62.2 % de avance con corte al 2026-09-09, idéntico a la fila TOTAL de la web del MEF. Las cifras cambian a diario; cada respuesta trae su fecha de corte en `meta.last_updated`.

## Instalación

**Requisito:** Go ≥ 1.26 para compilar. El binario resultante no necesita nada más: ni CGO ni runtime.

> **Estado de publicación:** el paquete npm (`npx -y mef-mcp`), `go install github.com/JOSETRA44/mef-mcp/...@latest` y los releases de GitHub **aún no están publicados**. Hoy se instala desde el código fuente.

```bash
git clone https://github.com/JOSETRA44/mef-mcp
cd mef-mcp
go install ./cmd/...        # deja mef y mef-mcp en $(go env GOPATH)/bin
mef --version               # mef 0.2.0
mef ayuda                   # el mapa completo del servidor
```

Si `mef` no se encuentra, agrega `$(go env GOPATH)/bin` al PATH. En Windows es `%USERPROFILE%\go\bin`.

### Claude Code

```bash
claude mcp add mef -- mef-mcp
```

### Claude Desktop

Edita `claude_desktop_config.json`: en macOS está en `~/Library/Application Support/Claude/claude_desktop_config.json` y en Windows en `%APPDATA%\Claude\claude_desktop_config.json`.

```json
{
  "mcpServers": {
    "mef": { "command": "mef-mcp" }
  }
}
```

Si Claude Desktop no hereda tu PATH, usa la ruta absoluta, por ejemplo `"command": "C:\\Users\\TU_USUARIO\\go\\bin\\mef-mcp.exe"`.

### VS Code / Cursor

`.vscode/mcp.json`:

```json
{
  "servers": {
    "mef": { "type": "stdio", "command": "mef-mcp" }
  }
}
```

### Cualquier cliente MCP

```json
{ "command": "mef-mcp", "args": [] }
```

`mef mcp` (subcomando oculto) arranca el mismo servidor desde el binario `mef`. Tras reinstalar, **reinicia el cliente**: la lista de tools se lee al arrancar.

## Configuración

No hay credenciales: todas las fuentes son públicas.

| Variable | Para qué | ¿Obligatoria? | ¿Secreta? | Valor por defecto |
|---|---|---|---|---|
| `MEF_HOME` | Carpeta de la caché de respuestas y la base local | No | No | `%LOCALAPPDATA%\mef-mcp` (Windows), directorio de caché del SO (macOS/Linux) |
| `MEF_DB` | Ruta de la base SQLite de `mef_sync`/`mef_sql` | No | No | `$MEF_HOME/mef.db` |
| `MEF_OUTPUT` | Carpeta donde `mef_reporte` escribe los archivos | No | No | `$MEF_HOME/output` |
| `NO_COLOR` | Desactiva colores en el CLI | No | No | — |

## Tools

**Orientación**

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_ayuda` | Enruta una pregunta en español a las tools y argumentos exactos; sin pregunta, devuelve todas las recetas (offline) | `question` |
| `mef_entidades` | Busca sector, pliego, ejecutora o municipalidad **por nombre** y devuelve el filtro listo (`pliego=011`) | `text`, `kind`, `where` |
| `mef_gasto_dimensiones` | Las 29 dimensiones de `mef_gasto` con alias, códigos y cuáles son lentas (offline) | `question` |

**Gasto público (Consulta Amigable, actualización diaria)**

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_gasto` | Ejecución del gasto (PIA, PIM, certificación, compromiso, devengado, girado, avance %) agrupada por cualquier dimensión, con orden y top-N en el servidor | `by`, `where`, `year`, `type`, `sort`, `max` |
| `mef_serie` | El mismo agrupamiento en un rango de años, con variación interanual y totales por año | `by`, `where`, `from`, `to`, `type` |

**Análisis y entregables**

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_analizar` | Hallazgos calculados: totales y avance, ranking con participación, concentración del top 5, rezagados con presupuesto, PIM sin ejecutar | `by`, `where`, `metric`, `top` |
| `mef_reporte` | Escribe el archivo: Excel de 3 hojas con gráfico nativo, HTML autocontenido o memo Markdown | `source`, `format`, `by`, `nombre` |

**Inversión pública (Invierte.pe / SSI)**

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_inversion_buscar` | Busca inversiones por nombre y devuelve CUIs | `text` |
| `mef_inversion_ficha` | Ficha de un CUI: estado, costo actualizado, viabilidad, devengado acumulado, avance físico | `cui`, `snip` |
| `mef_inversion_detalle` | Ejecución por ejecutora, año, mes, fuente o específica; contratos SEACE; obras de InfoObras | `cui`, `view` |

**Datos Abiertos y base local**

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_datos_buscar` | Busca en el catálogo (74 datasets, 6 categorías) | `text`, `category`, `resources` |
| `mef_datos_dataset` | Detalle de un dataset y sus recursos (`resource_id`, URLs CSV/ZIP) | `slug` |
| `mef_datos_consultar` | Lee filas de un recurso con filtros exactos y búsqueda de texto | `resource_id`, `filter`, `q`, `max` |
| `mef_sync` | Carga un recurso a SQLite local, con procedencia y tipos seguros para códigos | `resource_id`, `table`, `filter`, `max_rows` |
| `mef_sql` | SQL de solo lectura sobre lo sincronizado | `query`, `max` |
| `mef_tablas` | Lista las tablas locales o perfila las columnas de una | `table` |

**Resources:** `mef://guia` (glosario PIA/PIM/devengado y buenas prácticas), `mef://dimensiones`, `mef://catalogo`.
**Prompts:** `analisis_ejecucion_presupuestal`, `ficha_inversion`, `comparar_regiones`.

Las respuestas MCP devuelven como máximo 200 filas. `meta.truncated` avisa cuando hay más: conviene agregar con `mef_analizar` o `mef_sql` antes que subir `max`.

### Qué contiene cada formato de reporte

| Formato | Contenido |
|---|---|
| `xlsx` | Hoja **Resumen** (indicadores, hallazgos, gráfico de barras nativo y editable), **Datos** (tabla completa, encabezado fijo, autofiltro, montos como números reales) y **Fuente** (procedencia, URL de la consulta, fecha de corte) |
| `html` | Página autocontenida: sin CDN ni scripts, abre sin internet, se imprime a PDF, con gráfico SVG en línea y modo oscuro |
| `md` | Memo con resumen, hallazgos y tabla, para pegar en un repo o una respuesta |

Fuentes de datos (`source`): `gasto` (un año), `serie` (varios años) o `sql` (una consulta sobre la base local).

## CLI for agents

```bash
go install ./cmd/...   # mef + mef-mcp
mef describe --json    # manifiesto: 23 comandos con argumentos, row_fields y ejemplos
mef ayuda              # las recetas, en texto
```

**Flags globales:** `-f/--format table|json|jsonl|csv|md`, `-o/--output FILE`, `-q/--quiet`, `--no-color`, `--refresh`, `--no-cache`, `--version`. Los datos van a stdout; notas y progreso, a stderr.

**Envelope** (`-f json`):

```json
{
  "ok": true,
  "command": "amigable gasto",
  "source": "mef",
  "fetched_at": "2026-09-10T23:25:55Z",
  "cached": false,
  "count": 3,
  "meta": {"module": "amigable", "last_updated": "2026-09-09", "has_more": false, "max": 1000},
  "data": {"totales": {"pim": 287487003991, "devengado": 178916037899.54, "avance_pct": 62.2}},
  "rows": [{"codigo": "E", "nombre": "GOBIERNO NACIONAL", "pim": 164156985205, "avance_pct": 66}]
}
```

Los errores en `json` o `jsonl` imprimen `{"ok": false, "command", "source", "fetched_at", "error": {"code", "message", "hint"}}` en stdout y una línea `error: …` en stderr. El `hint` siempre dice qué hacer: ante una columna inválida, por ejemplo, lista las válidas.

**Exit codes**

| Código | Significado |
|---|---|
| 0 | éxito (un resultado vacío también es éxito) |
| 1 | `api`: la fuente del MEF respondió un error o algo inesperado |
| 2 | `usage`: flag, argumento, dimensión o columna inválida |
| 3 | `not_found`: recurso, CUI o dataset inexistente |
| 4 | `auth`: n/a, las fuentes son públicas |
| 5 | `rate_limit`: el MEF limitó las solicitudes (HTTP 429/503) |
| 6 | `network`: timeout, DNS o conexión |

**Recetas**

```bash
# 1. Un entregable completo en una línea
mef reporte --by pliego --where nivel=E --format xlsx --nombre ejecucion-pliegos

# 2. Cosecha masiva a CSV: todas las unidades ejecutoras del año
mef amigable gasto --by ejecutora --all --max 5000 -f csv -o ejecutoras.csv

# 3. Top-N sin ordenar a mano (lo hace el servidor)
mef amigable gasto --by departamento-meta --type proyecto --sort avance --desc=false --max 5

# 4. Serie de 8 años a jq
mef amigable serie --by nivel --from 2019 --to 2026 -f jsonl | jq -s 'group_by(.anio)[] | {anio: .[0].anio, dev: (map(.devengado) | add)}'

# 5. Consulta repetida: la segunda sale de la caché en disco (cached: true)
mef invierte ficha 2222116 -f json | jq .cached

# 6. Caso de error: una columna inválida devuelve exit 2 y el hint lista las válidas
mef datos consultar 5f3b3cbe-3955-41cc-8662-1757ebb5cf53 --filter NOEXISTE=1 -f json | jq .error.hint

# 7. Cruce entre herramientas del arsenal
mef amigable gasto --by departamento-meta -f csv -o gasto_regional.csv
arsenal run inei ... -f csv   # une por departamento en tu herramienta de análisis
```

## Solución de problemas

**Busco una entidad por nombre y no aparece.** El MEF abrevia sin criterio: en la misma lista escribe `M. DE SALUD` y `MINISTERIO DE TRANSPORTES Y COMUNICACIONES`. `mef_entidades` ya resuelve las abreviaturas más comunes e ignora conectores, pero ante una sin match usa menos palabras (`salud` en vez de `ministerio de salud`), prueba otra dimensión con `--kind` (`pliego`, `ejecutora`, `municipalidad`) o lista todo con `mef amigable gasto --by pliego`.

**`error: resource 510bae6d-… returned no records`.** Algunos recursos muy grandes (el comparativo de gasto 2022–2026, por ejemplo) no tienen datastore cargado en el MEF. Obtén la URL del archivo con `mef datos dataset comparacion-de-presupuesto-ejecucion-gasto` y descárgalo con `mef datos descargar <url>`.

**`error: timed out after 2m30s waiting for apps5.mineco.gob.pe`.** Agrupar por `meta`, `finalidad` o `actividad` sin filtros tarda 15–30 s en el propio MEF, y a veces más. Filtra antes, por ejemplo con `--where pliego=011`. Revisa con `mef doctor` si la fuente está caída.

**`mef datos consultar … --filter` o `mef sync … --filter` tarda más de un minuto.** No es un bug. Con filtros, el servidor del MEF recorre la tabla entera —unos 90 s por página— aunque coincidan pocas filas. Para análisis es más rápido sincronizar sin filtro y filtrar con `mef sql "… WHERE …"`.

**¿Dónde quedó el archivo de `mef reporte`?** En `$MEF_HOME/output`; la ruta exacta la imprime `mef cache path` y también `meta.directorio` de la respuesta. Cámbiala con `MEF_OUTPUT` o usa `--dest ruta/completa.xlsx` desde el CLI. Un archivo con el mismo nombre se reemplaza.

**`error: no local database yet at …\mef.db`.** `mef sql` y `mef tablas` solo leen lo que cargaste antes con `mef sync <resource_id> --table <nombre>`.

**Agrupo por `mes` y PIA y PIM vienen vacíos.** Es correcto: el presupuesto es anual, el MEF no lo reparte por mes. Para dimensiones de tiempo usa `devengado`.

**`mef: command not found` / `"mef" no se reconoce como comando`.** `$(go env GOPATH)/bin` no está en el PATH. Agrégalo y abre una terminal nueva.

## Desarrollo

```bash
go build ./...
go vet ./...
go test ./...           # 115 tests herméticos: sin red, desde fixtures reales en testdata/
gofmt -l .              # debe salir vacío
```

| Documento | Para qué |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Cómo una sola declaración por comando produce el CLI, la tool MCP y el manifiesto |
| [docs/ADDING-A-MODULE.md](docs/ADDING-A-MODULE.md) | Agregar una fuente del MEF: un paquete y una línea |
| [docs/SOURCES.md](docs/SOURCES.md) | Los endpoints reales y sus rarezas, con fecha de verificación |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contrato del arsenal, fixtures y expectativas de un PR |

Este proyecto no está afiliado al MEF. Los datos provienen de sus servicios públicos, que no documentan una API oficial y pueden cambiar sin aviso.

## Licencia

MIT, ver [LICENSE](LICENSE).
