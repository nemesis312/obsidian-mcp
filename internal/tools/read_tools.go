package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nemesis312/obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// notePathDesc describes the "path" argument of every tool that takes an
// existing note.
const notePathDesc = `Note path (with or without .md) or bare note name, e.g. "projects/x/Note.md" or "Note". A leading "/" means the vault root.`

// RegisterReadTools registers all read-only tools
func RegisterReadTools(s *server.MCPServer, v *vault.Vault) {
	registerListVaultFiles(s, v)
	registerGetNote(s, v)
	registerSearchVault(s, v)
	registerGetFrontmatter(s, v)
	registerGetNoteMetadata(s, v)
}

// list_vault_files - List all markdown files in the vault
func registerListVaultFiles(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "list_vault_files",
		Description: "List all markdown files in the Obsidian vault",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		files, err := v.ListFiles()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list files: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"files": files,
			"count": len(files),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_note - Read a note's content
func registerGetNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_note",
		Description: "Read a note by path or name. Returns its canonical path, its content and its version (pass it as expected_version when replacing or deleting the note).",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		snap, err := v.ReadNoteVersioned(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read note: %v", err)), nil
		}

		return jsonResult(struct {
			Path    string `json:"path"`
			Version string `json:"version"`
			Content string `json:"content"`
		}{snap.Path, snap.Version, snap.Content}), nil
	})
}

// search_vault - Search for notes containing a query
func registerSearchVault(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "search_vault",
		Description: "Search for notes containing a query string (case-insensitive)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := request.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid query: %v", err)), nil
		}

		matches, err := v.SearchVault(query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"matches": matches,
			"count":   len(matches),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_frontmatter - Get frontmatter from a note
func registerGetFrontmatter(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_frontmatter",
		Description: "Extract YAML frontmatter from a note",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		fm, err := v.GetFrontmatter(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get frontmatter: %v", err)), nil
		}

		data, _ := json.MarshalIndent(fm, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_note_metadata - Get comprehensive metadata about a note
func registerGetNoteMetadata(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_note_metadata",
		Description: "Get metadata about a note (frontmatter, tags, links, backlinks)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		// Address once; everything below uses the canonical path.
		snap, err := v.ReadNoteVersioned(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get metadata: %v", err)), nil
		}
		path = snap.Path

		fm, err := v.GetFrontmatter(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get metadata: %v", err)), nil
		}

		// Outgoing link targets and backlinking notes, as paths (ghost
		// targets as their lookup key). Errors leave the lists empty.
		outgoing := []string{}
		if edges, err := v.GetOutgoingLinks(path); err == nil {
			for _, e := range edges {
				outgoing = append(outgoing, e.To)
			}
		}
		backlinks := []string{}
		if edges, err := v.GetBacklinks(path); err == nil {
			for _, e := range edges {
				backlinks = append(backlinks, e.From)
			}
		}

		metadata := map[string]interface{}{
			"path":        path,
			"version":     snap.Version,
			"frontmatter": fm,
			"outgoing":    outgoing,
			"backlinks":   backlinks,
		}

		data, _ := json.MarshalIndent(metadata, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})
}
