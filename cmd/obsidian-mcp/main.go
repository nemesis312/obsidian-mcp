package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nemesis312/obsidian-mcp/internal/tools"
	"github.com/nemesis312/obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/server"
)

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	vaultFlag := flag.String("vault", "", "Path to Obsidian vault")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	// stdout is reserved for JSON-RPC; this is the only path allowed to print there.
	if *showVersion {
		fmt.Printf("obsidian-mcp %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	// 1. Detect vault
	vaultPath, err := vault.DetectVault(*vaultFlag)
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
		version,
		server.WithToolCapabilities(false),
	)

	// 4. Register all 20 tools
	tools.RegisterReadTools(s, v)
	tools.RegisterWriteTools(s, v)
	tools.RegisterGraphTools(s, v)
	tools.RegisterTagTools(s, v)

	// 5. Serve via stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "obsidian-mcp: server error: %v\n", err)
		os.Exit(1)
	}
}
