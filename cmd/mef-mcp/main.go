// Command mef-mcp runs the MEF MCP server over stdio.
//
// stdout carries only JSON-RPC; any diagnostic goes to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/JOSETRA44/mef-mcp/internal/mcpserver"
	"github.com/JOSETRA44/mef-mcp/internal/modules"
	"github.com/JOSETRA44/mef-mcp/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("mef-mcp " + version.Get())
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := mcpserver.Serve(ctx, modules.Registry()); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "mef-mcp:", err)
		os.Exit(1)
	}
}
