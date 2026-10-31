package tools

import (
	"context"
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

// expectedVersion reads the optional expected_version argument.
func expectedVersion(request mcp.CallToolRequest) (string, error) {
	raw, ok := request.GetArguments()["expected_version"]
	if !ok || raw == nil {
		return "", nil
	}
	str, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("expected_version must be a string")
	}
	return str, nil
}

// writeResult is the result of a write that leaves a note behind.
func writeResult(snap vault.NoteSnapshot) *mcp.CallToolResult {
	return jsonResult(struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
		Version string `json:"version"`
	}{true, snap.Path, snap.Version})
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
					"description": "Path for the new note (with or without .md), e.g. 'folder/note.md'. A leading \"/\" means the vault root.",
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
		return writeResult(snap), nil
	})
}

// append_to_note - Append content to an existing note
func registerAppendToNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "append_to_note",
		Description: "Append content to the end of an existing note. expected_version is optional.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to append",
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Version from get_note. Optional: if given and the note changed since, the write is refused with the current version.",
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

		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		snap, err := v.AppendToNote(path, content, expected)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to append: %v", err)), nil
		}
		return writeResult(snap), nil
	})
}

// patch_note - Replace content in a note
func registerPatchNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "patch_note",
		Description: "Replace a note's whole content. Requires expected_version (the \"version\" from get_note); refused with the current version if the note changed.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "New content for the note",
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Required. The \"version\" from get_note; the call is refused with the current version if the note changed.",
				},
			},
			Required: []string{"path", "content", "expected_version"},
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

		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		snap, err := v.ReplaceNote(path, content, expected)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to patch note: %v", err)), nil
		}
		return writeResult(snap), nil
	})
}

// update_frontmatter - Update frontmatter fields
func registerUpdateFrontmatter(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "update_frontmatter",
		Description: "Update frontmatter fields in a note (preserves body). expected_version is optional.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"updates": map[string]interface{}{
					"type":        "object",
					"description": "Frontmatter fields to update (null to delete)",
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Version from get_note. Optional: if given and the note changed since, the write is refused with the current version.",
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

		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		snap, err := v.UpdateFrontmatter(path, updates, expected)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update frontmatter: %v", err)), nil
		}
		return writeResult(snap), nil
	})
}

// delete_note - Delete a note
func registerDeleteNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "delete_note",
		Description: "Delete a note from the vault. Requires expected_version (the \"version\" from get_note); refused with the current version if the note changed.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Required. The \"version\" from get_note; the call is refused with the current version if the note changed.",
				},
			},
			Required: []string{"path", "expected_version"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}

		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		path, err = v.DeleteNote(path, expected)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete note: %v", err)), nil
		}
		return jsonResult(struct {
			Success bool   `json:"success"`
			Path    string `json:"path"`
		}{true, path}), nil
	})
}
