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
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// Source is the vault the index reads from. The index calls it while
// holding its own locks (Refresh holds refreshMu), so implementations must
// not call back into the Index.
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
	To        string // path for note/attachment; links.GhostKey(target) for ghost
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

	// refreshMu serializes Refresh. refreshReq counts Refresh calls and
	// refreshCovered is the last call number a finished walk is fresh for, so
	// callers that queued behind a walk that started after them return at once.
	refreshMu      sync.Mutex
	refreshReq     atomic.Uint64
	refreshCovered uint64

	mu       sync.RWMutex
	notes    map[string]*entry
	atts     map[string]struct{}
	resolver *links.Resolver
	in       map[string][]Edge // note path -> edges pointing to it, self-links included
	// epoch is bumped by every Update and Remove; written records the epoch
	// of the last Update/Remove per path. A Refresh snapshots epoch before
	// its stat walk and leaves alone any path written after that snapshot,
	// because its walk and reads may be older than that write.
	epoch   uint64
	written map[string]uint64
}

// Build stats and reads the whole vault. It fails only when the stat walk
// fails; an unreadable note is indexed as an empty note.
func Build(src Source) (*Index, error) {
	ix := &Index{
		src:      src,
		notes:    map[string]*entry{},
		atts:     map[string]struct{}{},
		resolver: links.NewResolver(nil, nil),
		in:       map[string][]Edge{},
		written:  map[string]uint64{},
	}
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	return ix, nil
}

type readResult struct {
	path    string
	stat    FileStat
	content string
	err     error
}

