// Package mcpserver exposes the registry over the Model Context Protocol.
//
// Tools are not written here: each core.Command with a Tool name becomes one
// MCP tool, its JSON Schema derived from the command's parameters. Tool
// failures are returned as results with isError and the error envelope, not
// as protocol errors, so the model can read the hint and correct itself.
package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JOSETRA44/mef-mcp/internal/app"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
	"github.com/JOSETRA44/mef-mcp/internal/render"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

// Instructions are sent to clients at initialize.
const Instructions = `Datos del Ministerio de Economía y Finanzas del Perú (MEF), en vivo y sin credenciales.

Choose the tool by question:
- Budget execution ("¿cuánto gastó/ejecutó…?", avance, devengado, PIM by sector, region, entity, month) → mef_gasto (list its dimensions with mef_gasto_dimensiones or the mef://dimensiones resource).
- A specific public investment project → mef_inversion_buscar (name → CUI), mef_inversion_ficha (sheet), mef_inversion_detalle (history, contracts, works).
- Open-data files (debt, payroll AIRHSP, accounting, procurement, historical budget) → mef_datos_buscar → mef_datos_dataset → mef_datos_consultar, or mef_sync + mef_sql for analysis.

Every result is an envelope {ok, command, source, fetched_at, cached, count, meta, data, rows}. Amounts are soles (PEN). Glossary (PIA, PIM, devengado…) in mef://guia. Rows are capped at 200 by default; meta.truncated signals more — aggregate with mef_sql or raise max.`

// Server wraps the SDK server with its dependencies.
type Server struct {
	Registry *core.Registry
	Shared   *app.Shared
	Now      func() time.Time
}

// New builds the MCP server for a registry.
func New(reg *core.Registry, shared *app.Shared) (*mcp.Server, *Server) {
	s := &Server{Registry: reg, Shared: shared, Now: time.Now}
	srv := mcp.NewServer(&mcp.Implementation{Name: "mef-mcp", Title: "MEF Perú", Version: version.Get()}, &mcp.ServerOptions{Instructions: Instructions})
	for _, c := range reg.Tools() {
		srv.AddTool(toolFor(c), s.handler(c))
	}
	addResources(srv, reg)
	addPrompts(srv)
	return srv, s
}

func toolFor(c *core.Command) *mcp.Tool {
	openWorld := c.Network
	notDestructive := false
	return &mcp.Tool{
		Name:        c.Tool,
		Title:       c.Title,
		Description: c.Description,
		InputSchema: c.InputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           c.Title,
			ReadOnlyHint:    c.ReadOnly,
			IdempotentHint:  true,
			OpenWorldHint:   &openWorld,
			DestructiveHint: &notDestructive,
		},
	}
}

func (s *Server) handler(c *core.Command) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw := map[string]any{}
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
				return s.errorResult(c, core.Usagef("Arguments must be a JSON object.", "invalid arguments: %v", err)), nil
			}
		}
		for _, p := range c.Params {
			if p.CLIOnly {
				delete(raw, p.Name)
			}
		}
		in, err := core.NewInput(c.Params, raw, nil)
		if err != nil {
			return s.errorResult(c, core.AsError(err)), nil
		}
		return s.Call(ctx, c, in), nil
	}
}

// Call runs a command with a buffering sink and builds the tool result.
func (s *Server) Call(ctx context.Context, c *core.Command, in core.Input) *mcp.CallToolResult {
	env, tr := s.Shared.Env(false, nil)
	env.Now = s.Now
	max := -1
	for _, p := range c.Params {
		if p.Name == "max" {
			max = in.Int("max")
			if !in.IsSet("max") {
				max = core.MCPDefaultMax
			}
		}
	}
	w := render.NewWriter("json", io.Discard, max)
	w.Columns = append([]string(nil), c.RowFields...)
	res, err := c.Run(ctx, env, in, w)
	if err != nil {
		return s.errorResult(c, core.AsError(err))
	}
	envl := app.Envelope(c, res, tr, w, s.Now(), s.Registry.ModuleOf(c))
	envl.Rows = w.Rows()
	if envl.Rows == nil {
		envl.Rows = []core.Row{}
	}
	envl.Count = len(envl.Rows)
	if w.Truncated() > 0 {
		envl.Meta["hint"] = "Result capped by max. For totals or rankings aggregate with mef_sql after mef_sync, or raise max."
	}
	if res != nil && len(res.Notes) > 0 {
		envl.Meta["notes"] = res.Notes
	}
	if cols := render.Columns(pick(res, w), envl.Rows); len(cols) > 0 {
		envl.Meta["columns"] = cols
	}
	text, _ := json.Marshal(envl)
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: envl,
	}
}

func pick(res *core.Result, w *render.Writer) []string {
	if res != nil && len(res.Columns) > 0 {
		return res.Columns
	}
	return w.Columns
}

func (s *Server) errorResult(c *core.Command, e *core.Error) *mcp.CallToolResult {
	envl := render.ErrorEnvelope{OK: false, Command: c.Name(), Source: "mef", FetchedAt: render.Now(s.Now()), Error: e}
	text, _ := json.Marshal(envl)
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: envl,
		IsError:           true,
	}
}

// Serve runs the server on stdio until the client disconnects.
func Serve(ctx context.Context, reg *core.Registry) error {
	srv, _ := New(reg, app.NewShared(httpx.CacheNormal))
	return srv.Run(ctx, &mcp.StdioTransport{})
}
