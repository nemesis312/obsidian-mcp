package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterGraphTools registers all graph/link navigation tools
func RegisterGraphTools(s *server.MCPServer, v *vault.Vault) {
	registerGetBacklinks(s, v)
	registerGetOutgoingLinks(s, v)
	registerGetOrphanedNotes(s, v)
	registerGetLinkedMentions(s, v)
	registerGetLinkGraph(s, v)
}

// get_backlinks - Get all notes linking to a note
func registerGetBacklinks(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_backlinks",
		Description: "Get all notes that link to the specified note",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		backlinks, err := v.GetBacklinks(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get backlinks: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"path":      path,
			"backlinks": backlinks,
			"count":     len(backlinks),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_outgoing_links - Get all links from a note
func registerGetOutgoingLinks(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_outgoing_links",
		Description: "Get all wikilinks from the specified note",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		links, err := v.GetOutgoingLinks(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get outgoing links: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"path":  path,
			"links": links,
			"count": len(links),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_orphaned_notes - Get notes with no links
func registerGetOrphanedNotes(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_orphaned_notes",
		Description: "Get all notes with no incoming or outgoing links",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orphans, err := v.GetOrphanedNotes()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get orphaned notes: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"orphans": orphans,
			"count":   len(orphans),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_linked_mentions - Get notes mentioning a note's name
func registerGetLinkedMentions(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_linked_mentions",
		Description: "Get notes that mention the target note's name in text (not as wikilink)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		mentions, err := v.GetLinkedMentions(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get mentions: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"path":     path,
			"mentions": mentions,
			"count":    len(mentions),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_link_graph - Build the full link graph
func registerGetLinkGraph(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_link_graph",
		Description: "Get the full link graph of the vault (nodes and edges)",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graph, err := v.BuildLinkGraph()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to build graph: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"nodes": graph.Nodes,
			"edges": graph.Edges,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}
