// Package core defines the contract every MEF module implements.
//
// A Command is declared once — its parameters, its row shape, its prose — and
// every surface is derived from that declaration: CLI flags, the MCP tool and
// its JSON Schema, and the `describe --json` manifest. Nothing is written
// twice, so the surfaces cannot drift apart.
package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParamType is the value type of a parameter, shared by CLI and JSON Schema.
type ParamType string

const (
	TypeString ParamType = "string"
	TypeInt    ParamType = "integer"
	TypeBool   ParamType = "boolean"
	// TypeStringList is repeatable on the CLI and an array in JSON.
	TypeStringList ParamType = "array"
)

// Param declares one input of a command.
type Param struct {
	Name       string    // snake_case key in JSON; kebab-case flag on the CLI
	Type       ParamType // value type
	Help       string    // one sentence, with a real example value
	Default    any       // zero means "no default"
	Required   bool      // rejected when absent
	Positional bool      // CLI takes it as a bare argument, in declaration order
	Enum       []string  // allowed values, when closed
	Short      string    // one-letter CLI alias
	Min, Max   *int      // integer bounds
	CLIOnly    bool      // not exposed to MCP (e.g. output file paths)
}

// Flag is the CLI spelling of the parameter.
func (p Param) Flag() string { return strings.ReplaceAll(p.Name, "_", "-") }

// Row is one tabular record. Column order comes from Result.Columns.
type Row = map[string]any

// Sink receives rows as they are produced. Streaming formats write them
// immediately, so `--all` over a large source never buffers the whole harvest.
type Sink interface {
	// SetColumns announces the column order before the first Emit. Commands
	// with source-defined columns (datastore, SQL) must call it, or streamed
	// csv would have to guess the header from the first row's map order.
	SetColumns(cols []string)
	Emit(rows ...Row) error
	// Wanted reports how many more rows the caller will accept (-1 = no limit).
	// Paginating commands stop fetching once it reaches zero.
	Wanted() int
}

// Result is what a command returns besides the rows it emitted.
type Result struct {
	Columns []string       // column order for table/csv/md; nil = derive from rows
	Data    any            // full nested payload for the JSON envelope
	Meta    map[string]any // pagination, provenance, upstream freshness
	Notes   []string       // human notes, written to stderr (never stdout)
}

// Env is what a command may use at run time. Modules never reach for globals;
// that keeps them testable against a fake transport.
type Env struct {
	HTTP  HTTPDoer
	Store StoreOpener
	Now   func() time.Time
	// Interactive is true for the CLI, false inside the MCP server. Commands
	// use it only to tune defaults (e.g. row caps), never to change semantics.
	Interactive bool
	Progress    func(format string, args ...any)
}

// Command is the single declaration from which all surfaces are derived.
type Command struct {
	Path        []string // CLI path, e.g. {"amigable", "gasto"}
	Tool        string   // MCP tool name; empty = CLI only
	Title       string   // short human title for MCP clients
	Summary     string   // one line for help and the manifest
	Description string   // long form: the MCP tool description (DOCS-STANDARD §6)
	Params      []Param
	RowFields   []string // documented columns; may be a prefix for dynamic sources
	Network     bool     // calls an upstream MEF service
	SupportsAll bool     // accepts --all / --max
	ReadOnly    bool     // no local side effects (MCP readOnlyHint)
	Examples    []string // full CLI invocations, each must parse
	Run         func(ctx context.Context, env *Env, in Input, sink Sink) (*Result, error)
}

// Name is the space-joined CLI path used as the manifest command name.
func (c *Command) Name() string { return strings.Join(c.Path, " ") }

// Module groups the commands of one MEF source.
type Module interface {
	ID() string      // short id, e.g. "amigable"
	Title() string   // human name of the source
	BaseURL() string // upstream root, for the manifest and doctor
	Commands() []*Command
}

// Input is a validated bag of parameter values.
type Input struct {
	values map[string]any
	set    map[string]bool
}

