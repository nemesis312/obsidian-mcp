package links

// Resolution rules. They were checked against the Obsidian app's metadata
// cache (app.metadataCache.resolvedLinks) on 2026-10-31. They apply alike to
// wikilinks, embeds and markdown links:
//
//  1. An empty target (only a heading or block) is the source note.
//  2. A target starting with "./" or "../" is a path relative to the source
//     note's folder, and only that; one that climbs out of the vault is
//     unresolved.
//  3. A target starting with "/" is an exact path from the vault root.
//  4. Otherwise an exact path from the vault root wins.
//  5. Otherwise files whose path ends with the target, by whole segments,
//     are candidates: the one in the source note's folder wins, then the one
//     with the fewest segments, then the smallest Key. More than one
//     candidate is Ambiguous.
//  6. Notes are tried before attachments.
//
// Matching ignores case and Unicode form, and ".md" is optional for notes.
// Paths that differ only in case are never picked silently: Ambiguous.

import (
	"path"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Key returns the matching key for s: NFC-normalized and lowercased.
func Key(s string) string { return strings.ToLower(norm.NFC.String(s)) }

// Status is the outcome of resolving a Ref.
type Status int

const (
	Resolved Status = iota
	Ambiguous
	Unresolved
)

// Resolution is the result of resolving one Ref.
type Resolution struct {
	Status     Status
	Path       string   // vault-relative, "/" separators; set for Resolved and Ambiguous (the tie-break winner)
	Candidates []string // all matches when Ambiguous, in tie-break order
	Attachment bool     // Path is a non-.md file
}

// file is one path of a fileSet with its keys computed once.
type file struct {
	path string
	key  string // Key(path), without ".md" for notes
	dir  string // Key(path.Dir(path))
	segs int
}

// fileSet indexes a set of vault paths by full-path key and basename key.
// Paths whose keys differ only by case or Unicode form share one key.
type fileSet struct {
	byPath map[string][]string // sorted lexicographically
	byBase map[string][]file   // sorted by segments, then key, then path
}

// Resolver resolves link targets against the paths of a vault. It does no I/O.
type Resolver struct {
	notes, attachments fileSet
}

// NewResolver builds a Resolver. Paths are vault-relative and use "/".
func NewResolver(notes, attachments []string) *Resolver {
	return &Resolver{
		notes:       newFileSet(notes, true),
		attachments: newFileSet(attachments, false),
	}
}

func newFileSet(paths []string, notes bool) fileSet {
	fs := fileSet{byPath: map[string][]string{}, byBase: map[string][]file{}}
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		pk := p
		if notes {
			pk = trimMD(pk)
		}
		pk = Key(pk)
		fs.byPath[pk] = append(fs.byPath[pk], p)
		bk := path.Base(pk)
		fs.byBase[bk] = append(fs.byBase[bk], file{path: p, key: pk, dir: Key(path.Dir(p)), segs: strings.Count(p, "/")})
	}
	for _, list := range fs.byPath {
		sort.Strings(list)
	}
	for _, list := range fs.byBase {
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.segs != b.segs {
				return a.segs < b.segs
			}
			if a.key != b.key {
				return a.key < b.key
			}
			return a.path < b.path
		})
	}
	return fs
}

// trimMD removes a trailing ".md" (any case).
func trimMD(s string) string {
	if len(s) >= 3 && strings.EqualFold(s[len(s)-3:], ".md") {
		return s[:len(s)-3]
	}
	return s
}

func escapes(p string) bool { return p == ".." || strings.HasPrefix(p, "../") }

// isAttachmentName reports whether p may name an attachment: it has an
// extension other than ".md".
func isAttachmentName(p string) bool {
	ext := path.Ext(p)
	return ext != "" && !strings.EqualFold(ext, ".md")
}

// anchor is where a target is looked up from.
type anchor int

const (
	anchorAny    anchor = iota // exact path, then path suffix (rules 4 and 5)
	anchorRoot                 // leading "/": exact path from the vault root
	anchorSource               // "./" or "../": relative to the source folder
)

// cleanTarget is the one place a target is normalized for matching: it
// strips surrounding spaces, works out the anchor, strips "./" prefixes and
// leading "/" and path-cleans the rest. It returns "" when nothing is left.
func cleanTarget(target string) (string, anchor) {
	t := strings.TrimSpace(target)
	a := anchorAny
	switch {
	case strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../"):
		a = anchorSource
		for strings.HasPrefix(t, "./") {
			t = strings.TrimSpace(t[2:])
		}
	case strings.HasPrefix(t, "/"):
		a = anchorRoot
	}
	if t = strings.TrimLeft(t, "/"); t == "" {
		return "", a
	}
	t = path.Clean(t)
	if a == anchorSource && t == "." {
		return "", a // "./a/.." is the source folder: names no file
	}
	return t, a
}

