package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/index"
	"github.com/nemesis312/obsidian-mcp/internal/links"
	"github.com/nemesis312/obsidian-mcp/internal/vault"

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

func nodeKindName(k index.NodeKind) string {
	switch k {
	case index.NodeAttachment:
		return "attachment"
	case index.NodeGhost:
		return "ghost"
	default:
		return "note"
	}
}

func linkKindName(k links.Kind) string {
	switch k {
	case links.KindEmbed:
		return "embed"
	case links.KindMarkdown:
		return "markdown"
	default:
		return "wikilink"
	}
}

type backlinkJSON struct {
	From string `json:"from"`
	Kind string `json:"kind"`
}

type outLinkJSON struct {
	To        string `json:"to"`
	ToKind    string `json:"to_kind"`
	Kind      string `json:"kind"`
	Ambiguous bool   `json:"ambiguous"`
}

type nodeJSON struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type edgeJSON struct {
	From   string `json:"from"`
	To     string `json:"to"`
	ToKind string `json:"to_kind"`
	Kind   string `json:"kind"`
}

// jsonResult marshals v compactly as the tool's text result.
func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError("failed to encode result")
	}
	return mcp.NewToolResultText(string(data))
}

// canonicalOr labels a result with the canonical note path, falling back to
// the argument with "/" separators.
func canonicalOr(v *vault.Vault, arg string) string {
	if c, ok := v.NotePath(arg); ok {
		return c
	}
	return strings.ReplaceAll(arg, `\`, "/")
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

		edges, err := v.GetBacklinks(path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		backlinks := make([]backlinkJSON, 0, len(edges))
		for _, e := range edges {
			backlinks = append(backlinks, backlinkJSON{From: e.From, Kind: linkKindName(e.Kind)})
		}
		return jsonResult(struct {
			Path      string         `json:"path"`
			Backlinks []backlinkJSON `json:"backlinks"`
			Count     int            `json:"count"`
		}{canonicalOr(v, path), backlinks, len(backlinks)}), nil
	})
}

// get_outgoing_links - Get all links from a note
func registerGetOutgoingLinks(s *server.MCPServer, v *vault.Vault) {
	s.AddTool(mcp.Tool{
		Name:        "get_outgoing_links",
		Description: "Get all links (wikilinks, embeds, markdown links) from the specified note, resolved to notes, attachments or ghost targets",
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

		edges, err := v.GetOutgoingLinks(path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		out := make([]outLinkJSON, 0, len(edges))
		for _, e := range edges {
			out = append(out, outLinkJSON{To: e.To, ToKind: nodeKindName(e.ToKind), Kind: linkKindName(e.Kind), Ambiguous: e.Ambiguous})
		}
		return jsonResult(struct {
			Path  string        `json:"path"`
			Links []outLinkJSON `json:"links"`
			Count int           `json:"count"`
		}{canonicalOr(v, path), out, len(out)}), nil
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
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(struct {
			Orphans []string `json:"orphans"`
			Count   int      `json:"count"`
		}{orphans, len(orphans)}), nil
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
		Description: "Get the link graph of the vault: notes, linked attachments and ghost targets as nodes, links as edges. Full graph; can be large on big vaults.",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gnodes, gedges, err := v.BuildLinkGraph()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		nodes := make([]nodeJSON, 0, len(gnodes))
		for _, n := range gnodes {
			nodes = append(nodes, nodeJSON{ID: n.ID, Kind: nodeKindName(n.Kind)})
		}
		edges := make([]edgeJSON, 0, len(gedges))
		for _, e := range gedges {
			edges = append(edges, edgeJSON{From: e.From, To: e.To, ToKind: nodeKindName(e.ToKind), Kind: linkKindName(e.Kind)})
		}
		return jsonResult(struct {
			Nodes     []nodeJSON `json:"nodes"`
			Edges     []edgeJSON `json:"edges"`
			NodeCount int        `json:"node_count"`
			EdgeCount int        `json:"edge_count"`
		}{nodes, edges, len(nodes), len(edges)}), nil
	})
}
