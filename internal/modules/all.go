// Package modules is the one place sources are registered.
//
// Adding a MEF source is: create internal/modules/<name> implementing
// core.Module, then add one line below. The CLI, the MCP server, the describe
// manifest and the completeness tests pick it up with no other change.
// See docs/ADDING-A-MODULE.md.
package modules

import (
	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/modules/amigable"
	"github.com/JOSETRA44/mef-mcp/internal/modules/datosabiertos"
	"github.com/JOSETRA44/mef-mcp/internal/modules/invierte"
	"github.com/JOSETRA44/mef-mcp/internal/modules/local"
)

// Registry builds the production registry.
func Registry() *core.Registry {
	da := datosabiertos.New()
	return core.NewRegistry([]core.Module{
		amigable.New(),
		da,
		invierte.New(),
		local.New(da),
	})
}
