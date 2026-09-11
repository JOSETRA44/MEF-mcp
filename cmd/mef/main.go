// Command mef is the CLI for Peru's Ministerio de Economía y Finanzas data.
package main

import (
	"context"
	"os"

	"github.com/JOSETRA44/mef-mcp/internal/cli"
	"github.com/JOSETRA44/mef-mcp/internal/mcpserver"
	"github.com/JOSETRA44/mef-mcp/internal/modules"
)

func main() {
	reg := modules.Registry()
	a := cli.NewApp(reg)
	a.ServeMCP = func(ctx context.Context) error { return mcpserver.Serve(ctx, reg) }
	os.Exit(a.Main(os.Args[1:]))
}