// NewInput validates raw values against the parameter declarations: types are
// coerced (CLI strings, JSON numbers), defaults applied, enums and bounds
// enforced. The same path serves both surfaces, so they validate identically.
func NewInput(params []Param, raw map[string]any, explicit map[string]bool) (Input, error) {
	in := Input{values: map[string]any{}, set: map[string]bool{}}
	known := map[string]bool{}
	for _, p := range params {
		known[p.Name] = true
		v, present := raw[p.Name]
		if present && explicit != nil && !explicit[p.Name] {
			present = false
		}
		if !present || v == nil {
			if p.Required {
				return in, Usagef("", "missing required parameter %q", p.Name)
			}
			if p.Default != nil {
				in.values[p.Name] = p.Default
			}
			continue
		}
		cv, err := coerce(p, v)
		if err != nil {
			return in, err
		}
		in.values[p.Name] = cv
		in.set[p.Name] = true
	}
	for k := range raw {
		if !known[k] {
			return in, Usagef("", "unknown parameter %q", k)
		}
	}
	return in, nil
}

func coerce(p Param, v any) (any, error) {
	bad := func() error {
		return Usagef(p.Help, "parameter %q expects %s, got %v", p.Name, p.Type, v)
	}
	switch p.Type {
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return nil, bad()
		}
		s = strings.TrimSpace(s)
		if p.Required && s == "" {
			return nil, Usagef(p.Help, "parameter %q must not be empty", p.Name)
		}
		if len(p.Enum) > 0 && !contains(p.Enum, strings.ToLower(s)) {
			return nil, Usagef("Valid values: "+strings.Join(p.Enum, ", "), "invalid %s %q", p.Name, s)
		}
		if len(p.Enum) > 0 {
			s = strings.ToLower(s)
		}
		return s, nil
	case TypeInt:
		var n int
		switch t := v.(type) {
		case int:
			n = t
		case int64:
			n = int(t)
		case float64:
			if t != float64(int(t)) {
				return nil, bad()
			}
			n = int(t)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return nil, bad()
			}
			n = parsed
		default:
			return nil, bad()
		}
		if p.Min != nil && n < *p.Min {
			return nil, Usagef(p.Help, "parameter %q must be >= %d", p.Name, *p.Min)
		}
		if p.Max != nil && n > *p.Max {
			return nil, Usagef(p.Help, "parameter %q must be <= %d", p.Name, *p.Max)
		}
		return n, nil
	case TypeBool:
		switch t := v.(type) {
		case bool:
			return t, nil
		case string:
			b, err := strconv.ParseBool(t)
			if err != nil {
				return nil, bad()
			}
			return b, nil
		}
		return nil, bad()
	case TypeStringList:
		switch t := v.(type) {
		case []string:
			return t, nil
		case string:
			return []string{t}, nil
		case []any:
			out := make([]string, 0, len(t))
			for _, item := range t {
				s, ok := item.(string)
				if !ok {
					return nil, bad()
				}
				out = append(out, s)
			}
			return out, nil
		}
		return nil, bad()
	}
	return nil, fmt.Errorf("param %q: unsupported type %s", p.Name, p.Type)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsSet reports whether the caller supplied the value (vs. a default).
func (in Input) IsSet(name string) bool { return in.set[name] }

func (in Input) Str(name string) string {
	s, _ := in.values[name].(string)
	return s
}

func (in Input) Int(name string) int {
	n, _ := in.values[name].(int)
	return n
}

func (in Input) Bool(name string) bool {
	b, _ := in.values[name].(bool)
	return b
}

func (in Input) List(name string) []string {
	l, _ := in.values[name].([]string)
	return l
}

// Pairs parses repeated KEY=VALUE values, preserving order (order is
// meaningful: Consulta Amigable treats it as the drill-down path).
func (in Input) Pairs(name string) ([][2]string, error) {
	var out [][2]string
	for _, item := range in.List(name) {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			k, v, ok := strings.Cut(part, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return nil, Usagef("Use KEY=VALUE, e.g. sector=01", "invalid %s entry %q", name, part)
			}
			out = append(out, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	return out, nil
}

// IntPtr is a helper for Param.Min/Max literals.
func IntPtr(n int) *int { return &n }
