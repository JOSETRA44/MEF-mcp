<img width="1600" height="500" alt="MEF-MCP _ README banner _ 1600 × 500 (1)" src="https://github.com/user-attachments/assets/444f5e5a-82d5-45d6-8fce-68b53e731db6" />


# mef-mcp — datos del MEF del Perú para agentes y personas

<!-- mcp-name: io.github.JOSETRA44/mef-mcp -->

CLI y servidor MCP para los datos públicos del **Ministerio de Economía y Finanzas del Perú**. Ofrece la ejecución diaria del gasto público (Consulta Amigable, 1999 a hoy), el portal de Datos Abiertos y el seguimiento de inversiones de Invierte.pe. Un solo binario Go, sin credenciales ni runtime, que arranca en milisegundos.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-0.1.0-informational.svg)](CHANGELOG.md)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)
[![Tests](https://img.shields.io/badge/tests-go%20test%20.%2F...-brightgreen.svg)](#desarrollo)

## Qué puedes preguntarle

| Pregunta | Tool que la responde |
|---|---|
| "¿Cuánto ha ejecutado el Gobierno Nacional este año y con qué avance?" | `mef_gasto` (`by=nivel`) |
| "¿Qué regiones ejecutan menos inversión en 2025?" | `mef_gasto` (`by=departamento-meta`, `type=proyecto`) |
| "¿En qué va la carretera Acos Vinchos – Urpay?" | `mef_inversion_buscar` → `mef_inversion_ficha` |
| "¿Qué contratos SEACE y obras tiene el CUI 2222116?" | `mef_inversion_detalle` (`view=contratos` / `obras`) |
| "¿Qué datos abiertos hay sobre deuda pública?" | `mef_datos_buscar` → `mef_datos_dataset` |
| "Top 10 pliegos por devengado en 2016, desde el dataset histórico" | `mef_sync` → `mef_sql` |

Ejemplo real: `mef amigable gasto --by nivel` devuelve PIM 287,487,003,991 soles y avance 62.2 % con corte al 2026-09-09. Son las mismas cifras que el total de la web del MEF.

## Instalación

**Requisito:** Go ≥ 1.26 para compilar. El binario resultante no necesita nada más: no requiere CGO ni runtime.

> **Estado de publicación:** el paquete npm (`npx -y mef-mcp`), el módulo público `go install github.com/JOSETRA44/mef-mcp/...@latest` y los releases de GitHub **aún no están publicados**. Hoy la instalación es desde el código fuente.

```bash
git clone https://github.com/JOSETRA44/mef-mcp
cd mef-mcp
go install ./cmd/...        # deja mef y mef-mcp en $(go env GOPATH)/bin
mef --version               # mef 0.1.0
```

Si `mef` no se encuentra, agrega `$(go env GOPATH)/bin` al PATH. En Windows es `%USERPROFILE%\go\bin`.

Cuando esté publicado, el camino sin instalación será `npx -y mef-mcp`.

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

`mef mcp` (subcomando oculto) arranca el mismo servidor desde el binario `mef`.

## Configuración

No hay credenciales: todas las fuentes son públicas.

| Variable | Para qué | ¿Obligatoria? | ¿Secreta? | Valor por defecto |
|---|---|---|---|---|
| `MEF_HOME` | Carpeta de la caché de respuestas y la base local | No | No | `%LOCALAPPDATA%\mef-mcp` (Windows), directorio de caché del SO (macOS/Linux) |
| `MEF_DB` | Ruta de la base SQLite de `mef_sync`/`mef_sql` | No | No | `$MEF_HOME/mef.db` |
| `NO_COLOR` | Desactiva colores en el CLI | No | No | — |

## Tools

| Tool | Qué hace | Parámetros clave |
|---|---|---|
| `mef_gasto` | Ejecución del gasto (PIA, PIM, certificación, compromiso, devengado, girado, avance %) agrupada por cualquier dimensión | `by`, `where`, `year`, `type`, `max` |
| `mef_gasto_dimensiones` | Lista las 29 dimensiones de `mef_gasto` con alias y códigos (offline) | `question` |
| `mef_datos_buscar` | Busca en el catálogo de Datos Abiertos (74 datasets) | `text`, `category`, `resources` |
| `mef_datos_dataset` | Detalle de un dataset y sus recursos (`resource_id`, URLs CSV/ZIP) | `slug` |
| `mef_datos_consultar` | Lee filas de un recurso con filtros exactos y búsqueda de texto | `resource_id`, `filter`, `q`, `max` |
| `mef_inversion_buscar` | Busca inversiones por nombre y devuelve CUIs | `text` |
| `mef_inversion_ficha` | Ficha Invierte.pe de un CUI: estado, costo, viabilidad, devengado | `cui`, `snip` |
| `mef_inversion_detalle` | Ejecución por ejecutora, año, mes, fuente o específica; contratos SEACE; obras InfoObras | `cui`, `view` |
| `mef_sync` | Carga un recurso a SQLite local, con procedencia | `resource_id`, `table`, `filter`, `max_rows` |
| `mef_sql` | SQL de solo lectura sobre lo sincronizado | `query`, `max` |
| `mef_tablas` | Lista tablas locales o perfila las columnas de una | `table` |

Resources: `mef://guia` (glosario PIA/PIM/devengado), `mef://dimensiones`, `mef://catalogo`.
Prompts: `analisis_ejecucion_presupuestal`, `ficha_inversion`, `comparar_regiones`.

Las respuestas MCP traen por defecto un máximo de 200 filas. `meta.truncated` avisa cuando hay más, y en ese caso conviene agregar con `mef_sql` o subir `max`.

## CLI for agents

```bash
go install ./cmd/...   # mef + mef-mcp
mef describe --json    # manifiesto completo: comandos, argumentos, row_fields, ejemplos
```

**Flags globales:** `-f/--format table|json|jsonl|csv|md`, `-o/--output FILE`, `-q/--quiet`, `--no-color`, `--refresh`, `--no-cache`, `--version`. Los datos van a stdout y las notas y el progreso a stderr.

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

Los errores en `json` o `jsonl` imprimen `{"ok": false, "command", "source", "fetched_at", "error": {"code", "message", "hint"}}` en stdout y una línea `error: …` en stderr.

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
# 1. Cosecha masiva a CSV: todas las unidades ejecutoras del año
mef amigable gasto --by ejecutora --all --max 5000 -f csv -o ejecutoras.csv

# 2. Pipe a jq: los 5 departamentos con menor avance en inversión
mef amigable gasto --by departamento-meta --type proyecto -f jsonl \
  | jq -s 'sort_by(.avance_pct) | .[:5] | .[] | {nombre, avance_pct}'

# 3. Consulta repetida: la segunda sale de la caché en disco (cached: true)
mef invierte ficha 2222116 -f json | jq .cached

# 4. Caso de error: una columna inválida devuelve exit 2 y el hint lista las columnas válidas
mef datos consultar 5f3b3cbe-3955-41cc-8662-1757ebb5cf53 --filter NOEXISTE=1 -f json | jq .error.hint

# 5. Cruce entre herramientas: ejecución regional del MEF junto a indicadores del BCRP/INEI
mef amigable gasto --by departamento-meta -f csv -o gasto_regional.csv
arsenal run inei ... -f csv   # une por departamento en tu herramienta de análisis
```

## Solución de problemas

**`error: resource 510bae6d-3d37-4fb2-af35-a40ce01715f4 returned no records`.** Algunos recursos muy grandes (por ejemplo el comparativo de gasto 2022–2026) no tienen el datastore cargado en el MEF. Obtén la URL del archivo con `mef datos dataset comparacion-de-presupuesto-ejecucion-gasto` y descárgalo con `mef datos descargar <url>`.

**`error: timed out after 2m30s waiting for apps5.mineco.gob.pe`.** Agrupar por `meta`, `finalidad` o `actividad` sin filtros tarda 15–30 s en el propio MEF, y a veces más. Filtra antes, por ejemplo con `--where pliego=011`. Revisa con `mef doctor` si la fuente está caída.

**`mef datos consultar ... --filter ...` o `mef sync ... --filter ...` tarda más de un minuto.** No es un bug. Con filtros, el servidor del MEF recorre la tabla entera, unos 90 s por página, aunque coincidan pocas filas. Para análisis es más rápido sincronizar sin filtro y filtrar con `mef sql "... WHERE ..."`.

**`error: no local database yet at ...\mef.db`.** `mef sql` y `mef tablas` solo leen lo que cargaste antes con `mef sync <resource_id> --table <nombre>`.

**`mef: command not found` / `"mef" no se reconoce como comando`.** `$(go env GOPATH)/bin` no está en el PATH. Agrégalo y abre una terminal nueva.

## Desarrollo

```bash
go build ./...
go vet ./...
go test ./...           # hermético: sin red, todo desde fixtures reales en testdata/
gofmt -l .              # debe salir vacío
```

La arquitectura está en [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Para agregar una fuente del MEF, en unas 30 líneas más fixtures, ver [docs/ADDING-A-MODULE.md](docs/ADDING-A-MODULE.md). Los endpoints y sus rarezas observadas están en [docs/SOURCES.md](docs/SOURCES.md).

Este proyecto no está afiliado al MEF. Los datos provienen de sus servicios públicos, que no documentan una API oficial y pueden cambiar sin aviso.

## Licencia

MIT, ver [LICENSE](LICENSE).
