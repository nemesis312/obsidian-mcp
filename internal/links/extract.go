// Package links extracts and resolves note references (wikilinks, embeds and
// markdown links) following Obsidian's rules. It does no I/O.
package links

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// Kind is the syntax a Ref was written with.
type Kind int

const (
	KindWikilink Kind = iota // [[...]]
	KindEmbed                // ![[...]]
	KindMarkdown             // [text](target)
)

// Ref is one link found in a note.
type Ref struct {
	Kind    Kind
	Raw     string // exact source text, e.g. "![[Note#H|A]]"
	Target  string // decoded target without heading/block/alias; may contain "/" and an extension
	Heading string // after '#', without '^'
	Block   string // after '#^'
	Alias   string // after '|' (wikilink) or the link text (markdown)
	Offset  int    // byte offset of Raw in the original content
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// Extract returns the links in content in source order. It scans the whole
// content, frontmatter included, and ignores fenced code and inline code.
func Extract(content string) []Ref {
	masked := markdown.MaskCode(content)
	var refs []Ref

	scanWikilinks(masked, func(start, end int, embed bool, inner string) {
		ref, ok := parseWikilink(inner)
		if !ok {
			return
		}
		ref.Kind = KindWikilink
		if embed {
			ref.Kind = KindEmbed
		}
		ref.Raw = content[start:end]
		ref.Offset = start
		refs = append(refs, ref)
	})

	scanMarkdownLinks(masked, func(start, end int, text, dest string) {
		ref, ok := parseMarkdownLink(text, dest)
		if !ok {
			return
		}
		ref.Kind = KindMarkdown
		ref.Raw = content[start:end]
		ref.Offset = start
		refs = append(refs, ref)
	})

	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Offset < refs[j].Offset })
	return refs
}

// parseWikilink parses the text between [[ and ]]. The pipe may be escaped as
// "\|" (markdown tables).
func parseWikilink(inner string) (Ref, bool) {
	var ref Ref
	body := inner
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		body = strings.TrimSuffix(inner[:i], `\`)
		ref.Alias = strings.TrimSpace(inner[i+1:])
	}
	ref.Target, ref.Heading, ref.Block = splitFragment(strings.TrimSpace(body))
	if ref.Target == "" && ref.Heading == "" && ref.Block == "" {
		return Ref{}, false
	}
	return ref, true
}

func parseMarkdownLink(text, dest string) (Ref, bool) {
	if len(dest) >= 2 && strings.HasPrefix(dest, "<") && strings.HasSuffix(dest, ">") {
		dest = dest[1 : len(dest)-1]
	}
	if dest == "" || strings.HasPrefix(dest, "#") || schemeRe.MatchString(dest) {
		return Ref{}, false
	}
	ref := Ref{Alias: text}
	ref.Target, ref.Heading, ref.Block = splitFragment(dest)
	ref.Target = unescape(ref.Target)
	ref.Heading = unescape(ref.Heading)
	ref.Block = unescape(ref.Block)
	if ref.Target == "" {
		return Ref{}, false
	}
	return ref, true
}

// splitFragment splits "Note#Heading" or "Note#^block" at the first '#'.
func splitFragment(s string) (target, heading, block string) {
	target, frag, found := strings.Cut(s, "#")
	target = strings.TrimSpace(target)
	if !found {
		return target, "", ""
	}
	frag = strings.TrimSpace(frag)
	if rest, ok := strings.CutPrefix(frag, "^"); ok {
		return target, "", rest
	}
	return target, frag, ""
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// The scanners below are hand-written equivalents of the regexes
//   (!?)\[\[([^\[\]\n]*?)\]\]
//   (!?)\[([^\]\n]*)\]\((<[^>\n]+>|[^)\s]+)\)
// Go's regexp engine needs seconds on megabytes of unclosed "[[" under the race
// detector, while these stay linear: every inner scan stops at a byte that the
// next scan resumes from.

// scanWikilinks calls fn for every [[...]] or ![[...]] in s.
func scanWikilinks(s string, fn func(start, end int, embed bool, inner string)) {
	pos := 0
	for pos < len(s) {
		k := strings.Index(s[pos:], "[[")
		if k < 0 {
			return
		}
		i := pos + k
		j := i + 2
		for j < len(s) && s[j] != '[' && s[j] != ']' && s[j] != '\n' {
			j++
		}
		if j+1 < len(s) && s[j] == ']' && s[j+1] == ']' {
			start, embed := i, i > pos && s[i-1] == '!'
			if embed {
				start--
			}
			fn(start, j+2, embed, s[i+2:j])
			pos = j + 2
			continue
		}
		pos = j
	}
}

// scanMarkdownLinks calls fn for every [text](dest) or ![text](dest) in s.
// dest is returned as written, angle brackets included.
func scanMarkdownLinks(s string, fn func(start, end int, text, dest string)) {
	pos := 0
	for pos < len(s) {
		k := strings.Index(s[pos:], "](")
		if k < 0 {
			return
		}
		mid := pos + k
		// The text runs back to the previous ']' or newline, never before pos.
		segStart := mid
		for segStart > pos && s[segStart-1] != ']' && s[segStart-1] != '\n' {
			segStart--
		}
		open := strings.IndexByte(s[segStart:mid], '[')
		dest, end := "", 0
		if open >= 0 {
			dest, end = linkDest(s, mid+2)
		}
		if end == 0 {
			pos = mid + 1
			continue
		}
		open += segStart
		start := open
		if open > pos && s[open-1] == '!' {
			start--
		}
		fn(start, end, s[open+1:mid], dest)
		pos = end
	}
}

// linkDest parses the destination starting at s[d] and returns it with the
// offset just past the closing ')'. end is 0 when there is no valid one.
func linkDest(s string, d int) (dest string, end int) {
	if d < len(s) && s[d] == '<' {
		e := d + 1
		for e < len(s) && s[e] != '>' && s[e] != '\n' {
			e++
		}
		if e > d+1 && e+1 < len(s) && s[e] == '>' && s[e+1] == ')' {
			return s[d : e+1], e + 2
		}
	}
	e := d
	for e < len(s) && s[e] != ')' && !isSpace(s[e]) {
		e++
	}
	if e > d && e < len(s) && s[e] == ')' {
		return s[d:e], e + 1
	}
	return "", 0
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}