// Refresh stats the vault, re-reads only new or changed notes, drops deleted
// ones and re-resolves every edge when the set of paths changed. When nothing
// changed it costs one stat walk and no reads. It returns an error only when
// the stat walk fails. A note that cannot be read (other than not existing)
// is indexed as an empty note with a zero stat, so every Refresh retries it
// until a read succeeds.
//
// Concurrent calls coalesce: a call that queued behind a walk which started
// after it returns without walking again. A path written by Update after a
// walk's snapshot is left to that Update, so when Update races an external
// edit of the same note the index may serve the content Update read, which
// can predate that edit, until the next Refresh sees the new stat.
func (ix *Index) Refresh() error {
	ticket := ix.refreshReq.Add(1)
	ix.refreshMu.Lock()
	defer ix.refreshMu.Unlock()
	if ix.refreshCovered >= ticket {
		return nil // a walk that started after this call already finished
	}
	covered := ix.refreshReq.Load()

	ix.mu.RLock()
	snap := ix.epoch
	ix.mu.RUnlock()

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
	ix.mu.RLock()
	for p, s := range walked {
		if ix.written[p] > snap {
			continue
		}
		e, ok := ix.notes[p]
		if !ok || e.stat.Size != s.Size || !e.stat.ModTime.Equal(s.ModTime) {
			toRead = append(toRead, readResult{path: p, stat: s})
		}
	}
	for p := range ix.notes {
		if _, ok := walked[p]; !ok && ix.written[p] <= snap {
			deleted = append(deleted, p)
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

	if len(toRead) > 0 || len(deleted) > 0 || attsChanged {
		for i := range toRead {
			toRead[i].content, toRead[i].err = ix.src.ReadNote(toRead[i].path)
		}
		ix.apply(snap, toRead, deleted, attsChanged, attStats)
	}

	ix.mu.Lock()
	// Refresh calls are serialized, so no later Refresh can hold a snapshot
	// older than this one: entries at or below it are no longer needed.
	for p, w := range ix.written {
		if w <= snap {
			delete(ix.written, p)
		}
	}
	ix.mu.Unlock()
	ix.refreshCovered = covered
	return nil
}

func (ix *Index) apply(snap uint64, toRead []readResult, deleted []string, attsChanged bool, attStats []FileStat) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	pathsChanged := false
	var changed []*entry
	for _, r := range toRead {
		if ix.written[r.path] > snap {
			continue // Update or Remove ran meanwhile; it wins
		}
		_, existed := ix.notes[r.path]
		if errors.Is(r.err, fs.ErrNotExist) {
			if existed {
				delete(ix.notes, r.path)
				pathsChanged = true
			}
			continue
		}
		content, stat := r.content, r.stat
		if r.err != nil {
			// Unreadable: an existing note with no content. A zero stat
			// makes the next Refresh retry it, so transient locks heal.
			content, stat = "", FileStat{}
		}
		e := parse(r.path, content)
		e.stat = stat
		ix.notes[r.path] = e
		changed = append(changed, e)
		if !existed {
			pathsChanged = true
		}
	}
	for _, p := range deleted {
		if ix.written[p] > snap {
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
}

// markWrittenLocked records an Update or Remove of path. ix.mu must be held
// for writing.
func (ix *Index) markWrittenLocked(path string) {
	ix.epoch++
	ix.written[path] = ix.epoch
}

// Update re-reads one note now. It is used after the server writes a note.
// If the note does not exist it is removed from the index. If it exists but
// cannot be read it is indexed as an empty note. In both cases the read
// error is returned.
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
	ix.markWrittenLocked(path)
	_, existed := ix.notes[path]
	if errors.Is(err, fs.ErrNotExist) {
		if existed {
			delete(ix.notes, path)
			ix.reindex(true, nil)
		}
		return fmt.Errorf("index: update %s: %w", path, err)
	}
	if err != nil {
		content = ""
	}
	e := parse(path, content)
	ix.notes[path] = e
	ix.reindex(!existed, []*entry{e})
	if err != nil {
		return fmt.Errorf("index: update %s: %w", path, err)
	}
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
	ix.markWrittenLocked(path)
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
	return append([]Edge{}, e.Out...), nil
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
	edges := []Edge{}
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
		memo := resolveMemo{}
		for _, e := range ix.notes {
			e.Out = ix.resolve(e, memo)
		}
	} else {
		memo := resolveMemo{}
		for _, e := range changed {
			e.Out = ix.resolve(e, memo)
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

// target is what a ref's resolution depends on besides the source folder:
// the target as links.NormalizeTarget keys it (relative "./" targets keep
// their prefix) and whether it is rooted. Every kind resolves alike, so the
// kind is not part of it. Spellings Resolve treats alike share a target.
type target struct {
	key    string
	rooted bool
}

func targetOf(r links.Ref) target {
	key, rooted := links.NormalizeTarget(r.Target)
	return target{key: key, rooted: rooted}
}

// memoKey is what a resolution depends on: the source note's folder (for
// the same-folder tie-break and "./" targets) and the normalized target, so
// a memo hit is always exact.
type memoKey struct {
	dir string
	t   target
}

type memoVal struct {
	toKind    NodeKind
	to        string
	ambiguous bool
}

// resolveMemo caches resolutions for one resolver, so a target shared by many
// notes in a folder is resolved once, however many candidates it has.
type resolveMemo map[memoKey]memoVal

func (ix *Index) resolveRef(from string, ref links.Ref, memo resolveMemo) memoVal {
	// An empty target is the source note itself: not shared across notes.
	k := memoKey{path.Dir(from), targetOf(ref)}
	if ref.Target != "" {
		if v, ok := memo[k]; ok {
			return v
		}
	}
	res := ix.resolver.Resolve(from, ref)
	var v memoVal
	switch {
	case res.Status == links.Unresolved:
		v.toKind, v.to = NodeGhost, links.GhostKey(ref.Target)
	case res.Attachment:
		v.toKind, v.to = NodeAttachment, res.Path
	default:
		v.toKind, v.to = NodeNote, res.Path
	}
	v.ambiguous = res.Status == links.Ambiguous
	if ref.Target != "" {
		memo[k] = v
	}
	return v
}

func (ix *Index) resolve(e *entry, memo resolveMemo) []Edge {
	out := []Edge{}
	seen := map[edgeKey]bool{}
	for _, ref := range e.refs {
		v := ix.resolveRef(e.Path, ref, memo)
		if v.toKind == NodeGhost && v.to == "" {
			continue // "[[/]]", "[[./]]": names no file
		}
		edge := Edge{From: e.Path, To: v.to, ToKind: v.toKind, Kind: ref.Kind, Ambiguous: v.ambiguous, Raw: ref.Raw}
		k := edgeKey{edge.ToKind, edge.To}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, edge)
	}
	return out
}

// refKey identifies refs that always resolve alike from the same note. An
// empty Target (the note itself) and "./" share a normalized target but not
// a resolution, so self tells them apart.
type refKey struct {
	kind links.Kind
	t    target
	self bool
}

// parse extracts a note's refs, keeping only the first ref per (Kind,
// normalized target): repeats and equivalent spellings resolve to the same
// edge, and dropping them bounds memory and resolve work by distinct
// targets, not by link count.
func parse(path, content string) *entry {
	all := links.Extract(content)
	refs := make([]links.Ref, 0, len(all))
	seen := make(map[refKey]bool, len(all))
	for _, r := range all {
		k := refKey{r.Kind, targetOf(r), r.Target == ""}
		if seen[k] {
			continue
		}
		seen[k] = true
		refs = append(refs, r)
	}
	e := &entry{Note: Note{Path: path}, refs: refs}
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

// addressSource is the note a target passed to Address is resolved from:
// one at the vault root, so the same-folder rule means the root.
const addressSource = "_address.md"

// Address resolves target as a [[target]] written in a note at the vault root,
// without refreshing. An attachment-only match is returned as Unresolved.
// An Ambiguous result has no Path: addressing never applies the tie-break.
// Its Candidates are every match, sorted.
func (ix *Index) Address(target string) links.Resolution {
	if strings.TrimSpace(target) == "" {
		return links.Resolution{Status: links.Unresolved}
	}
	ix.mu.RLock()
	res := ix.resolver.Resolve(addressSource, links.Ref{Kind: links.KindWikilink, Target: target})
	ix.mu.RUnlock()
	switch {
	case res.Attachment || res.Status == links.Unresolved:
		return links.Resolution{Status: links.Unresolved}
	case res.Status == links.Ambiguous:
		cands := append([]string(nil), res.Candidates...)
		sort.Strings(cands)
		return links.Resolution{Status: links.Ambiguous, Candidates: cands}
	}
	return links.Resolution{Status: links.Resolved, Path: res.Path}
}

// NotePaths returns every indexed note path, sorted, without refreshing.
func (ix *Index) NotePaths() []string {
	ix.mu.RLock()
	out := make([]string, 0, len(ix.notes))
	for p := range ix.notes {
		out = append(out, p)
	}
	ix.mu.RUnlock()
	sort.Strings(out)
	return out
}
