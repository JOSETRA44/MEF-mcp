package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JOSETRA44/mef-mcp/internal/app"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/render"
)

// AnnotationCommand links a cobra leaf back to its core.Command name; the
// manifest and tests use it to prove the two trees match.
const AnnotationCommand = "mef/command"

func (a *App) leaf(c *core.Command) *cobra.Command {
	var positional []core.Param
	use := c.Path[len(c.Path)-1]
	for _, p := range c.Params {
		if p.Positional {
			positional = append(positional, p)
			if p.Required {
				use += " <" + p.Name + ">"
			} else {
				use += " [" + p.Name + "]"
			}
		}
	}
	required := 0
	for _, p := range positional {
		if p.Required {
			required++
		}
	}
	cc := &cobra.Command{
		Use:         use,
		Short:       c.Summary,
		Long:        longHelp(c),
		Example:     "  " + strings.Join(c.Examples, "\n  "),
		Args:        cobra.RangeArgs(required, len(positional)),
		Annotations: map[string]string{AnnotationCommand: c.Name()},
	}
	flags := cc.Flags()
	for _, p := range c.Params {
		if p.Positional {
			continue
		}
		help := p.Help
		if len(p.Enum) > 0 {
			help += " [" + strings.Join(p.Enum, "|") + "]"
		}
		switch p.Type {
		case core.TypeString:
			def, _ := p.Default.(string)
			flags.StringP(p.Flag(), p.Short, def, help)
		case core.TypeInt:
			def, _ := p.Default.(int)
			flags.IntP(p.Flag(), p.Short, def, help)
		case core.TypeBool:
			def, _ := p.Default.(bool)
			flags.BoolP(p.Flag(), p.Short, def, help)
		case core.TypeStringList:
			flags.StringArrayP(p.Flag(), p.Short, nil, help+" (repeatable)")
		}
		if p.Required {
			_ = cc.MarkFlagRequired(p.Flag())
		}
	}
	cc.RunE = func(cmd *cobra.Command, args []string) error {
		raw := map[string]any{}
		for i, p := range positional {
			if i < len(args) {
				raw[p.Name] = args[i]
			}
		}
		for _, p := range c.Params {
			if p.Positional || !cmd.Flags().Changed(p.Flag()) {
				continue
			}
			var v any
			var err error
			switch p.Type {
			case core.TypeString:
				v, err = cmd.Flags().GetString(p.Flag())
			case core.TypeInt:
				v, err = cmd.Flags().GetInt(p.Flag())
			case core.TypeBool:
				v, err = cmd.Flags().GetBool(p.Flag())
			case core.TypeStringList:
				v, err = cmd.Flags().GetStringArray(p.Flag())
			}
			if err != nil {
				return core.Usagef("", "%v", err)
			}
			raw[p.Name] = v
		}
		in, err := core.NewInput(c.Params, raw, nil)
		if err != nil {
			return err
		}
		return a.run(cmd, c, in)
	}
	return cc
}

func longHelp(c *core.Command) string {
	if c.Description != "" {
		return c.Description
	}
	return c.Summary
}

// run executes a command and renders it. Errors propagate to Main, which owns
// exit codes and the error envelope.
func (a *App) run(cmd *cobra.Command, c *core.Command, in core.Input) error {
	format := a.format()
	if !render.Valid(format) {
		return core.Usagef("Valid formats: "+strings.Join(render.Formats, ", "), "unknown format %q", format)
	}
	var out io.Writer = a.Stdout
	var file *os.File
	if a.G.Output != "" {
		f, err := os.Create(a.G.Output)
		if err != nil {
			return core.Wrap(core.CodeUsage, err, "Choose a writable path for --output.", "cannot write %s", a.G.Output)
		}
		file = f
		out = f
		defer f.Close()
	}

	shared := a.NewShared(a.cacheMode())
	env, tr := shared.Env(true, a.progress())
	env.Now = a.Now

	max := -1
	for _, p := range c.Params {
		if p.Name == "max" {
			max = in.Int("max")
		}
	}
	w := render.NewWriter(format, out, max)
	w.Columns = append([]string(nil), c.RowFields...)
	w.Color = a.colorOK(out)

	res, err := c.Run(cmd.Context(), env, in, w)
	if err != nil {
		if file != nil {
			file.Close()
			os.Remove(a.G.Output)
		}
		return err
	}
	if res != nil && !a.G.Quiet {
		for _, n := range res.Notes {
			fmt.Fprintf(a.Stderr, "note: %s\n", n)
		}
	}
	if w.Truncated() > 0 && !a.G.Quiet {
		fmt.Fprintf(a.Stderr, "note: output capped at --max %d (%d more rows discarded)\n", w.Max, w.Truncated())
	}
	envl := app.Envelope(c, res, tr, w, a.Now(), a.moduleOf(c))
	if err := w.Finish(envl, res); err != nil {
		return core.Wrap(core.CodeInternal, err, "", "writing output: %v", err)
	}
	if file != nil && !a.G.Quiet {
		fmt.Fprintf(a.Stderr, "wrote %d rows to %s\n", w.Emitted(), a.G.Output)
	}
	return nil
}
