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
type fileSet struct {
	byPath map[string]string
	byBase map[string][]string // sorted by segments, then Key(path)
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
	fs := fileSet{byPath: map[string]string{}, byBase: map[string][]string{}}
	for _, p := range paths {
		pk, bk := p, path.Base(p)
		if notes {
			pk, bk = trimMD(pk), trimMD(bk)
		}
		pk, bk = Key(pk), Key(bk)
		if old, ok := fs.byPath[pk]; !ok || p < old {
			fs.byPath[pk] = p
		}
		fs.byBase[bk] = append(fs.byBase[bk], p)
	}
	for k, list := range fs.byBase {
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
		fs.byBase[k] = dedupe(list)
	}
	return fs
}

func dedupe(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// trimMD removes a trailing ".md" (any case).
func trimMD(s string) string {
	if len(s) >= 3 && strings.EqualFold(s[len(s)-3:], ".md") {
		return s[:len(s)-3]
	}
	return s
}

func escapes(p string) bool { return p == ".." || strings.HasPrefix(p, "../") }

// Lookup finds a path by exact, case/NFC-insensitive match with ".md" optional.
// It returns the canonical path.
func (r *Resolver) Lookup(p string) (string, bool) {
	if c, ok := r.notes.byPath[Key(trimMD(p))]; ok {
		return c, true
	}
	c, ok := r.attachments.byPath[Key(p)]
	return c, ok
}

// Resolve resolves ref, found in fromNote, following Obsidian's rules.
func (r *Resolver) Resolve(fromNote string, ref Ref) Resolution {
	if ref.Target == "" {
		return Resolution{Status: Resolved, Path: fromNote}
	}
	t := strings.TrimSpace(ref.Target)
	for strings.HasPrefix(t, "./") {
		t = strings.TrimSpace(t[2:])
	}
	rooted := strings.HasPrefix(t, "/")
	t = strings.TrimLeft(t, "/")
	if t == "" {
		return Resolution{Status: Unresolved}
	}

	if ref.Kind == KindMarkdown && !rooted {
		rel := path.Clean(path.Join(path.Dir(fromNote), t))
		if escapes(rel) {
			return Resolution{Status: Unresolved}
		}
		if c, ok := r.notes.byPath[Key(trimMD(rel))]; ok {
			return Resolution{Status: Resolved, Path: c}
		}
		if path.Ext(rel) != "" {
			if c, ok := r.attachments.byPath[Key(rel)]; ok {
				return Resolution{Status: Resolved, Path: c, Attachment: true}
			}
		}
	}

	t = path.Clean(t)
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

// match looks up key (already trimmed as the set expects) either as a full
// path from the vault root, when t has a "/", or by basename.
func (r *Resolver) match(fs *fileSet, fromNote, t, name string, attachment bool) (Resolution, bool) {
	if strings.Contains(t, "/") {
		c, ok := fs.byPath[Key(name)]
		if !ok {
			return Resolution{}, false
		}
		return Resolution{Status: Resolved, Path: c, Attachment: attachment}, true
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
