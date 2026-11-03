// Package meta holds repository-level checks: the documentation and registry
// metadata must agree with the code (DOCS-STANDARD §8).
package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/modules"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

type serverJSON struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Packages []struct {
		RegistryType    string `json:"registryType"`
		RegistryBaseURL string `json:"registryBaseUrl"`
		Identifier      string `json:"identifier"`
		Version         string `json:"version"`
		Env             []struct {
			Name     string `json:"name"`
			IsSecret bool   `json:"isSecret"`
			Default  any    `json:"default"`
		} `json:"environmentVariables"`
	} `json:"packages"`
}

func server(t *testing.T) serverJSON {
	var s serverJSON
	if err := json.Unmarshal([]byte(read(t, "server.json")), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOwnershipMarkerMatchesServerJSON(t *testing.T) {
	s := server(t)
	m := regexp.MustCompile(`mcp-name:\s*(\S+)`).FindStringSubmatch(read(t, "README.md"))
	if m == nil || m[1] != s.Name {
		t.Fatalf("README marker %v != server.json name %q", m, s.Name)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		MCPName string `json:"mcpName"`
	}
	if err := json.Unmarshal([]byte(read(t, "npm/package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.MCPName != s.Name {
		t.Fatalf("npm mcpName %q != %q", pkg.MCPName, s.Name)
	}
	if pkg.Version != version.Version {
		t.Fatalf("npm version %s != %s", pkg.Version, version.Version)
	}
}

func TestVersionsAgree(t *testing.T) {
	s := server(t)
	if s.Version != version.Version {
		t.Fatalf("server.json %s != version.Version %s", s.Version, version.Version)
	}
	for _, p := range s.Packages {
		if p.Version != version.Version {
			t.Errorf("package %s version %s", p.Identifier, p.Version)
		}
	}
	if !strings.Contains(read(t, "CHANGELOG.md"), "## ["+version.Version+"]") {
		t.Error("CHANGELOG has no entry for the current version")
	}
}

func TestRegistryBaseURLsAreTrustedAndSecretsSafe(t *testing.T) {
	trusted := map[string]bool{"https://pypi.org": true, "https://registry.npmjs.org": true, "https://api.nuget.org/v3/index.json": true}
	for _, p := range server(t).Packages {
		if !trusted[p.RegistryBaseURL] {
			t.Errorf("untrusted registry %q", p.RegistryBaseURL)
		}
		for _, e := range p.Env {
			if e.IsSecret && e.Default != nil {
				t.Errorf("secret %s has a default", e.Name)
			}
		}
	}
}

func TestLicenseIsMIT(t *testing.T) {
	if l := read(t, "LICENSE"); !strings.Contains(l, "MIT License") || !strings.Contains(l, "JOSETRA44") {
		t.Fatal("LICENSE must be MIT, JOSETRA44")
	}
}

// A tool listed but absent is worse than no table: both directions must hold.
func TestREADMEToolTableMatchesRegistry(t *testing.T) {
	readme := read(t, "README.md")
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `(mef_[a-z_]+)` \\|").FindAllStringSubmatch(readme, -1) {
		listed[m[1]] = true
	}
	registered := map[string]bool{}
	for _, c := range modules.Registry().Tools() {
		registered[c.Tool] = true
		if !listed[c.Tool] {
			t.Errorf("tool %s missing from README table", c.Tool)
		}
	}
	for name := range listed {
		if !registered[name] {
			t.Errorf("README lists %s, which is not registered", name)
		}
	}
}
