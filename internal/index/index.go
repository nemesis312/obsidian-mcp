// Package index keeps an in-memory index of a vault's notes and the links
// between them. It serves backlinks, outgoing links, orphans and the graph,
// and refreshes itself with a stat walk that re-reads only changed notes.
// The markdown files stay the source of truth: the index can always be
// rebuilt from them.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nemesis312/obsidian-mcp/internal/links"
	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// FileStat is the stat data the index compares to detect changes.
// Path is vault-relative and uses "/".
type FileStat struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Source is the vault the index reads from.
type Source interface {
	StatNotes() ([]FileStat, error)       // all .md notes
	StatAttachments() ([]FileStat, error) // all non-.md, non-.canvas files outside protected/hidden dirs
	ReadNote(path string) (string, error)
}

// NodeKind is the kind of a graph node or edge target.
type NodeKind int

const (
	NodeNote NodeKind = iota
	NodeAttachment
	NodeGhost
)

// Edge is a resolved link from a note.
type Edge struct {
	From      string // note path
	To        string // path for note/attachment; links.Key(target) for ghost
	ToKind    NodeKind
	Kind      links.Kind
	Ambiguous bool
	Raw       string // first occurrence's Raw
}

// Note is one indexed note. Out is deduped by (ToKind, To).
type Note struct {
	Path        string
	Frontmatter map[string]any
	Out         []Edge
}

// GraphNode is a node of the vault graph.
type GraphNode struct {
	ID   string
	Kind NodeKind
}

type entry struct {
	Note
	refs []links.Ref
	stat FileStat // zero after Update: re-verified by the next stat walk
}

// Index is safe for concurrent use.
type Index struct {
	src Source

	mu       sync.RWMutex
	notes    map[string]*entry
	atts     map[string]struct{}
	resolver *links.Resolver
	in       map[string][]Edge // note path -> edges pointing to it, self-links included
	gen      map[string]uint64 // bumped by Update/Remove so a concurrent Refresh does not apply stale reads
}

// Build stats and reads the whole vault.
func Build(src Source) (*Index, error) {
	ix := &Index{
		src:      src,
		notes:    map[string]*entry{},
		atts:     map[string]struct{}{},
		resolver: links.NewResolver(nil, nil),
		in:       map[string][]Edge{},
		gen:      map[string]uint64{},
	}
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	return ix, nil
}

type readResult struct {
	path    string
	stat    FileStat
	gen     uint64
	content string
	err     error
}

