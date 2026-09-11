package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/JOSETRA44/mef-mcp/internal/cache"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/paths"
	"github.com/JOSETRA44/mef-mcp/internal/render"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

// Builtin core commands are CLI-only (no Tool) but still flow through the
// common renderer, so `mef cache stats -f json` has a proper envelope.
func (a *App) builtinCommands() []*core.Command {
	disk := func() *cache.Disk { return cache.New(paths.CacheDir()) }
	return []*core.Command{
		{
			Path: []string{"cache", "stats"}, Summary: "Show cache location, entry count, expired entries and size",
			RowFields: []string{"path", "entries", "expired", "bytes"}, ReadOnly: true,
			Examples: []string{"mef cache stats -f json"},
			Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
				st, err := disk().Stats()
				if err != nil {
					return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
				}
				return &core.Result{Columns: []string{"path", "entries", "expired", "bytes"}}, sink.Emit(core.Row{"path": st.Path, "entries": st.Entries, "expired": st.Expired, "bytes": st.Bytes})
			},
		},
		{
			Path: []string{"cache", "clear"}, Summary: "Delete every cached response",
			RowFields: []string{"path", "removed"},
			Examples:  []string{"mef cache clear"},
			Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
				d := disk()
				n, err := d.Clear()
				if err != nil {
					return nil, core.Wrap(core.CodeInternal, err, "", "%v", err)
				}
				return &core.Result{Columns: []string{"path", "removed"}}, sink.Emit(core.Row{"path": d.Dir, "removed": n})
			},
		},
		{
			Path: []string{"cache", "path"}, Summary: "Print the cache directory and the local database path",
			RowFields: []string{"cache", "database"}, ReadOnly: true,
			Examples: []string{"mef cache path"},
			Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
				return &core.Result{Columns: []string{"cache", "database"}}, sink.Emit(core.Row{"cache": paths.CacheDir(), "database": paths.DBPath()})
			},
		},
		a.doctorCommand(),
	}
}

func (a *App) builtins() []*cobra.Command {
	var out []*cobra.Command
	groups := map[string]*cobra.Command{}
	for _, c := range a.builtinCommands() {
		leaf := a.leaf(c)
		if len(c.Path) == 1 {
			out = append(out, leaf)
			continue
		}
		g, ok := groups[c.Path[0]]
		if !ok {
			g = &cobra.Command{Use: c.Path[0], Short: "Manage the on-disk response cache", Args: cobra.NoArgs, RunE: groupHelp}
			groups[c.Path[0]] = g
			out = append(out, g)
		}
		g.AddCommand(leaf)
	}
	out = append(out, a.describeCommand())
	if a.ServeMCP != nil {
		out = append(out, &cobra.Command{
			Use:    "mcp",
			Short:  "Run the MCP server over stdio (same as the mef-mcp binary)",
			Hidden: true,
			Args:   cobra.NoArgs,
			RunE:   func(cmd *cobra.Command, args []string) error { return a.ServeMCP(cmd.Context()) },
		})
	}
	return out
}

