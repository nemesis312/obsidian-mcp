package main

import (
	"fmt"
	"os"

	"obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// 1. Detect vault
	vaultPath, err := vault.DetectVault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "obsidian-mcp: %v\n", err)
		fmt.Fprintf(os.Stderr, "Use --vault=/path/to/vault or set OBSIDIAN_VAULT_PATH\n")
		os.Exit(1)
	}

	// 2. Initialize vault
	v, err := vault.New(vaultPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "obsidian-mcp: failed to initialize vault: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "obsidian-mcp: using vault at %s\n", v.Root())

	// 3. Create MCP server
	s := server.NewMCPServer(
		"obsidian-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// 4. TODO: Register tools (Phase 2-5)
	// tools.RegisterReadTools(s, v)
	// tools.RegisterWriteTools(s, v)
	// tools.RegisterGraphTools(s, v)
	// tools.RegisterTagTools(s, v)
	// tools.RegisterCanvasTools(s, v)

	// 5. Serve via stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "obsidian-mcp: server error: %v\n", err)
		os.Exit(1)
	}
}