// Refresh stats the vault, re-reads only new or changed notes, drops deleted
// ones and re-resolves every edge when the set of paths changed. When nothing
// changed it costs one stat walk and no reads.
func (ix *Index) Refresh() error {
	noteStats, err := ix.src.StatNotes()
	if err != nil {
		return fmt.Errorf("index: stat notes: %w", err)
	}
	attStats, err := ix.src.StatAttachments()
	if err != nil {
		return fmt.Errorf("index: stat attachments: %w", err)
	}
	walked := make(map[string]FileStat, len(noteStats))
	for _, s := range noteStats {
		walked[s.Path] = s
	}

	// Diff against the current state under the read lock.
	var toRead []readResult
	var deleted []string
	var deletedGen []uint64
	ix.mu.RLock()
	for p, s := range walked {
		e, ok := ix.notes[p]
		if !ok || e.stat.Size != s.Size || !e.stat.ModTime.Equal(s.ModTime) {
			toRead = append(toRead, readResult{path: p, stat: s, gen: ix.gen[p]})
		}
	}
	for p := range ix.notes {
		if _, ok := walked[p]; !ok {
			deleted = append(deleted, p)
			deletedGen = append(deletedGen, ix.gen[p])
		}
	}
	attsChanged := len(attStats) != len(ix.atts)
	if !attsChanged {
		for _, s := range attStats {
			if _, ok := ix.atts[s.Path]; !ok {
				attsChanged = true
				break
			}
		}
	}
	ix.mu.RUnlock()

	if len(toRead) == 0 && len(deleted) == 0 && !attsChanged {
		return nil
	}

	for i := range toRead {
		toRead[i].content, toRead[i].err = ix.src.ReadNote(toRead[i].path)
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	pathsChanged := false
	var changed []*entry
	var errs []error
	for _, r := range toRead {
		if ix.gen[r.path] != r.gen {
			continue // Update or Remove ran meanwhile; it wins
		}
		if r.err != nil {
			if _, ok := ix.notes[r.path]; ok {
				delete(ix.notes, r.path)
				pathsChanged = true
			}
			if !errors.Is(r.err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("index: read %s: %w", r.path, r.err))
			}
			continue
		}
		if _, ok := ix.notes[r.path]; !ok {
			pathsChanged = true
		}
		e := parse(r.path, r.content)
		e.stat = r.stat
		ix.notes[r.path] = e
		changed = append(changed, e)
	}
	for i, p := range deleted {
		if ix.gen[p] != deletedGen[i] {
			continue
		}
		if _, ok := ix.notes[p]; ok {
			delete(ix.notes, p)
			pathsChanged = true
		}
	}
	if attsChanged {
		ix.atts = make(map[string]struct{}, len(attStats))
		for _, s := range attStats {
			ix.atts[s.Path] = struct{}{}
		}
		pathsChanged = true
	}
	ix.reindex(pathsChanged, changed)
	return errors.Join(errs...)
}

// Update re-reads one note now. It is used after the server writes a note.
// If the read fails the note is removed from the index and the error returned.
func (ix *Index) Update(path string) error {
	path = slash(path)
	ix.mu.RLock()
	if _, ok := ix.notes[path]; !ok {
		if c, ok := ix.canonicalLocked(path); ok {
			path = c
		}
	}
	ix.mu.RUnlock()

	content, err := ix.src.ReadNote(path)

	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.gen[path]++
	if err != nil {
		if _, ok := ix.notes[path]; ok {
			delete(ix.notes, path)
			ix.reindex(true, nil)
		}
		return fmt.Errorf("index: update %s: %w", path, err)
	}
	_, existed := ix.notes[path]
	e := parse(path, content)
	ix.notes[path] = e
	ix.reindex(!existed, []*entry{e})
	return nil
}

// Remove drops a note from the index.
func (ix *Index) Remove(path string) {
	path = slash(path)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if _, ok := ix.notes[path]; !ok {
		c, ok := ix.canonicalLocked(path)
		if !ok {
			return
		}
		path = c
	}
	ix.gen[path]++
	delete(ix.notes, path)
	ix.reindex(true, nil)
}

// Canonical returns the indexed note path matching path, ignoring case, NFC
// form and an optional ".md". It accepts "\" or "/" and never returns
// attachments.
func (ix *Index) Canonical(path string) (string, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.canonicalLocked(slash(path))
}

func (ix *Index) canonicalLocked(path string) (string, bool) {
	c, ok := ix.resolver.Lookup(path)
	if !ok {
		return "", false
	}
	if _, isNote := ix.notes[c]; !isNote {
		return "", false
	}
	return c, true
}

// Outgoing returns the deduped links of a note, in source order.
func (ix *Index) Outgoing(path string) ([]Edge, error) {
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, err := ix.noteLocked(path)
	if err != nil {
		return nil, err
	}
	return append([]Edge(nil), e.Out...), nil
}

// Backlinks returns the edges from other notes to path, sorted by From.
func (ix *Index) Backlinks(path string) ([]Edge, error) {
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, err := ix.noteLocked(path)
	if err != nil {
		return nil, err
	}
	out := []Edge{}
	for _, edge := range ix.in[e.Path] {
		if edge.From != e.Path {
			out = append(out, edge)
		}
	}
	return out, nil
}

