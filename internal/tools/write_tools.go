package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nemesis312/obsidian-mcp/internal/vault"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterWriteTools registers all write/modification tools
func RegisterWriteTools(s *server.MCPServer, v *vault.Vault) {
	registerCreateNote(s, v)
	registerAppendToNote(s, v)
	registerPatchNote(s, v)
	registerUpdateFrontmatter(s, v)
	registerDeleteNote(s, v)
}

// create_note - Create a new note
func registerCreateNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "create_note",
		Description: "Create a new note with optional frontmatter",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path for the new note (e.g., 'folder/note.md')",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Note content",
				},
				"frontmatter": map[string]interface{}{
					"type":        "object",
					"description": "Optional frontmatter fields",
				},
			},
			Required: []string{"path", "content"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid content: %v", err)), nil
		}

		var fm map[string]interface{}
		if m, ok := request.GetArguments()["frontmatter"].(map[string]interface{}); ok {
			fm = m
		}

		// Single, exclusive write: fails if the note already exists.
		snap, err := v.CreateNote(path, content, fm)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create note: %v", err)), nil
		}
		path = snap.Path // report the path actually written
		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// append_to_note - Append content to an existing note
func registerAppendToNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "append_to_note",
		Description: "Append content to the end of an existing note",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to append",
				},
			},
			Required: []string{"path", "content"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid content: %v", err)), nil
		}

		snap, err := v.AppendToNote(path, content, "") // expected_version: Task 4
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to append: %v", err)), nil
		}
		path = snap.Path // report the path actually written
		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// patch_note - Replace content in a note
func registerPatchNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "patch_note",
		Description: "Update a note by completely replacing its content",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "New content for the note",
				},
			},
			Required: []string{"path", "content"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid content: %v", err)), nil
		}

		snap, err := v.ReplaceNote(path, content, "") // expected_version: Task 4
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to patch note: %v", err)), nil
		}
		path = snap.Path // report the path actually written
		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// update_frontmatter - Update frontmatter fields
func registerUpdateFrontmatter(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "update_frontmatter",
		Description: "Update frontmatter fields in a note (preserves body)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note",
				},
				"updates": map[string]interface{}{
					"type":        "object",
					"description": "Frontmatter fields to update (null to delete)",
				},
			},
			Required: []string{"path", "updates"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		args := request.GetArguments()
		updates, ok := args["updates"].(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("updates must be an object"), nil
		}

		snap, err := v.UpdateFrontmatter(path, updates, "") // expected_version: Task 4
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update frontmatter: %v", err)), nil
		}
		path = snap.Path // report the path actually written
		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}

// delete_note - Delete a note
func registerDeleteNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "delete_note",
		Description: "Delete a note from the vault",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the note to delete",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		path, err = v.DeleteNote(path, "") // expected_version: Task 4
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete note: %v", err)), nil
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}
