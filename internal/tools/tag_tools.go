package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTagTools registers all tag-related tools
func RegisterTagTools(s *server.MCPServer, v *vault.Vault) {
	registerListAllTags(s, v)
	registerGetNotesByTag(s, v)
	registerRenameTag(s, v)
	registerListCanvases(s, v)
	registerGetCanvas(s, v)
}

// list_all_tags - List all unique tags in the vault
func registerListAllTags(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "list_all_tags",
		Description: "List all unique tags across the vault",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tags, err := v.ListAllTags()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list tags: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"tags":  tags,
			"count": len(tags),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_notes_by_tag - Get all notes with a specific tag
func registerGetNotesByTag(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_notes_by_tag",
		Description: "Get all notes containing a specific tag",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"tag": map[string]interface{}{
					"type":        "string",
					"description": "Tag name (without # prefix)",
				},
			},
			Required: []string{"tag"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tag, err := request.RequireString("tag")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid tag: %v", err)), nil
		}

		notes, err := v.GetNotesByTag(tag)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get notes by tag: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"tag":   tag,
			"notes": notes,
			"count": len(notes),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// rename_tag - Rename a tag across all notes
func registerRenameTag(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "rename_tag",
		Description: "Rename a tag across all notes in the vault",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"old_tag": map[string]interface{}{
					"type":        "string",
					"description": "Current tag name (without # prefix)",
				},
				"new_tag": map[string]interface{}{
					"type":        "string",
					"description": "New tag name (without # prefix)",
				},
			},
			Required: []string{"old_tag", "new_tag"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		oldTag, err := request.RequireString("old_tag")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid old_tag: %v", err)), nil
		}

		newTag, err := request.RequireString("new_tag")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid new_tag: %v", err)), nil
		}

		count, err := v.RenameTag(oldTag, newTag)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to rename tag: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"old_tag":       oldTag,
			"new_tag":       newTag,
			"notes_updated": count,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// list_canvases - List all canvas files
func registerListCanvases(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "list_canvases",
		Description: "List all canvas files in the vault",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		canvases, err := v.ListCanvases()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list canvases: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"canvases": canvases,
			"count":    len(canvases),
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// get_canvas - Get canvas content
func registerGetCanvas(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_canvas",
		Description: "Get the content of a canvas file",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the canvas file",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		canvas, err := v.ReadCanvas(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read canvas: %v", err)), nil
		}

		data, _ := json.MarshalIndent(canvas, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})
}
