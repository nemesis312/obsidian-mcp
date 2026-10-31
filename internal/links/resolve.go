package links

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

// fileSet indexes a set of vault paths by full-path key and basename key.
// Paths whose keys differ only by case or Unicode form share one key.
type fileSet struct {
	byPath map[string][]string // sorted lexicographically
	byBase map[string][]string // sorted by segments, then Key(path), then path
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
	fs := fileSet{byPath: map[string][]string{}, byBase: map[string][]string{}}
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		pk, bk := p, path.Base(p)
		if notes {
			pk, bk = trimMD(pk), trimMD(bk)
		}
		pk, bk = Key(pk), Key(bk)
		fs.byPath[pk] = append(fs.byPath[pk], p)
		fs.byBase[bk] = append(fs.byBase[bk], p)
	}
	for _, list := range fs.byPath {
		sort.Strings(list)
	}
	for _, list := range fs.byBase {
		sort.Slice(list, func(i, j int) bool {
			si, sj := strings.Count(list[i], "/"), strings.Count(list[j], "/")
			if si != sj {
				return si < sj
			}
			ki, kj := Key(list[i]), Key(list[j])
			if ki != kj {
				return ki < kj
			}
			return list[i] < list[j]
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

// cleanTarget is the one place a target is normalized for matching: it
// strips surrounding spaces, "./" prefixes and leading "/" (rooted reports
// whether there was one) and path-cleans the rest. It returns "" when
// nothing is left.
func cleanTarget(target string) (t string, rooted bool) {
	t = strings.TrimSpace(target)
	for strings.HasPrefix(t, "./") {
		t = strings.TrimSpace(t[2:])
	}
	rooted = strings.HasPrefix(t, "/")
	if t = strings.TrimLeft(t, "/"); t == "" {
		return "", rooted
	}
	return path.Clean(t), rooted
}

// NormalizeTarget returns the form of a non-empty target that Resolve
// matches on: cleaned as Resolve cleans it and then Key'd. From the same
// note, two refs resolve alike when their kinds are both markdown or both
// not, their keys are equal and, for markdown links, so is rooted (the
// other kinds ignore it). "[[./ ./Index]]" and "[[index]]" share a key;
// ".md" is kept because "x.png" and "x.png.md" resolve differently. The key
// is "" when nothing names a file ("/", "./"); an empty Target, which names
// the source note, also yields "", so callers must check Target == "" first.
func NormalizeTarget(target string) (key string, rooted bool) {
	t, rooted := cleanTarget(target)
	return Key(t), rooted
}

// GhostKey returns the key an unresolved target is known by: its
// NormalizeTarget key without ".md". "[[Ghost]]", "[[Ghost.md]]",
// "[[./Ghost]]" and "[g](Ghost.md)" share one key. It is "" when the target
// names nothing ("/", "./").
func GhostKey(target string) string {
	k, _ := NormalizeTarget(target)
	return trimMD(k)
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

// Resolve resolves ref, found in fromNote, following Obsidian's rules.
func (r *Resolver) Resolve(fromNote string, ref Ref) Resolution {
	if ref.Target == "" {
		return Resolution{Status: Resolved, Path: fromNote}
	}
	t, rooted := cleanTarget(ref.Target)
	if t == "" {
		return Resolution{Status: Unresolved}
	}

	if ref.Kind == KindMarkdown && !rooted {
		rel := path.Join(path.Dir(fromNote), t)
		if escapes(rel) {
			return Resolution{Status: Unresolved}
		}
		if list := r.notes.byPath[Key(trimMD(rel))]; len(list) > 0 {
			return pick(list, false)
		}
		if path.Ext(rel) != "" {
			if list := r.attachments.byPath[Key(rel)]; len(list) > 0 {
				return pick(list, true)
			}
		}
	}

	if t == "." || escapes(t) {
		return Resolution{Status: Unresolved}
	}

	if res, ok := r.match(&r.notes, fromNote, t, trimMD(t), false); ok {
		return res
	}
	if ext := path.Ext(t); ext != "" && !strings.EqualFold(ext, ".md") {
		if res, ok := r.match(&r.attachments, fromNote, t, t, true); ok {
			return res
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

// match looks up key (already trimmed as the set expects) either as a full
// path from the vault root, when t has a "/", or by basename.
func (r *Resolver) match(fs *fileSet, fromNote, t, name string, attachment bool) (Resolution, bool) {
	if strings.Contains(t, "/") {
		list := fs.byPath[Key(name)]
		if len(list) == 0 {
			return Resolution{}, false
		}
		return pick(list, attachment), true
	}
	list := fs.byBase[Key(name)]
	switch len(list) {
	case 0:
		return Resolution{}, false
	case 1:
		return Resolution{Status: Resolved, Path: list[0], Attachment: attachment}, true
	}
	cands := make([]string, 0, len(list))
	dir := Key(path.Dir(fromNote))
	for _, p := range list {
		if Key(path.Dir(p)) == dir {
			cands = append(cands, p)
		}
	}
	for _, p := range list {
		if Key(path.Dir(p)) != dir {
			cands = append(cands, p)
		}
	}
	return Resolution{Status: Ambiguous, Path: cands[0], Candidates: cands, Attachment: attachment}, true
}
