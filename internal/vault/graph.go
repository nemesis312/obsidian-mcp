package vault

import (
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/index"
)

// linkIndex returns the vault's link index, building it on first use. A
// failed Build is remembered and returned on every later call.
func (v *Vault) linkIndex() (*index.Index, error) {
	v.idxOnce.Do(func() {
		ix, err := index.Build(v)
		if err != nil {
			v.idxErr = err
			return
		}
		v.idx.Store(ix)
	})
	if ix := v.idx.Load(); ix != nil {
		return ix, nil
	}
	return nil, v.idxErr
}

// lookupKey turns a note path argument into the form the index matches on:
// "/" separators and lexically cleaned ("./a.md" -> "a.md"). It never touches
// the file system; a path that escapes the vault ("../x") simply matches no
// note.
func lookupKey(arg string) string {
	return path.Clean(strings.ReplaceAll(arg, `\`, "/"))
}

func errNoteNotFound(arg string) error {
	return errors.New("note not found: " + arg)
}

// notePathErr maps an index lookup error to the public error: "note not
// found: <arg>" when arg names no indexed note, otherwise the index error
// (a refresh failure, which carries vault-relative paths only).
func notePathErr(ix *index.Index, arg, key string, err error) error {
	if _, ok := ix.Canonical(key); !ok {
		return errNoteNotFound(arg)
	}
	return err
}

// NotePath returns the canonical vault-relative path of the indexed note
// matching arg, without refreshing the index. It is meant to label the
// result of a graph call that just refreshed it.
func (v *Vault) NotePath(arg string) (string, bool) {
	ix := v.idx.Load()
	if ix == nil {
		return "", false
	}
	return ix.Canonical(lookupKey(arg))
}

// GetBacklinks returns the edges from other notes to the given note, sorted
// by source note.
func (v *Vault) GetBacklinks(path string) ([]index.Edge, error) {
	ix, err := v.linkIndex()
	if err != nil {
		return nil, err
	}
	key := lookupKey(path)
	edges, err := ix.Backlinks(key)
	if err != nil {
		return nil, notePathErr(ix, path, key, err)
	}
	return edges, nil
}

// GetOutgoingLinks returns the deduped links of the given note, in source
// order. Unresolved targets are ghost edges.
func (v *Vault) GetOutgoingLinks(path string) ([]index.Edge, error) {
	ix, err := v.linkIndex()
	if err != nil {
		return nil, err
	}
	key := lookupKey(path)
	edges, err := ix.Outgoing(key)
	if err != nil {
		return nil, notePathErr(ix, path, key, err)
	}
	return edges, nil
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

// GetLinkedMentions returns notes that mention the target note name in text (not as wikilink)
func (v *Vault) GetLinkedMentions(path string) ([]string, error) {
	targetName := strings.TrimSuffix(filepath.Base(path), ".md")
	self := filepath.ToSlash(path)

	files, err := v.ListFiles()
	if err != nil {
		return nil, err
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

	return mentions, nil
}
