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
	registerEditSection(s, v)
	registerMoveNote(s, v)
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

// edit_section - Replace, append to or prepend to one section of a note
func registerEditSection(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "edit_section",
		Description: "Edit one section of a note, the part under a heading, leaving every other byte as it was. A section includes its subsections. Use \"Parent > Child\" to pick between headings with the same text. mode=replace needs expected_version (the \"version\" from get_note); append and prepend do not require it.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"heading": map[string]interface{}{
					"type":        "string",
					"description": "Heading text of the section, e.g. \"Risks\" or \"Plan > Risks\" when the text is not unique",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"replace", "append", "prepend"},
					"description": "replace: swap the section body (needs expected_version); append: add at the end of the section; prepend: add right after the heading",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Text to write",
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Version from get_note. Required for mode=replace; optional otherwise: if given and the note changed since, the write is refused with the current version.",
				},
			},
			Required: []string{"path", "heading", "mode", "content"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}
		heading, err := request.RequireString("heading")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid heading: %v", err)), nil
		}
		mode, err := request.RequireString("mode")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid mode: %v", err)), nil
		}
		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid content: %v", err)), nil
		}
		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		snap, err := v.EditSection(path, heading, vault.SectionMode(mode), content, expected)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to edit section: %v", err)), nil
		}
		return writeResult(snap), nil
	})
}

// move_note - Move or rename a note and rewrite the links the move changes
func registerMoveNote(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name: "move_note",
		Description: "Move or rename a single .md note and keep the vault's links working. " +
			"Rewrites every link form the move affects, in other notes and in the moved note itself: wikilinks, embeds, markdown links, and links with an alias, a #heading or a ^block fragment keep their alias, fragment and form. " +
			"A ghost link that starts resolving because of the move (to the moved note, or for the moved note's own ./ links to another note) is reported in warnings, not rewritten. Links in .canvas files are not rewritten. " +
			"The destination is a vault-relative path (.md is optional; missing folders are created; an existing folder keeps its on-disk spelling). A case-only rename is allowed. " +
			"The move is refused, with nothing written, when: the destination already exists; the source is a symlink; a folder on the destination path is a symlink; the destination is in a protected folder (.git, .obsidian, .obsidian-mcp); its name contains # | [ ] ^, a control character, or a leading or trailing space; or any note's links (the moved note's included) could not be rewritten safely or its frontmatter would break. " +
			"A note that fails to write after the move started is listed in failed; the move is not rolled back. " +
			"Use dry_run=true to preview: it returns the same result, lists every note that could not be rewritten in failed, takes no lock and writes nothing. " +
			"expected_version is optional, except when the destination is inside a hidden folder (a folder name starting with '.'), where it is required.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": notePathDesc,
				},
				"new_path": map[string]interface{}{
					"type":        "string",
					"description": "Vault-relative destination path, e.g. \"Archive/Old note\" (.md is optional). Must not exist yet.",
				},
				"expected_version": map[string]interface{}{
					"type":        "string",
					"description": "Version from get_note. Optional, but required when new_path is inside a hidden folder: if given and the note changed since, the move is refused with the current version.",
				},
				"dry_run": map[string]interface{}{
					"type":        "boolean",
					"description": "Preview the move and its link rewrites without writing anything (default false)",
				},
			},
			Required: []string{"path", "new_path"},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
		}
		newPath, err := request.RequireString("new_path")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid new_path: %v", err)), nil
		}
		expected, err := expectedVersion(request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dryRun := false
		if raw, ok := request.GetArguments()["dry_run"]; ok && raw != nil {
			if dryRun, ok = raw.(bool); !ok {
				return mcp.NewToolResultError("invalid dry_run: must be a boolean"), nil
			}
		}

		res, err := v.MoveNote(ctx, path, newPath, expected, dryRun)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to move: %v", err)), nil
		}
		if res.Updated == nil {
			res.Updated = []vault.LinkChange{}
		}
		if res.Failed == nil {
			res.Failed = []vault.RenameFailure{}
		}
		if res.Warnings == nil {
			res.Warnings = []string{}
		}
		return jsonResult(struct {
			Success bool `json:"success"`
			vault.MoveResult
		}{true, res}), nil
	})
}
