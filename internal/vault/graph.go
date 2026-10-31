package vault

import (
	"path"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/index"
)

// linkIndex returns the vault's link index, building it on first use. A
// failed Build is not remembered: the next call tries again.
func (v *Vault) linkIndex() (*index.Index, error) {
	if ix := v.idx.Load(); ix != nil {
		return ix, nil
	}
	v.idxMu.Lock()
	defer v.idxMu.Unlock()
	if ix := v.idx.Load(); ix != nil {
		return ix, nil
	}
	ix, err := index.Build(v)
	if err != nil {
		return nil, err
	}
	v.idx.Store(ix)
	return ix, nil
}

// graphErr maps an index query error for rel, the addressed path of arg,
// to the public error: "note not found: <arg>" when rel is not an indexed
// note (an addressed note in a hidden folder), otherwise the index error (a
// refresh failure, which carries vault-relative paths only).
func graphErr(ix *index.Index, arg, rel string, err error) error {
	if _, ok := ix.Canonical(rel); !ok {
		return &notFoundError{arg: arg}
	}
	return err
}

// GetBacklinks returns the edges from other notes to the note arg
// addresses (see AddressNote), sorted by source note.
func (v *Vault) GetBacklinks(arg string) ([]index.Edge, error) {
	_, edges, err := v.NoteBacklinks(arg)
	return edges, err
}

// NoteBacklinks is GetBacklinks that also returns the path arg addressed,
// the note the edges belong to, for labelling the result.
func (v *Vault) NoteBacklinks(arg string) (string, []index.Edge, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return "", nil, err
	}
	ix, err := v.linkIndex()
	if err != nil {
		return "", nil, err
	}
	edges, err := ix.Backlinks(rel)
	if err != nil {
		return "", nil, graphErr(ix, arg, rel, err)
	}
	return rel, edges, nil
}

// GetOutgoingLinks returns the deduped links of the note arg addresses (see
// AddressNote), in source order. Unresolved targets are ghost edges.
func (v *Vault) GetOutgoingLinks(arg string) ([]index.Edge, error) {
	_, edges, err := v.NoteOutgoingLinks(arg)
	return edges, err
}

// NoteOutgoingLinks is GetOutgoingLinks that also returns the path arg
// addressed, the note the edges belong to, for labelling the result.
func (v *Vault) NoteOutgoingLinks(arg string) (string, []index.Edge, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return "", nil, err
	}
	ix, err := v.linkIndex()
	if err != nil {
		return "", nil, err
	}
	edges, err := ix.Outgoing(rel)
	if err != nil {
		return "", nil, graphErr(ix, arg, rel, err)
	}
	return rel, edges, nil
}

// GetOrphanedNotes returns the sorted notes with no outgoing links and no
// links from other notes.
func (v *Vault) GetOrphanedNotes() ([]string, error) {
	ix, err := v.linkIndex()
	if err != nil {
		return nil, err
	}
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	return ix.Orphans(), nil
}

// BuildLinkGraph returns every note, linked attachment and ghost target, and
// every edge between them.
func (v *Vault) BuildLinkGraph() ([]index.GraphNode, []index.Edge, error) {
	ix, err := v.linkIndex()
	if err != nil {
		return nil, nil, err
	}
	if err := ix.Refresh(); err != nil {
		return nil, nil, err
	}
	nodes, edges := ix.Graph()
	return nodes, edges, nil
}

// GetLinkedMentions returns notes that mention the name of the note arg
// addresses (see AddressNote) in text (not as wikilink)
func (v *Vault) GetLinkedMentions(arg string) ([]string, error) {
	_, mentions, err := v.NoteLinkedMentions(arg)
	return mentions, err
}

// NoteLinkedMentions is GetLinkedMentions that also returns the path arg
// addressed, for labelling the result.
func (v *Vault) NoteLinkedMentions(arg string) (string, []string, error) {
	self, err := v.AddressNote(arg)
	if err != nil {
		return "", nil, err
	}
	targetName := strings.TrimSuffix(path.Base(self), ".md")

	files, err := v.ListFiles()
	if err != nil {
		return "", nil, err
	}

	var mentions []string

	for _, file := range files {
		if file == self {
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

	return self, mentions, nil
}