// NormalizeTarget returns the form of a non-empty target that Resolve
// matches on: cleaned as Resolve cleans it and then Key'd. From the same
// note, two refs resolve alike, whatever their kind, when their keys and
// rooted are equal. A relative target ("./", "../") keeps a "./" prefix,
// so "[[./ ./Index]]" and "[[./index]]" share the key "./index", which
// differs from "index" and from "/index" (rooted). ".md" is kept because
// "x.png" and "x.png.md" resolve differently. The key is "" when nothing
// names a file ("/", "./"); an empty Target, which names the source note,
// also yields "", so callers must check Target == "" first.
func NormalizeTarget(target string) (key string, rooted bool) {
	t, a := cleanTarget(target)
	if t == "" {
		return "", a == anchorRoot
	}
	key = Key(t)
	if a == anchorSource {
		key = "./" + key
	}
	return key, a == anchorRoot
}

// GhostKey returns the key an unresolved target is known by: its
// NormalizeTarget key without a "./" prefix or ".md". "[[Ghost]]",
// "[[Ghost.md]]", "[[./Ghost]]" and "[g](Ghost.md)" share one key, even
// when "./" is written in different folders. It is "" when the target names
// nothing ("/", "./").
func GhostKey(target string) string {
	k, _ := NormalizeTarget(target)
	return trimMD(strings.TrimPrefix(k, "./"))
}

// Lookup finds a path by exact, case/NFC-insensitive match with ".md" optional.
// It returns the canonical path.
// Paths that differ only by case or Unicode form share a key; the
// lexicographically smallest one is returned.
func (r *Resolver) Lookup(p string) (string, bool) {
	if list := r.notes.byPath[Key(trimMD(p))]; len(list) > 0 {
		return list[0], true
	}
	if list := r.attachments.byPath[Key(p)]; len(list) > 0 {
		return list[0], true
	}
	return "", false
}

// Resolve resolves ref, found in fromNote, following Obsidian's rules (see
// the top of this file). The kind of ref does not matter.
func (r *Resolver) Resolve(fromNote string, ref Ref) Resolution {
	if ref.Target == "" {
		return Resolution{Status: Resolved, Path: fromNote}
	}
	t, a := cleanTarget(ref.Target)
	if t == "" {
		return Resolution{Status: Unresolved}
	}
	switch a {
	case anchorSource:
		t = path.Join(path.Dir(fromNote), t)
		if t == "." || escapes(t) {
			return Resolution{Status: Unresolved}
		}
		return r.exact(t)
	case anchorRoot:
		return r.exact(t)
	}
	if t == "." || escapes(t) {
		return Resolution{Status: Unresolved}
	}

	nk := Key(trimMD(t))
	if list := r.notes.byPath[nk]; len(list) > 0 {
		return pick(list, false)
	}
	if res, ok := suffix(&r.notes, fromNote, nk, false); ok {
		return res
	}
	if isAttachmentName(t) {
		ak := Key(t)
		if list := r.attachments.byPath[ak]; len(list) > 0 {
			return pick(list, true)
		}
		if res, ok := suffix(&r.attachments, fromNote, ak, true); ok {
			return res
		}
	}
	return Resolution{Status: Unresolved}
}

// exact resolves t only as an exact path from the vault root, notes first.
func (r *Resolver) exact(t string) Resolution {
	if list := r.notes.byPath[Key(trimMD(t))]; len(list) > 0 {
		return pick(list, false)
	}
	if isAttachmentName(t) {
		if list := r.attachments.byPath[Key(t)]; len(list) > 0 {
			return pick(list, true)
		}
	}
	return Resolution{Status: Unresolved}
}

// pick resolves to the single path of list, or, when several paths share
// one key (case-only duplicates), to an Ambiguous result: the resolver never
// silently picks. list is sorted, so Path is the smallest.
func pick(list []string, attachment bool) Resolution {
	if len(list) == 1 {
		return Resolution{Status: Resolved, Path: list[0], Attachment: attachment}
	}
	return Resolution{Status: Ambiguous, Path: list[0], Candidates: append([]string(nil), list...), Attachment: attachment}
}

// suffix matches k (a key, trimmed as fs expects) against the paths of fs
// that equal it or end with "/"+k. Only the paths sharing k's basename are
// scanned, never the whole set. Candidates in the source note's folder come
// first, then the rest in fs order (fewest segments, then key).
func suffix(fs *fileSet, fromNote, k string, attachment bool) (Resolution, bool) {
	list := fs.byBase[path.Base(k)]
	if len(list) == 0 {
		return Resolution{}, false
	}
	nested := strings.Contains(k, "/")
	tail := "/" + k
	match := func(f file) bool {
		return !nested || f.key == k || strings.HasSuffix(f.key, tail)
	}
	if !nested && len(list) == 1 {
		return Resolution{Status: Resolved, Path: list[0].path, Attachment: attachment}, true
	}
	dir := Key(path.Dir(fromNote))
	cands := make([]string, 0, len(list))
	for _, f := range list {
		if f.dir == dir && match(f) {
			cands = append(cands, f.path)
		}
	}
	for _, f := range list {
		if f.dir != dir && match(f) {
			cands = append(cands, f.path)
		}
	}
	switch len(cands) {
	case 0:
		return Resolution{}, false
	case 1:
		return Resolution{Status: Resolved, Path: cands[0], Attachment: attachment}, true
	}
	return Resolution{Status: Ambiguous, Path: cands[0], Candidates: cands, Attachment: attachment}, true
}
