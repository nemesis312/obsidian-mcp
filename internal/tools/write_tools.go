package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"obsidian-mcp/internal/vault"

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

		// Check if note already exists
		if v.NoteExists(path) {
			return mcp.NewToolResultError("note already exists"), nil
		}

		// Add frontmatter if provided
		args := request.GetArguments()
		if fm, ok := args["frontmatter"].(map[string]interface{}); ok && len(fm) > 0 {
			// Write to add frontmatter, then update
			if err := v.WriteNote(path, content); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to create note: %v", err)), nil
			}
			if err := v.UpdateFrontmatter(path, fm); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to add frontmatter: %v", err)), nil
			}
		} else {
			if err := v.WriteNote(path, content); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to create note: %v", err)), nil
			}
		}

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

		if err := v.AppendToNote(path, content); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to append: %v", err)), nil
		}

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

		if err := v.WriteNote(path, content); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to patch note: %v", err)), nil
		}

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

		if err := v.UpdateFrontmatter(path, updates); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update frontmatter: %v", err)), nil
		}

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

		if err := v.DeleteNote(path); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete note: %v", err)), nil
		}

		data, _ := json.MarshalIndent(map[string]interface{}{
			"success": true,
			"path":    path,
		}, "", "  ")

		return mcp.NewToolResultText(string(data)), nil
	})
}
