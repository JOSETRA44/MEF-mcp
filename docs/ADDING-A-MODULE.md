# Agregar una fuente del MEF

Una fuente nueva es **un paquete** que implementa `core.Module` más **una línea** en `internal/modules/all.go`. El CLI, las tools MCP, el manifiesto `describe --json` y los tests de completitud la recogen solos.

Ejemplo completo: **Consulta Amigable de Ingresos**. Todavía no está implementada; es el siguiente módulo natural.

## 1. Investiga el endpoint y captura fixtures

Antes de escribir código, verifica en vivo y guarda respuestas reales:

```bash
mkdir -p internal/modules/ingresos/testdata
curl -s "https://…endpoint…" -o internal/modules/ingresos/testdata/rubro_2026.html
```

Anota en `docs/SOURCES.md` qué observaste (URL, parámetros, charset, cómo se ve un error, cómo se ve "no hay datos", latencias) y la fecha.

## 2. Implementa el módulo

```go
// internal/modules/ingresos/module.go
package ingresos

import (
	"context"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

type Module struct{ Endpoint string }

func New() *Module { return &Module{Endpoint: "https://…"} }

func (m *Module) ID() string      { return "ingresos" }
func (m *Module) Title() string   { return "Consulta Amigable (ejecución del ingreso)" }
func (m *Module) BaseURL() string { return m.Endpoint }

func (m *Module) Commands() []*core.Command {
	return []*core.Command{{
		Path:    []string{"ingresos", "recaudado"},
		Tool:    "mef_ingresos",
		Title:   "Ejecución del ingreso público",
		Summary: "Revenue collected (PIA, PIM, recaudado) grouped by any income dimension",
		Description: `Returns … (qué devuelve, en la primera oración).

Use it when … instead of mef_gasto, which covers spending only.

Behaviour: latencia, paginación, caché, qué significa un resultado vacío.
Not for: …`,
		Params: append([]core.Param{
			{Name: "by", Type: core.TypeString, Default: "rubro", Help: `Dimension, e.g. "rubro".`},
			{Name: "year", Type: core.TypeInt, Default: 0, Help: "Fiscal year, e.g. 2026."},
		}, core.PagingParams()...),
		RowFields:   []string{"codigo", "nombre", "pia", "pim", "recaudado"},
		Network:     true,
		SupportsAll: true,
		ReadOnly:    true,
		Examples:    []string{"mef ingresos recaudado --by rubro --year 2026"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			resp, err := env.HTTP.Do(ctx, core.Request{
				Method: "GET", URL: m.Endpoint,
				TTLKey:  "ingresos/recaudado", // entra en cache.TTLFor
				Charset: "mixed",              // si la fuente mezcla codificaciones
			})
			if err != nil {
				return nil, err
			}
			// parsea resp.Body y emite filas: sink.Emit(core.Row{...})
			_ = resp
			return &core.Result{Meta: map[string]any{}}, nil
		},
	}}
}
```

Reglas:

- **Nunca uses `net/http` directamente.** Usa siempre `env.HTTP`: así heredas caché, reintentos, cortesía por host y el `cached` veraz del envelope.
- **Emite filas con `sink.Emit`** a medida que llegan y respeta `sink.Wanted()` para dejar de paginar.
- Si las columnas vienen de la fuente, llama `sink.SetColumns(cols)` antes del primer `Emit`.
- **Clasifica errores** con `core.Usagef` / `core.NotFoundf` / `core.NewError(core.CodeAPI, …)` y da siempre un `hint` accionable.
- Un resultado vacío **no** es error (exit 0). Un recurso inexistente sí (`not_found`).
- Parámetros solo para el CLI (rutas de archivo): `CLIOnly: true`.

## 3. Regístralo (una línea)

```go
// internal/modules/all.go
return core.NewRegistry([]core.Module{
	amigable.New(),
	da,
	invierte.New(),
	ingresos.New(), // ← aquí
	local.New(da),
})
```

## 4. Tests

En `internal/modules/ingresos/ingresos_test.go`, con `internal/testutil`:

```go
fake := &testutil.Fake{Routes: []testutil.Route{
	{Match: "endpoint", Reply: testutil.Static(testutil.Fixture(t, "rubro_2026.html"))},
}}
cmd := New().Commands()[0]
sink := &testutil.Sink{}
_, err := cmd.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, cmd, map[string]any{"by": "rubro"}), sink)
```

Cubre: parseo del fixture real (con totales contrastados contra la web), errores con su clase, paginación que termina y respeta `max` sin duplicar filas.

Los tests existentes en `internal/cli` y `internal/mcpserver` verificarán automáticamente que el comando aparezca en `describe`, que sus ejemplos parseen y que la tool tenga una descripción suficiente.

## 5. Hazlo reutilizable (opcional)

Si tu fuente devuelve filas con montos, expón sus comandos por nombre y
`mef_reporte` podrá generar Excel y HTML de ellas sin cambios:

```go
// Command exposes one command by its CLI leaf name so other modules can run
// the same query instead of reimplementing it.
func (m *Module) Command(name string) *core.Command {
	switch name {
	case "recaudado":
		return m.recaudado()
	}
	return nil
}
```

`internal/modules/informes` ejecuta ese comando con un sink que captura filas,
analiza su columna de monto y escribe el documento. Para que el análisis
funcione, nombra las columnas como el resto del proyecto: `codigo`, `nombre`,
`pim`, `devengado`, `avance_pct`.

## 6. Documenta

- Fila nueva en la tabla de tools del README (`internal/meta` lo exige).
- Entrada en `CHANGELOG.md`.
- Endpoint y rarezas en `docs/SOURCES.md`.
- TTL de la clave nueva en `internal/cache/ttl.go`.
