package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
)

func params() []Param {
	return []Param{
		{Name: "text", Type: TypeString, Positional: true, Required: true},
		{Name: "year", Type: TypeInt, Default: 2026, Min: IntPtr(1999), Max: IntPtr(2100)},
		{Name: "type", Type: TypeString, Default: "todo", Enum: []string{"todo", "proyecto"}},
		{Name: "all", Type: TypeBool, Default: false},
		{Name: "where", Type: TypeStringList},
	}
}

func TestNewInputCoercesBothSurfaces(t *testing.T) {
	// CLI delivers strings, MCP delivers JSON numbers and arrays of any.
	cli, err := NewInput(params(), map[string]any{"text": "x", "year": "2024", "all": "true", "where": []string{"a=1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := NewInput(params(), map[string]any{"text": "x", "year": float64(2024), "all": true, "where": []any{"a=1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []Input{cli, mcp} {
		if in.Int("year") != 2024 || !in.Bool("all") || !reflect.DeepEqual(in.List("where"), []string{"a=1"}) {
			t.Fatalf("coercion mismatch: %+v", in.values)
		}
	}
}

func TestNewInputDefaultsAndValidation(t *testing.T) {
	in, err := NewInput(params(), map[string]any{"text": "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.Int("year") != 2026 || in.Str("type") != "todo" || in.IsSet("year") {
		t.Fatalf("defaults not applied: %+v", in.values)
	}
	bad := []map[string]any{
		{},                                // missing required
		{"text": "x", "year": 1990},       // below min
		{"text": "x", "type": "otro"},     // enum
		{"text": "x", "year": 20.5},       // non-integer
		{"text": "x", "nope": 1},          // unknown parameter
		{"text": "x", "all": "sometimes"}, // bad bool
	}
	for _, raw := range bad {
		_, err := NewInput(params(), raw, nil)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeUsage || e.ExitCode() != 2 {
			t.Errorf("%v: want usage error, got %v", raw, err)
		}
	}
}

func TestEnumIsCaseInsensitive(t *testing.T) {
	in, err := NewInput(params(), map[string]any{"text": "x", "type": "PROYECTO"}, nil)
	if err != nil || in.Str("type") != "proyecto" {
		t.Fatalf("got %q, %v", in.Str("type"), err)
	}
}

func TestPairsPreservesOrderAndSplitsCommas(t *testing.T) {
	in, _ := NewInput(params(), map[string]any{"text": "x", "where": []string{"nivel=E,sector=01", "pliego=011"}}, nil)
	got, err := in.Pairs("where")
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"nivel", "E"}, {"sector", "01"}, {"pliego", "011"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	in, _ = NewInput(params(), map[string]any{"text": "x", "where": []string{"broken"}}, nil)
	if _, err := in.Pairs("where"); err == nil {
		t.Fatal("expected usage error for missing '='")
	}
}

// One case per exit code (ARSENAL-SPEC §6.3).
func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
		cls  ErrorCode
	}{
		{NewError(CodeAPI, "", "x"), 1, CodeAPI},
		{errors.New("boom"), 1, CodeInternal},
		{Usagef("", "x"), 2, CodeUsage},
		{NotFoundf("", "x"), 3, CodeNotFound},
		{NewError(CodeAuth, "", "x"), 4, CodeAuth},
		{NewError(CodeRateLimit, "", "x"), 5, CodeRateLimit},
		{context.DeadlineExceeded, 6, CodeNetwork},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, 6, CodeNetwork},
		{fmt.Errorf("wrapped: %w", NotFoundf("", "inner")), 3, CodeNotFound},
	}
	for _, c := range cases {
		e := AsError(c.err)
		if e.ExitCode() != c.code || e.Code != c.cls {
			t.Errorf("%v: got %s/%d, want %s/%d", c.err, e.Code, e.ExitCode(), c.cls, c.code)
		}
		if e.Hint == "" {
			t.Errorf("%v: every error must carry a hint", c.err)
		}
	}
	for code := range exitByCode {
		if _, ok := ExitCodes[fmt.Sprint(exitByCode[code])]; !ok {
			t.Errorf("exit code for %s undocumented", code)
		}
	}
}

func TestInputSchemaOmitsCLIOnly(t *testing.T) {
	c := &Command{Path: []string{"x"}, Params: append(params(), Param{Name: "dest", Type: TypeString, CLIOnly: true})}
	s := c.InputSchema()
	props := s["properties"].(map[string]any)
	if _, ok := props["dest"]; ok {
		t.Fatal("CLI-only param leaked into MCP schema")
	}
	if s["additionalProperties"] != false || !reflect.DeepEqual(s["required"], []string{"text"}) {
		t.Fatalf("schema: %+v", s)
	}
	if props["where"].(map[string]any)["items"] == nil {
		t.Fatal("array param needs items")
	}
}

type mod struct {
	id   string
	cmds []*Command
}

func (m mod) ID() string           { return m.id }
func (m mod) Title() string        { return m.id }
func (m mod) BaseURL() string      { return "" }
func (m mod) Commands() []*Command { return m.cmds }

func TestRegistryRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate tool")
		}
	}()
	NewRegistry([]Module{mod{"a", []*Command{{Path: []string{"a"}, Tool: "t"}, {Path: []string{"b"}, Tool: "t"}}}})
}

func TestRegistryModuleOfIsStable(t *testing.T) {
	c := &Command{Path: []string{"x"}, Tool: "mef_x"}
	r := NewRegistry([]Module{mod{"m1", []*Command{c}}})
	if r.ModuleOf(r.Commands()[0]) != "m1" || r.ModuleOf(&Command{}) != "mef" {
		t.Fatal("ModuleOf mismatch")
	}
}