// Orphans returns the sorted notes with no outgoing edges and no incoming
// edges from other notes. A refresh error is ignored: it serves the last
// good state.
func (ix *Index) Orphans() []string {
	_ = ix.Refresh()
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := []string{}
	for p, e := range ix.notes {
		if len(e.Out) == 0 && len(ix.in[p]) == 0 {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Graph returns every note, every ghost target and every linked attachment,
// sorted by (Kind, ID), and every edge sorted by (From, ToKind, To). A
// refresh error is ignored: it serves the last good state.
func (ix *Index) Graph() ([]GraphNode, []Edge) {
	_ = ix.Refresh()
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	nodes := make([]GraphNode, 0, len(ix.notes))
	seen := map[GraphNode]bool{}
	var edges []Edge
	for p, e := range ix.notes {
		nodes = append(nodes, GraphNode{ID: p, Kind: NodeNote})
		for _, edge := range e.Out {
			edges = append(edges, edge)
			if edge.ToKind == NodeNote {
				continue
			}
			n := GraphNode{ID: edge.To, Kind: edge.ToKind}
			if !seen[n] {
				seen[n] = true
				nodes = append(nodes, n)
			}
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind < nodes[j].Kind
		}
		return nodes[i].ID < nodes[j].ID
	})
	sortEdges(edges)
	return nodes, edges
}

func (ix *Index) noteLocked(path string) (*entry, error) {
	p := slash(path)
	if e, ok := ix.notes[p]; ok {
		return e, nil
	}
	if c, ok := ix.canonicalLocked(p); ok {
		return ix.notes[c], nil
	}
	return nil, fmt.Errorf("index: note not found: %s", path)
}

// reindex re-resolves edges and rebuilds the in map. When the path set
// changed it rebuilds the resolver and re-resolves every note's stored refs
// (no I/O); otherwise only the changed notes.
func (ix *Index) reindex(pathsChanged bool, changed []*entry) {
	if pathsChanged {
		notes := make([]string, 0, len(ix.notes))
		for p := range ix.notes {
			notes = append(notes, p)
		}
		atts := make([]string, 0, len(ix.atts))
		for p := range ix.atts {
			atts = append(atts, p)
		}
		ix.resolver = links.NewResolver(notes, atts)
		for _, e := range ix.notes {
			e.Out = ix.resolve(e)
		}
	} else {
		for _, e := range changed {
			e.Out = ix.resolve(e)
		}
	}
	in := make(map[string][]Edge)
	for _, e := range ix.notes {
		for _, edge := range e.Out {
			if edge.ToKind == NodeNote {
				in[edge.To] = append(in[edge.To], edge)
			}
		}
	}
	for _, list := range in {
		sortEdges(list)
	}
	ix.in = in
}

type edgeKey struct {
	kind NodeKind
	to   string
}

func (ix *Index) resolve(e *entry) []Edge {
	out := []Edge{}
	seen := map[edgeKey]bool{}
	for _, ref := range e.refs {
		res := ix.resolver.Resolve(e.Path, ref)
		edge := Edge{From: e.Path, Kind: ref.Kind, Raw: ref.Raw}
		switch {
		case res.Status == links.Unresolved:
			edge.ToKind, edge.To = NodeGhost, links.Key(ref.Target)
		case res.Attachment:
			edge.ToKind, edge.To = NodeAttachment, res.Path
		default:
			edge.ToKind, edge.To = NodeNote, res.Path
		}
		edge.Ambiguous = res.Status == links.Ambiguous
		k := edgeKey{edge.ToKind, edge.To}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, edge)
	}
	return out
}

func parse(path, content string) *entry {
	e := &entry{Note: Note{Path: path}, refs: links.Extract(content)}
	if fm, _, err := markdown.ParseFrontmatter(content); err == nil && fm != nil {
		e.Frontmatter = fm.Data
	}
	return e
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.ToKind != b.ToKind {
			return a.ToKind < b.ToKind
		}
		return a.To < b.To
	})
}

func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