// doctorCommand probes each upstream with its cheapest real request.
func (a *App) doctorCommand() *core.Command {
	return &core.Command{
		Path:      []string{"doctor"},
		Summary:   "Check that each MEF source answers, with latency (bypasses the cache)",
		RowFields: []string{"source", "ok", "status", "latency_ms", "detail", "url"},
		Network:   true,
		ReadOnly:  true,
		Examples:  []string{"mef doctor", "mef doctor -f json"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			type probe struct {
				name string
				req  core.Request
				ok   func([]byte) (bool, string)
			}
			probes := []probe{
				{"amigable", core.Request{Method: "GET", URL: "https://apps5.mineco.gob.pe/transparencia/Navegador/Navegar_7.aspx",
					Query: url.Values{"_raw": {"_uhc=yes&0=&1=&y=" + fmt.Sprint(time.Now().Year()) + "&cpage=1&psize=5"}}, Charset: "mixed", Timeout: 30},
					func(b []byte) (bool, string) {
						s := string(b)
						return strings.Contains(s, `name="grp1"`), "navigator grid present"
					}},
				{"datosabiertos", core.Request{Method: "POST", URL: "https://datosabiertos.mef.gob.pe/Rest/PortalWebDatasets/v1.0/getDatasets",
					JSON: map[string]any{"search": "", "sort": nil, "tags": []string{}, "formats": []string{}, "categories": []string{}, "page": 1}, Timeout: 30},
					func(b []byte) (bool, string) {
						var r struct {
							CountTotal string `json:"countTotal"`
						}
						_ = json.Unmarshal(b, &r)
						return r.CountTotal != "", "catalog datasets: " + r.CountTotal
					}},
				{"datastore", core.Request{Method: "GET", URL: "https://api.datosabiertos.mef.gob.pe/DatosAbiertos/v1/datastore_search",
					Query: url.Values{"resource_id": {"5f3b3cbe-3955-41cc-8662-1757ebb5cf53"}, "limit": {"1"}}, Timeout: 60},
					func(b []byte) (bool, string) {
						return strings.Contains(string(b), `"records"`) && !strings.Contains(string(b), `"records" : null`), "datastore returns records"
					}},
				{"invierte", core.Request{Method: "POST", URL: "https://ofi5.mef.gob.pe/invierteWS/Ssi/traeDetInvSSI",
					Form: url.Values{"id": {"2194487"}, "tipo": {"SIAF"}}, Timeout: 30},
					func(b []byte) (bool, string) {
						return strings.Contains(string(b), "NOMBRE_INVERSION"), "SSI project sheet"
					}},
			}
			rows := make([]core.Row, len(probes))
			var wg sync.WaitGroup
			for i, p := range probes {
				wg.Add(1)
				go func(i int, p probe) {
					defer wg.Done()
					start := time.Now()
					resp, err := doUncached(ctx, env, p.req)
					lat := time.Since(start).Milliseconds()
					row := core.Row{"source": p.name, "latency_ms": lat, "url": p.req.URL}
					if err != nil {
						e := core.AsError(err)
						row["ok"], row["status"], row["detail"] = false, 0, e.Message
					} else {
						good, detail := p.ok(resp.Body)
						row["ok"], row["status"], row["detail"] = good && resp.Status == 200, resp.Status, detail
					}
					rows[i] = row
				}(i, p)
			}
			wg.Wait()
			healthy := 0
			for _, r := range rows {
				if r["ok"] == true {
					healthy++
				}
			}
			if err := sink.Emit(rows...); err != nil {
				return nil, err
			}
			res := &core.Result{Columns: []string{"source", "ok", "status", "latency_ms", "detail", "url"}, Meta: map[string]any{"healthy": healthy, "total": len(rows)}}
			if healthy < len(rows) {
				res.Notes = append(res.Notes, fmt.Sprintf("%d of %d MEF sources are degraded — failures there are upstream, not in mef", len(rows)-healthy, len(rows)))
			}
			return res, nil
		},
	}
}

// doUncached bypasses the cache so doctor measures the live service.
func doUncached(ctx context.Context, env *core.Env, req core.Request) (*core.Response, error) {
	req.TTLKey = "doctor"
	type refresher interface {
		DoFresh(ctx context.Context, req core.Request) (*core.Response, error)
	}
	if r, ok := env.HTTP.(refresher); ok {
		return r.DoFresh(ctx, req)
	}
	return env.HTTP.Do(ctx, req)
}

// Manifest types (ARSENAL-SPEC §4).
type manifestArg struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Default    any    `json:"default"`
	Help       string `json:"help"`
	Repeatable bool   `json:"repeatable,omitempty"`
}

type manifestCommand struct {
	Name        string        `json:"name"`
	Summary     string        `json:"summary"`
	Tool        string        `json:"mcp_tool,omitempty"`
	Arguments   []manifestArg `json:"arguments"`
	Options     []manifestArg `json:"options"`
	RowFields   []string      `json:"row_fields"`
	SupportsAll bool          `json:"supports_all"`
	Network     bool          `json:"network"`
	Examples    []string      `json:"examples"`
}

