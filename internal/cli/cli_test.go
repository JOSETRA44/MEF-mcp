package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/JOSETRA44/mef-mcp/internal/app"
	"github.com/JOSETRA44/mef-mcp/internal/cli"
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
	"github.com/JOSETRA44/mef-mcp/internal/modules"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type result struct {
	code           int
	stdout, stderr string
}

func newApp(t *testing.T, fake *testutil.Fake) (*cli.App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("MEF_HOME", t.TempDir())
	var out, errb bytes.Buffer
	a := cli.NewApp(modules.Registry())
	a.Stdout, a.Stderr = &out, &errb
	shared := testutil.Shared(t, fake)
	a.NewShared = func(httpx.CacheMode) *app.Shared { return shared }
	a.Now = func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }
	return a, &out, &errb
}

func run(t *testing.T, fake *testutil.Fake, args ...string) result {
	t.Helper()
	a, out, errb := newApp(t, fake)
	code := a.Main(args)
	return result{code, out.String(), errb.String()}
}

func ssiFake(t *testing.T, cached bool) *testutil.Fake {
	return &testutil.Fake{Cached: cached, Routes: []testutil.Route{
		{Match: "traeDetInvSSI", Body: "id=2222116", Reply: testutil.Static(fixture(t, "internal/modules/invierte/testdata/ficha_2222116.json"))},
		{Match: "traeDetInvSSI", Reply: testutil.Static([]byte("[]"))},
		{Match: "Navegar_7.aspx", Reply: testutil.Static(fixture(t, "internal/modules/amigable/testdata/nivel_2026.html"))},
	}}
}

func TestEnvelopeAndCachedFlag(t *testing.T) {
	for _, cached := range []bool{false, true} {
		r := run(t, ssiFake(t, cached), "invierte", "ficha", "2222116", "-f", "json")
		if r.code != 0 {
			t.Fatalf("exit %d: %s", r.code, r.stderr)
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &env); err != nil {
			t.Fatalf("stdout is not pure JSON: %v\n%s", err, r.stdout)
		}
		for _, k := range []string{"ok", "command", "source", "fetched_at", "cached", "count", "meta", "data", "rows"} {
			if _, ok := env[k]; !ok {
				t.Fatalf("envelope missing %s", k)
			}
		}
		if env["cached"] != cached || env["source"] != "mef" || env["command"] != "invierte ficha" {
			t.Fatalf("envelope %v", env)
		}
		if int(env["count"].(float64)) != len(env["rows"].([]any)) || env["meta"].(map[string]any)["module"] != "invierte" {
			t.Fatalf("count/module wrong: %v", env)
		}
	}
}

func TestOfflineCommandIsNeverCached(t *testing.T) {
	r := run(t, &testutil.Fake{Cached: true}, "amigable", "dimensiones", "-f", "json")
	var env map[string]any
	_ = json.Unmarshal([]byte(r.stdout), &env)
	if r.code != 0 || env["cached"] != false {
		t.Fatalf("exit %d cached=%v", r.code, env["cached"])
	}
}

// One case per reachable exit code, plus the error envelope on stdout.
func TestExitCodesAndErrorEnvelope(t *testing.T) {
	throttled := &testutil.Fake{Routes: []testutil.Route{{Match: "traeDetInvSSI", Reply: func(core.Request, int) ([]byte, error) {
		return nil, core.NewError(core.CodeRateLimit, "", "HTTP 429")
	}}}}
	offline := &testutil.Fake{} // no routes: every call is a network error
	cases := []struct {
		name string
		fake *testutil.Fake
		args []string
		code int
		err  string
	}{
		{"ok", ssiFake(t, false), []string{"invierte", "ficha", "2222116", "-f", "json"}, 0, ""},
		{"api", &testutil.Fake{Routes: []testutil.Route{{Match: "traeDetInvSSI", Reply: testutil.Static([]byte("<html>"))}}}, []string{"invierte", "ficha", "2222116", "-f", "json"}, 1, "api"},
		{"usage-dimension", ssiFake(t, false), []string{"amigable", "gasto", "--by", "nope", "-f", "json"}, 2, "usage"},
		{"usage-unknown-command", ssiFake(t, false), []string{"nope", "-f", "json"}, 2, "usage"},
		{"usage-unknown-flag", ssiFake(t, false), []string{"invierte", "ficha", "1", "--bogus", "-f", "json"}, 2, "usage"},
		{"usage-format", ssiFake(t, false), []string{"amigable", "dimensiones", "-f", "xml"}, 2, ""},
		{"not-found", ssiFake(t, false), []string{"invierte", "ficha", "1234567", "-f", "json"}, 3, "not_found"},
		{"rate-limit", throttled, []string{"invierte", "ficha", "2222116", "-f", "json"}, 5, "rate_limit"},
		{"network", offline, []string{"invierte", "ficha", "2222116", "-f", "jsonl"}, 6, "network"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, c.fake, c.args...)
			if r.code != c.code {
				t.Fatalf("exit %d want %d\nstdout=%s\nstderr=%s", r.code, c.code, r.stdout, r.stderr)
			}
			if c.err == "" {
				return
			}
			var env struct {
				OK     bool   `json:"ok"`
				Source string `json:"source"`
				Error  struct {
					Code, Message, Hint string
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(r.stdout), &env); err != nil {
				t.Fatalf("error envelope not on stdout: %q", r.stdout)
			}
			if env.OK || env.Source != "mef" || env.Error.Code != c.err || env.Error.Hint == "" {
				t.Fatalf("envelope %+v", env)
			}
			if !strings.HasPrefix(r.stderr, "error: ") {
				t.Fatalf("stderr must carry a one-line message: %q", r.stderr)
			}
		})
	}
}

