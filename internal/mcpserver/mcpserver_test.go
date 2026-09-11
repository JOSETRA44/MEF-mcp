package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/mcpserver"
	"github.com/JOSETRA44/mef-mcp/internal/modules"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

// connect runs the real server against a real client over in-memory pipes:
// initialize, then whatever the test asks — the same path a host uses.
func connect(t *testing.T, fake *testutil.Fake) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv, _ := mcpserver.New(modules.Registry(), testutil.Shared(t, fake))
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("%s: text content is not the JSON envelope: %q", name, text)
	}
	return env, res.IsError
}

func TestToolsListMatchesRegistry(t *testing.T) {
	cs := connect(t, &testutil.Fake{})
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := modules.Registry().Tools()
	if len(list.Tools) != len(want) {
		t.Fatalf("got %d tools, registry has %d", len(list.Tools), len(want))
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		byName[tool.Name] = tool
	}
	for _, c := range want {
		tool, ok := byName[c.Tool]
		if !ok {
			t.Errorf("tool %s not served", c.Tool)
			continue
		}
		// DOCS-STANDARD §6: the weakest description drags the server score,
		// so every one must say when to use it and how it behaves.
		if len(tool.Description) < 300 || !strings.Contains(tool.Description, "Use ") {
			t.Errorf("%s: description too thin (%d chars)", tool.Name, len(tool.Description))
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(schema), `"type":"object"`) {
			t.Errorf("%s: schema %s", tool.Name, schema)
		}
		for _, p := range c.MCPParams() {
			if p.Help == "" {
				t.Errorf("%s.%s has no description", c.Tool, p.Name)
			}
		}
	}
}

func TestOfflineToolReturnsEnvelope(t *testing.T) {
	cs := connect(t, &testutil.Fake{})
	env, isErr := callTool(t, cs, "mef_gasto_dimensiones", map[string]any{"question": "funcional"})
	if isErr || env["ok"] != true || env["count"].(float64) < 2 {
		t.Fatalf("envelope %v", env)
	}
}

func TestToolErrorsAreResultsWithHints(t *testing.T) {
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "traeDetInvSSI", Reply: testutil.Static([]byte("[]"))}}}
	cs := connect(t, fake)
	cases := []struct {
		tool string
		args map[string]any
		code string
	}{
		{"mef_inversion_ficha", map[string]any{"cui": "1234567"}, "not_found"},
		{"mef_gasto", map[string]any{"by": "nope"}, "usage"},
		{"mef_gasto", map[string]any{"bogus": 1}, "usage"},
		{"mef_sql", map[string]any{"query": "SELECT 1"}, "not_found"},
		{"mef_sql", map[string]any{"query": "DELETE FROM x"}, "usage"},
	}
	for _, c := range cases {
		env, isErr := callTool(t, cs, c.tool, c.args)
		errObj, _ := env["error"].(map[string]any)
		if !isErr || env["ok"] != false || errObj["code"] != c.code || errObj["hint"] == "" {
			t.Errorf("%s %v: isError=%v env=%v", c.tool, c.args, isErr, env)
		}
	}
}

func TestDefaultRowCapProtectsContext(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, `<tr id="tr%d"><td><input type="radio" name="grp1" value="%04d/10/20/5/4/3/2/6"></td><td align="left">%04d: X</td></tr>`, i, i, i)
	}
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "Navegar_7.aspx", Reply: testutil.Static([]byte(sb.String()))}}}
	cs := connect(t, fake)
	env, isErr := callTool(t, cs, "mef_gasto", map[string]any{"by": "ejecutora", "year": 2025})
	meta := env["meta"].(map[string]any)
	if isErr || env["count"].(float64) != float64(core.MCPDefaultMax) || meta["truncated"] != true || meta["hint"] == nil {
		t.Fatalf("count=%v meta=%v", env["count"], meta)
	}
	env, _ = callTool(t, cs, "mef_gasto", map[string]any{"by": "ejecutora", "year": 2025, "max": 250})
	if env["count"].(float64) != 250 {
		t.Fatalf("explicit max ignored: %v", env["count"])
	}
}

func TestStructuredContentForRealFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "internal/modules/invierte/testdata/ficha_2222116.json"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "traeDetInvSSI", Reply: testutil.Static(b)}}}
	cs := connect(t, fake)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "mef_inversion_ficha", Arguments: map[string]any{"cui": "2222116"}})
	if err != nil || res.IsError || res.StructuredContent == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	sc, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(sc), `"cui":"2222116"`) || !strings.Contains(string(sc), `"module":"invierte"`) {
		t.Fatalf("structured content %s", sc)
	}
}

func TestResourcesAndPrompts(t *testing.T) {
	cs := connect(t, &testutil.Fake{})
	ctx := context.Background()
	rl, err := cs.ListResources(ctx, nil)
	if err != nil || len(rl.Resources) != 3 {
		t.Fatalf("resources: %v %v", rl, err)
	}
	for _, uri := range []string{"mef://guia", "mef://dimensiones", "mef://catalogo"} {
		rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil || len(rr.Contents) == 0 || rr.Contents[0].Text == "" {
			t.Errorf("%s: %v %v", uri, rr, err)
		}
	}
	guide, _ := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "mef://guia"})
	if !strings.Contains(guide.Contents[0].Text, "Devengado") {
		t.Error("glossary lacks devengado")
	}
	pl, err := cs.ListPrompts(ctx, nil)
	if err != nil || len(pl.Prompts) != 3 {
		t.Fatalf("prompts: %v %v", pl, err)
	}
	gp, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "ficha_inversion", Arguments: map[string]string{"proyecto": "2222116"}})
	if err != nil || !strings.Contains(gp.Messages[0].Content.(*mcp.TextContent).Text, "2222116") {
		t.Fatalf("prompt: %v %v", gp, err)
	}
}
