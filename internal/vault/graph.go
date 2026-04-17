package vault

import (
	"obsidian-mcp/internal/markdown"
	"path/filepath"
	"strings"
)

// LinkGraph represents the vault's note connections
type LinkGraph struct {
	Nodes map[string]*GraphNode // path -> node
	Edges []GraphEdge
}

type GraphNode struct {
	Path         string
	Outgoing     []string // paths this note links to
	Incoming     []string // paths that link to this note
}

type GraphEdge struct {
	From string
	To   string
}

// GetBacklinks returns all notes that link to the given note
func (v *Vault) GetBacklinks(path string) ([]string, error) {
	// Normalize path
	targetName := strings.TrimSuffix(filepath.Base(path), ".md")

	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	var backlinks []string

	for _, file := range files {
		if file == path {
			continue // Skip self
		}

		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		links := markdown.ParseWikilinks(content)
		for _, link := range links {
			linkName := strings.TrimSuffix(filepath.Base(link.Target), ".md")
			if linkName == targetName {
				backlinks = append(backlinks, file)
				break
			}
		}
	}

	return backlinks, nil
}

// GetOutgoingLinks returns all notes linked from the given note
func (v *Vault) GetOutgoingLinks(path string) ([]string, error) {
	content, err := v.ReadNote(path)
	if err != nil {
		return nil, err
	}

	targets := markdown.GetOutgoingLinks(content)

	// Resolve to actual paths
	var resolved []string
	for _, target := range targets {
		// Try to find the actual file
		if resolvedPath, err := v.ResolvePath(target); err == nil {
			resolved = append(resolved, resolvedPath)
		} else {
			// Keep unresolved link
			resolved = append(resolved, target)
		}
	}

	return resolved, nil
}

// GetOrphanedNotes returns notes with no incoming or outgoing links
func (v *Vault) GetOrphanedNotes() ([]string, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	var orphans []string

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		// Check outgoing links
		outgoing := markdown.GetOutgoingLinks(content)
		if len(outgoing) > 0 {
			continue // Has outgoing links
		}

		// Check incoming links
		backlinks, err := v.GetBacklinks(file)
		if err != nil || len(backlinks) > 0 {
			continue // Has incoming links or error
		}

		orphans = append(orphans, file)
	}

	return orphans, nil
}

// BuildLinkGraph constructs the full link graph of the vault
func (v *Vault) BuildLinkGraph() (*LinkGraph, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	graph := &LinkGraph{
		Nodes: make(map[string]*GraphNode),
	}

	// Initialize nodes
	for _, file := range files {
		graph.Nodes[file] = &GraphNode{
			Path:     file,
			Outgoing: []string{},
			Incoming: []string{},
		}
	}

	// Build edges
	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		targets := markdown.GetOutgoingLinks(content)

		for _, target := range targets {
			// Try to resolve target
			resolved, err := v.ResolvePath(target)
			if err != nil {
				continue // Skip broken links
			}

			// Add edge
			graph.Edges = append(graph.Edges, GraphEdge{
				From: file,
				To:   resolved,
			})

			// Update nodes
			if node, exists := graph.Nodes[file]; exists {
				node.Outgoing = append(node.Outgoing, resolved)
			}
			if node, exists := graph.Nodes[resolved]; exists {
				node.Incoming = append(node.Incoming, file)
			}
		}
	}

	return graph, nil
}

// GetLinkedMentions returns notes that mention the target note name in text (not as wikilink)
func (v *Vault) GetLinkedMentions(path string) ([]string, error) {
	targetName := strings.TrimSuffix(filepath.Base(path), ".md")

	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	var mentions []string

	for _, file := range files {
		if file == path {
			continue
		}

		content, err := v.ReadNote(file)
		if err != nil {
			continue
		}

		// Check if note name appears in content (case-insensitive)
		if strings.Contains(strings.ToLower(content), strings.ToLower(targetName)) {
			mentions = append(mentions, file)
		}
	}

	return mentions, nil
}