func TestVersionAndHelpExitZero(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}, {"amigable", "gasto", "--help"}} {
		if r := run(t, &testutil.Fake{}, args...); r.code != 0 {
			t.Errorf("%v exited %d", args, r.code)
		}
	}
}

func TestTableOutputKeepsNotesOffStdout(t *testing.T) {
	r := run(t, ssiFake(t, false), "amigable", "gasto", "--by", "nivel", "--max", "1")
	if r.code != 0 || strings.Contains(r.stdout, "note:") || !strings.Contains(r.stderr, "note:") {
		t.Fatalf("stdout=%q stderr=%q", r.stdout, r.stderr)
	}
}

func TestOutputFileAndUTF8RoundTrip(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dims.csv")
	r := run(t, &testutil.Fake{}, "amigable", "dimensiones", "-f", "csv", "-o", dest)
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("exit %d stdout %q", r.code, r.stdout)
	}
	b, _ := os.ReadFile(dest)
	if !strings.Contains(string(b), "¿Quién gasta?") || !strings.Contains(string(b), "Categoría Presupuestal") {
		t.Fatalf("non-ASCII lost:\n%s", b)
	}
}

func TestLegacyJSONFlag(t *testing.T) {
	r := run(t, &testutil.Fake{}, "datos", "categorias", "--json")
	if r.code != 0 || !strings.HasPrefix(strings.TrimSpace(r.stdout), "{") {
		t.Fatalf("--json alias: %d %q", r.code, r.stdout)
	}
}

// The manifest is derived from the parser: every leaf the parser registers
// must appear in describe --json (ARSENAL-SPEC §6.5).
func TestDescribeListsEveryRegisteredCommand(t *testing.T) {
	a, out, _ := newApp(t, &testutil.Fake{})
	if code := a.Main([]string{"describe", "--json"}); code != 0 {
		t.Fatalf("describe exited %d", code)
	}
	var manifest struct {
		Name      string            `json:"name"`
		Formats   []string          `json:"formats"`
		ExitCodes map[string]string `json:"exit_codes"`
		Commands  []struct {
			Name     string   `json:"name"`
			Summary  string   `json:"summary"`
			Tool     string   `json:"mcp_tool"`
			Examples []string `json:"examples"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "mef" || len(manifest.Formats) != 5 || len(manifest.ExitCodes) != 7 {
		t.Fatalf("manifest header: %+v", manifest)
	}
	listed := map[string]bool{}
	for _, c := range manifest.Commands {
		if c.Summary == "" {
			t.Errorf("%s has no summary", c.Name)
		}
		if len(c.Examples) == 0 {
			t.Errorf("%s has no example", c.Name)
		}
		listed[c.Name] = true
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if !sub.HasSubCommands() {
				name := strings.TrimPrefix(sub.CommandPath(), "mef ")
				if !listed[name] {
					t.Errorf("parser registers %q but describe omits it", name)
				}
			}
			walk(sub)
		}
	}
	root, _, _ := newApp(t, &testutil.Fake{})
	walk(root.Root())
	for _, c := range modules.Registry().Tools() {
		if !listed[c.Name()] {
			t.Errorf("tool %s missing from manifest", c.Tool)
		}
	}
}

// Every documented example must parse against the real parser.
func TestEveryExampleParses(t *testing.T) {
	a, _, _ := newApp(t, &testutil.Fake{})
	root := a.Root()
	var examples []string
	for _, c := range modules.Registry().Commands() {
		examples = append(examples, c.Examples...)
	}
	for _, ex := range examples {
		args := splitArgs(ex)
		if len(args) == 0 || args[0] != "mef" {
			t.Errorf("example must start with mef: %q", ex)
			continue
		}
		fresh, _, _ := newApp(t, &testutil.Fake{})
		r := fresh.Root()
		cmd, rest, err := r.Find(args[1:])
		if err != nil || cmd == r {
			t.Errorf("%q: command not found (%v)", ex, err)
			continue
		}
		if err := cmd.ParseFlags(rest); err != nil {
			t.Errorf("%q: flags: %v", ex, err)
			continue
		}
		if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
			t.Errorf("%q: args: %v", ex, err)
		}
		if err := cmd.ValidateRequiredFlags(); err != nil {
			t.Errorf("%q: %v", ex, err)
		}
	}
	_ = root
}

// splitArgs is a minimal shell splitter supporting double and single quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out
}
