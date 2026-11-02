package links

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// LinkStyle is how a link's target is written. A rewrite keeps it.
type LinkStyle int

const (
	StyleName     LinkStyle = iota // no "/": "Note", "Note.md"
	StylePath                      // has "/", not rooted: "a/Note"
	StyleRooted                    // leading "/": "/a/Note"
	StyleRelative                  // leading "./" or "../"
)

// StyleOf returns the style ref's target is written in.
func StyleOf(ref Ref) LinkStyle {
	t := strings.TrimSpace(ref.Target)
	switch {
	case strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../"):
		return StyleRelative
	case strings.HasPrefix(t, "/"):
		return StyleRooted
	case strings.Contains(t, "/"):
		return StylePath
	}
	return StyleName
}

// TargetFor returns the target text (no fragment, no alias) to write in
// `from` so that it resolves to note `to` under r, in the given style.
//
//   - StyleName: the basename when it resolves to `to` unambiguously
//     (Status Resolved); otherwise the shortest whole-segment suffix that
//     does; otherwise the full vault path (spec §7.3: a name stays
//     name-only while it is unambiguous, else it becomes path-qualified).
//   - StylePath: the full vault path without a leading "/".
//   - StyleRooted: "/" plus the full vault path.
//   - StyleRelative: a path relative to from's folder, starting with "./"
//     or "../".
//
// The text ends in ".md" only when keepExt is set; an attachment keeps its
// own extension.
func TargetFor(r *Resolver, from, to string, style LinkStyle, keepExt bool) string {
	p := to
	if !keepExt {
		p = trimMD(p)
	}
	switch style {
	case StylePath:
		return p
	case StyleRooted:
		return "/" + p
	case StyleRelative:
		return relativeTo(path.Dir(from), p)
	}
	segs := strings.Split(p, "/")
	for k := 1; k < len(segs); k++ {
		cand := strings.Join(segs[len(segs)-k:], "/")
		res := r.Resolve(from, Ref{Kind: KindWikilink, Target: cand})
		if res.Status == Resolved && res.Path == to {
			return cand
		}
	}
	return p
}

// relativeTo returns p (vault-relative) as a path from folder dir, starting
// with "./" or "../". Folders are compared as written.
func relativeTo(dir, p string) string {
	var from []string
	if dir != "." && dir != "" {
		from = strings.Split(dir, "/")
	}
	to := strings.Split(p, "/")
	n := 0
	for n < len(from) && n < len(to)-1 && from[n] == to[n] {
		n++
	}
	ups := len(from) - n
	rest := strings.Join(to[n:], "/")
	if ups == 0 {
		return "./" + rest
	}
	return strings.Repeat("../", ups) + rest
}

// RewriteRaw returns ref.Raw with only its target part replaced by
// newTarget. Ref has no sub-spans, so Raw is parsed again here.
//
// For a wikilink or embed the target part runs from after "[[" up to the
// first '#', '|' or `\|`; the rest is kept byte for byte. For a markdown
// link only the path of the destination (before '#') is replaced: written
// raw inside "<…>", percent-encoded otherwise (see encodeDest).
//
// Delimiters are looked up in Raw with code masked, as Extract does, so a
// '#' or '|' inside inline code is part of the target, not a delimiter.
func RewriteRaw(ref Ref, newTarget string) string {
	raw := ref.Raw
	m := markdown.MaskCode(raw) // same length as raw
	if ref.Kind == KindMarkdown {
		return rewriteMarkdown(raw, m, newTarget)
	}
	open := strings.Index(m, "[[")
	end := len(raw) - 2 // before "]]"
	if open < 0 || end < open+2 || !strings.HasSuffix(raw, "]]") {
		return raw
	}
	start := open + 2
	stop := end
	if i := strings.IndexAny(m[start:end], "#|"); i >= 0 {
		stop = start + i
		if m[stop] == '|' && stop > start && m[stop-1] == '\\' {
			stop--
		}
	}
	return raw[:start] + newTarget + raw[stop:]
}

func rewriteMarkdown(raw, m, newTarget string) string {
	mid := strings.Index(m, "](")
	if mid < 0 || !strings.HasSuffix(raw, ")") || len(raw)-1 < mid+2 {
		return raw
	}
	start, end := mid+2, len(raw)-1
	angle := end-start >= 2 && m[start] == '<' && m[end-1] == '>'
	enc := encodeDest(newTarget)
	if angle {
		start++
		end--
		enc = newTarget
	}
	stop := end
	if i := strings.IndexByte(m[start:end], '#'); i >= 0 {
		stop = start + i
	}
	return raw[:start] + enc + raw[stop:]
}

// destEscaper percent-encodes the bytes a plain markdown destination path
// cannot hold as written. Unicode is kept, as Obsidian does.
var destEscaper = strings.NewReplacer(
	"%", "%25",
	" ", "%20",
	"(", "%28",
	")", "%29",
	"<", "%3C",
	">", "%3E",
	"#", "%23",
)

func encodeDest(s string) string { return destEscaper.Replace(s) }

// Edit replaces Old, found at byte Offset of a content, with New.
type Edit struct {
	Offset   int
	Old, New string
}

// ApplyEdits applies edits to content. The edits are applied in Offset
// order and must not overlap, and content[Offset:Offset+len(Old)] must equal
// Old for each; otherwise it returns an error and no result. Bytes outside
// the edits are kept as they are.
func ApplyEdits(content string, edits []Edit) (string, error) {
	sorted := append([]Edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Offset < sorted[j].Offset })
	var b strings.Builder
	b.Grow(len(content))
	pos := 0
	for _, e := range sorted {
		if e.Offset < pos {
			return "", fmt.Errorf("apply edits: edit at %d overlaps the previous one", e.Offset)
		}
		if e.Offset+len(e.Old) > len(content) {
			return "", fmt.Errorf("apply edits: edit at %d runs past the end", e.Offset)
		}
		if content[e.Offset:e.Offset+len(e.Old)] != e.Old {
			return "", fmt.Errorf("apply edits: content at %d does not match the expected text", e.Offset)
		}
		b.WriteString(content[pos:e.Offset])
		b.WriteString(e.New)
		pos = e.Offset + len(e.Old)
	}
	b.WriteString(content[pos:])
	return b.String(), nil
}
