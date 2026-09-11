// Package cli builds the `mef` command line from the module registry.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JOSETRA44/mef-mcp/internal/app"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
	"github.com/JOSETRA44/mef-mcp/internal/render"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

// Globals are the ARSENAL-SPEC §1 flags shared by every subcommand.
type Globals struct {
	Format  string
	Output  string
	Quiet   bool
	NoColor bool
	Refresh bool
	NoCache bool
	JSON    bool // hidden legacy alias for --format json
}

// App is one CLI invocation context.
type App struct {
	Registry *core.Registry
	Stdout   io.Writer
	Stderr   io.Writer
	G        Globals
	// NewShared builds dependencies; tests replace it with fakes.
	NewShared func(mode httpx.CacheMode) *app.Shared
	Now       func() time.Time
	// ServeMCP runs the MCP server (wired by main to avoid an import cycle).
	ServeMCP func(ctx context.Context) error

	exit int
}

// NewApp returns an App bound to real stdio.
func NewApp(reg *core.Registry) *App {
	return &App{Registry: reg, Stdout: os.Stdout, Stderr: os.Stderr, NewShared: app.NewShared, Now: time.Now}
}

func (a *App) moduleOf(c *core.Command) string { return a.Registry.ModuleOf(c) }

// Root builds the full command tree.
func (a *App) Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "mef",
		Short: "Datos del Ministerio de Economía y Finanzas del Perú, para personas y agentes",
		Long: `mef consulta en vivo las fuentes públicas del MEF (Perú):

  amigable   Consulta Amigable: ejecución diaria del gasto público (1999–hoy)
  datos      Datos Abiertos: catálogo, recursos y descargas
  invierte   Invierte.pe / SSI: fichas de inversión por CUI
  sync/sql   Base SQLite local para análisis con SQL

Salida en table, json, jsonl, csv o md. Descubre todo con: mef describe --json`,
		Version:       version.Get(),
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
	}
	root.SetVersionTemplate("mef {{.Version}}\n")
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	pf := root.PersistentFlags()
	pf.StringVarP(&a.G.Format, "format", "f", "table", "output format: table|json|jsonl|csv|md")
	pf.StringVarP(&a.G.Output, "output", "o", "", "write output to FILE instead of stdout")
	pf.BoolVarP(&a.G.Quiet, "quiet", "q", false, "suppress progress and notes on stderr")
	pf.BoolVar(&a.G.NoColor, "no-color", false, "disable ANSI colour (also honours NO_COLOR)")
	pf.BoolVar(&a.G.Refresh, "refresh", false, "ignore cached responses, refetch and rewrite the cache")
	pf.BoolVar(&a.G.NoCache, "no-cache", false, "neither read nor write the cache")
	pf.BoolVar(&a.G.JSON, "json", false, "alias for --format json")
	_ = pf.MarkHidden("json")

	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return core.Usagef("Run `"+c.CommandPath()+" --help`.", "%v", err)
	})

	groups := map[string]*cobra.Command{}
	titles := map[string]string{
		"amigable": "Consulta Amigable: ejecución del gasto público",
		"datos":    "Datos Abiertos del MEF: catálogo, recursos, descargas",
		"invierte": "Invierte.pe / SSI: inversiones públicas por CUI",
	}
	for _, c := range a.Registry.Commands() {
		parent := root
		for i, seg := range c.Path[:len(c.Path)-1] {
			key := strings.Join(c.Path[:i+1], " ")
			g, ok := groups[key]
			if !ok {
				g = &cobra.Command{Use: seg, Short: titles[seg], Args: cobra.NoArgs, RunE: groupHelp}
				groups[key] = g
				parent.AddCommand(g)
			}
			parent = g
		}
		parent.AddCommand(a.leaf(c))
	}
	for _, b := range a.builtins() {
		root.AddCommand(b)
	}
	return root
}

func groupHelp(c *cobra.Command, args []string) error { return c.Help() }

// Main runs the CLI and returns the process exit code.
func (a *App) Main(args []string) int {
	configureConsole()
	root := a.Root()
	root.SetArgs(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if err != nil {
		// An unknown command or flag fails before cobra parses --format, so
		// recover it from the raw arguments: a pipeline that asked for JSON
		// must get the error envelope even for a typo.
		if f, ok := formatFromArgs(args); ok {
			a.G.Format = f
		}
		e := core.AsError(err)
		if e.Code == core.CodeInternal && isCobraUsage(err) {
			e = core.Usagef("Run `mef --help`.", "%v", err)
		}
		a.fail(commandName(root, args), e)
		return e.ExitCode()
	}
	return a.exit
}

func formatFromArgs(args []string) (string, bool) {
	for i, arg := range args {
		switch {
		case arg == "--":
			return "", false
		case arg == "--json":
			return "json", true
		case (arg == "-f" || arg == "--format") && i+1 < len(args):
			return args[i+1], true
		case strings.HasPrefix(arg, "--format="):
			return strings.TrimPrefix(arg, "--format="), true
		case strings.HasPrefix(arg, "-f") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			return strings.TrimPrefix(arg[2:], "="), true
		}
	}
	return "", false
}

func isCobraUsage(err error) bool {
	msg := err.Error()
	for _, s := range []string{"unknown command", "unknown flag", "unknown shorthand", "accepts", "requires", "required flag", "invalid argument", "flag needs"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func commandName(root *cobra.Command, args []string) string {
	if c, _, err := root.Find(args); err == nil && c != root {
		return strings.TrimPrefix(c.CommandPath(), "mef ")
	}
	return "mef"
}

// fail prints the one-line stderr message and, in machine formats, the error
// envelope on stdout so a pipeline can parse the failure.
func (a *App) fail(command string, e *core.Error) {
	format := a.format()
	if format == "json" || format == "jsonl" {
		_ = render.WriteError(a.Stdout, render.ErrorEnvelope{
			OK: false, Command: command, Source: "mef", FetchedAt: render.Now(a.Now()), Error: e,
		})
	}
	fmt.Fprintf(a.Stderr, "error: %s\n", e.Message)
	if e.Hint != "" && !a.G.Quiet {
		fmt.Fprintf(a.Stderr, "hint: %s\n", e.Hint)
	}
}

func (a *App) format() string {
	if a.G.JSON {
		return "json"
	}
	return a.G.Format
}

func (a *App) cacheMode() httpx.CacheMode {
	switch {
	case a.G.NoCache:
		return httpx.CacheOff
	case a.G.Refresh:
		return httpx.CacheRefresh
	}
	return httpx.CacheNormal
}

func (a *App) progress() func(string, ...any) {
	if a.G.Quiet {
		return nil
	}
	return func(format string, args ...any) {
		fmt.Fprintf(a.Stderr, "· "+format+"\n", args...)
	}
}

func (a *App) colorOK(out io.Writer) bool {
	if a.G.NoColor || os.Getenv("NO_COLOR") != "" || a.G.Output != "" {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

var errHandled = errors.New("handled")