// Manifest derives the discovery document from the parser: it walks the
// cobra tree and reads each leaf's core.Command, so an unregistered or
// renamed command cannot be missing from it.
func (a *App) Manifest(root *cobra.Command) map[string]any {
	byName := map[string]*core.Command{}
	for _, c := range append(a.Registry.Commands(), a.builtinCommands()...) {
		byName[c.Name()] = c
	}
	var cmds []manifestCommand
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if name, ok := sub.Annotations[AnnotationCommand]; ok {
				cmds = append(cmds, manifestFor(byName[name]))
				continue
			}
			if sub.Name() == "describe" {
				cmds = append(cmds, manifestCommand{
					Name: "describe", Summary: sub.Short, Arguments: []manifestArg{{Name: "command", Type: "string", Help: "Optional command path to describe, e.g. \"amigable gasto\""}},
					Options: []manifestArg{{Name: "--json", Type: "boolean", Default: false, Help: "Machine-readable manifest"}}, RowFields: []string{}, Examples: []string{"mef describe --json", "mef describe amigable gasto --json"},
				})
				continue
			}
			walk(sub)
		}
	}
	walk(root)
	sort.SliceStable(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return map[string]any{
		"name":    version.Name,
		"version": version.Get(),
		"source": map[string]any{
			"api":       "Ministerio de Economía y Finanzas del Perú (Consulta Amigable, Datos Abiertos, Invierte.pe SSI)",
			"base_url":  "https://www.mef.gob.pe",
			"auth":      "none",
			"endpoints": sourceList(a.Registry),
		},
		"formats":    render.Formats,
		"exit_codes": core.ExitCodes,
		"commands":   cmds,
	}
}

func sourceList(reg *core.Registry) []map[string]string {
	var out []map[string]string
	for _, m := range reg.Modules() {
		out = append(out, map[string]string{"module": m.ID(), "title": m.Title(), "base_url": m.BaseURL()})
	}
	return out
}

func manifestFor(c *core.Command) manifestCommand {
	mc := manifestCommand{
		Name: c.Name(), Summary: c.Summary, Tool: c.Tool, RowFields: c.RowFields,
		SupportsAll: c.SupportsAll, Network: c.Network, Examples: c.Examples,
		Arguments: []manifestArg{}, Options: []manifestArg{},
	}
	if mc.RowFields == nil {
		mc.RowFields = []string{}
	}
	if mc.Examples == nil {
		mc.Examples = []string{}
	}
	for _, p := range c.Params {
		arg := manifestArg{Name: p.Name, Type: string(p.Type), Required: p.Required, Default: p.Default, Help: p.Help, Repeatable: p.Type == core.TypeStringList}
		if len(p.Enum) > 0 {
			arg.Help += " One of: " + strings.Join(p.Enum, ", ") + "."
		}
		if p.Positional {
			mc.Arguments = append(mc.Arguments, arg)
		} else {
			arg.Name = "--" + p.Flag()
			mc.Options = append(mc.Options, arg)
		}
	}
	return mc
}

func (a *App) describeCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "describe [command...]",
		Short: "Describe every command (or one) for agents: arguments, options, row fields, examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			manifest := a.Manifest(root)
			if len(args) > 0 {
				want := strings.Join(args, " ")
				for _, c := range manifest["commands"].([]manifestCommand) {
					if c.Name == want {
						if asJSON || a.format() == "json" {
							return writeJSON(a.Stdout, c)
						}
						printCommandHuman(a, c)
						return nil
					}
				}
				return core.Usagef("Run `mef describe` to list commands.", "unknown command %q", want)
			}
			if asJSON || a.format() == "json" {
				return writeJSON(a.Stdout, manifest)
			}
			fmt.Fprintf(a.Stdout, "mef %s — %s\n\n", version.Get(), "MEF Perú: Consulta Amigable, Datos Abiertos, Invierte.pe")
			for _, c := range manifest["commands"].([]manifestCommand) {
				tool := ""
				if c.Tool != "" {
					tool = "  [MCP " + c.Tool + "]"
				}
				fmt.Fprintf(a.Stdout, "  %-22s %s%s\n", c.Name, c.Summary, tool)
			}
			fmt.Fprintf(a.Stdout, "\nFormats: %s. Machine manifest: mef describe --json\n", strings.Join(render.Formats, ", "))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the machine-readable manifest")
	return cmd
}

func printCommandHuman(a *App, c manifestCommand) {
	fmt.Fprintf(a.Stdout, "%s — %s\n", c.Name, c.Summary)
	for _, arg := range c.Arguments {
		fmt.Fprintf(a.Stdout, "  <%s> %s\n", arg.Name, arg.Help)
	}
	for _, o := range c.Options {
		fmt.Fprintf(a.Stdout, "  %s (%s) %s\n", o.Name, o.Type, o.Help)
	}
	for _, e := range c.Examples {
		fmt.Fprintf(a.Stdout, "  $ %s\n", e)
	}
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
