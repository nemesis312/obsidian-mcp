package markdown

import (
	"fmt"
	"strings"
)

const maxListedHeadings = 10

// Heading is one heading found in a note. All offsets are byte offsets into
// the content passed to Headings, BOM included.
type Heading struct {
	Level     int    // 1-6
	Text      string // trimmed, closing #s removed, setext underline excluded
	Path      string // ancestor texts joined by " > ", e.g. "Plan > Risks"
	Start     int    // byte offset of the heading line
	BodyStart int    // byte offset just after the heading line(s), including its line ending
	End       int    // start of the next heading with Level <= this one, or len(content)
}

// SectionNotFoundError reports that no heading matched the query. Headings
// holds up to 10 heading Paths so the caller can pick one.
type SectionNotFoundError struct {
	Query    string
	Headings []string
}

func (e *SectionNotFoundError) Error() string {
	if len(e.Headings) == 0 {
		return fmt.Sprintf("section %q not found: the note has no headings", e.Query)
	}
	return fmt.Sprintf("section %q not found; available headings: %s", e.Query, strings.Join(e.Headings, "; "))
}

// AmbiguousSectionError reports that more than one heading matched the query.
// Matches holds their Paths.
type AmbiguousSectionError struct {
	Query   string
	Matches []string
}

func (e *AmbiguousSectionError) Error() string {
	return fmt.Sprintf("section %q is ambiguous, it matches: %s; pass \"Parent > Child\" to pick one",
		e.Query, strings.Join(e.Matches, "; "))
}

// Headings returns the ATX and setext headings of content in document order.
// Headings inside frontmatter, fenced code blocks and inline code are skipped.
func Headings(content string) []Heading {
	masked := MaskCode(content)
	type line struct{ start, end, next int } // end excludes "\n" and a trailing "\r"
	var lines []line
	for pos := frontmatterEnd(content); pos < len(content); {
		nl := strings.IndexByte(content[pos:], '\n')
		next := len(content)
		end := len(content)
		if nl >= 0 {
			next = pos + nl + 1
			end = pos + nl
		}
		if end > pos && content[end-1] == '\r' {
			end--
		}
		lines = append(lines, line{pos, end, next})
		pos = next
	}

	var hs []Heading
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		m := masked[l.start:l.end]
		if level, ok := atxLevel(m); ok {
			hs = append(hs, Heading{
				Level: level, Text: atxText(content[l.start:l.end], level),
				Start: l.start, BodyStart: l.next,
			})
			continue
		}
		if i+1 < len(lines) && isParagraphLine(m) {
			u := lines[i+1]
			if level := setextLevel(masked[u.start:u.end]); level > 0 {
				hs = append(hs, Heading{
					Level: level, Text: strings.TrimSpace(content[l.start:l.end]),
					Start: l.start, BodyStart: u.next,
				})
				i++
			}
		}
	}

	var stack []Heading
	for i := range hs {
		for len(stack) > 0 && stack[len(stack)-1].Level >= hs[i].Level {
			stack = stack[:len(stack)-1]
		}
		path := hs[i].Text
		if len(stack) > 0 {
			path = stack[len(stack)-1].Path + " > " + path
		}
		hs[i].Path = path
		stack = append(stack, hs[i])
		hs[i].End = len(content)
		for j := i + 1; j < len(hs); j++ {
			if hs[j].Level <= hs[i].Level {
				hs[i].End = hs[j].Start
				break
			}
		}
	}
	return hs
}

// frontmatterEnd returns the offset just after the closing fence line of the
// frontmatter, or 0 when content has none. A leading BOM is transparent.
func frontmatterEnd(content string) int {
	pos := 0
	if strings.HasPrefix(content, "\uFEFF") {
		pos = len("\uFEFF")
	}
	first := true
	for pos < len(content) {
		nl := strings.IndexByte(content[pos:], '\n')
		end, next := len(content), len(content)
		if nl >= 0 {
			end, next = pos+nl, pos+nl+1
		}
		fence := strings.TrimSpace(content[pos:end]) == frontmatterDelimiter
		if first {
			if !fence {
				return 0
			}
			first = false
		} else if fence {
			return next
		}
		pos = next
	}
	return 0
}

// atxLevel reports the level of an ATX heading line (up to 3 leading spaces,
// 1-6 '#', then a space, tab or the end of the line).
func atxLevel(line string) (int, bool) {
	i := 0
	for i < len(line) && i < 4 && line[i] == ' ' {
		i++
	}
	if i > 3 {
		return 0, false
	}
	n := 0
	for i+n < len(line) && line[i+n] == '#' {
		n++
	}
	if n < 1 || n > 6 {
		return 0, false
	}
	if i+n < len(line) && line[i+n] != ' ' && line[i+n] != '\t' {
		return 0, false
	}
	return n, true
}

func atxText(line string, level int) string {
	s := strings.TrimSpace(line)
	s = strings.TrimSpace(s[level:])
	t := strings.TrimRight(s, "#")
	if t == "" {
		return ""
	}
	if len(t) < len(s) && (t[len(t)-1] == ' ' || t[len(t)-1] == '\t') {
		return strings.TrimSpace(t)
	}
	return s
}

// setextLevel returns 1 for a "===" underline, 2 for "---", 0 otherwise.
func setextLevel(line string) int {
	s := strings.TrimRight(line, " \t")
	indent := len(s) - len(strings.TrimLeft(s, " "))
	s = strings.TrimLeft(s, " ")
	if indent > 3 || s == "" {
		return 0
	}
	if strings.Trim(s, "=") == "" {
		return 1
	}
	if strings.Trim(s, "-") == "" {
		return 2
	}
	return 0
}

// isParagraphLine reports whether a (masked) line can be the text of a setext
// heading: non-blank, indented less than 4, and not itself an underline.
func isParagraphLine(line string) bool {
	if strings.TrimSpace(line) == "" || setextLevel(line) > 0 {
		return false
	}
	return len(line)-len(strings.TrimLeft(line, " ")) < 4
}

// FindSection returns the heading that query names. The query matches a
// heading's Text or Path, ignoring case and Unicode form, after trimming
// spaces and leading '#'. More than one match is an *AmbiguousSectionError;
// none is a *SectionNotFoundError.
func FindSection(content, query string) (Heading, error) {
	hs := Headings(content)
	q := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(query), "#"))
	parts := strings.Split(q, ">")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	qPath := Key(strings.Join(parts, " > "))
	qRaw := Key(q)

	var matches []Heading
	if q != "" {
		for _, h := range hs {
			if k := Key(h.Text); k == qRaw || Key(h.Path) == qPath || k == qPath {
				matches = append(matches, h)
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		var paths []string
		for _, h := range hs {
			if len(paths) == maxListedHeadings {
				break
			}
			paths = append(paths, h.Path)
		}
		return Heading{}, &SectionNotFoundError{Query: query, Headings: paths}
	}
	paths := make([]string, len(matches))
	for i, h := range matches {
		paths[i] = h.Path
	}
	return Heading{}, &AmbiguousSectionError{Query: query, Matches: paths}
}

// sectionEOL returns the line ending of the heading line, or of the first line
// of content when the heading is the last line and has none.
func sectionEOL(content string, h Heading) string {
	if strings.HasSuffix(content[:h.BodyStart], "\r\n") {
		return "\r\n"
	}
	if strings.HasSuffix(content[:h.BodyStart], "\n") {
		return "\n"
	}
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// withEOL converts every line ending in text to eol and makes sure a
// non-empty text ends with one.
func withEOL(text, eol string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if eol != "\n" {
		text = strings.ReplaceAll(text, "\n", eol)
	}
	return text
}

// insertAt puts text (already normalized) at offset at, first adding a line
// ending when the byte before at is not one.
func insertAt(content string, at int, text, eol string, replaceTo int) string {
	if text == "" {
		return content[:at] + content[replaceTo:]
	}
	sep := ""
	if at > 0 && content[at-1] != '\n' {
		sep = eol
	}
	return content[:at] + sep + text + content[replaceTo:]
}

// ReplaceSection replaces everything between the heading line and the end of
// the section (subsections included) with body. When the replaced text ended
// with a blank line before the next heading, one blank line is kept there.
func ReplaceSection(content string, h Heading, body string) string {
	eol := sectionEOL(content, h)
	text := withEOL(body, eol)
	if text != "" && h.End < len(content) && endsWithBlankLine(content[h.BodyStart:h.End]) {
		text = strings.TrimRight(text, "\r\n") + eol + eol
	}
	return insertAt(content, h.BodyStart, text, eol, h.End)
}

// AppendToSection inserts text at the end of the section, after its
// subsections and before the run of trailing blank lines, so those blank lines
// still separate the new text from the next heading.
func AppendToSection(content string, h Heading, text string) string {
	eol := sectionEOL(content, h)
	at := h.End
	for at > h.BodyStart && content[at-1] == '\n' {
		ls := strings.LastIndexByte(content[:at-1], '\n') + 1
		if ls < h.BodyStart {
			break
		}
		if l := content[ls : at-1]; l != "" && l != "\r" {
			break
		}
		at = ls
	}
	return insertAt(content, at, withEOL(text, eol), eol, at)
}

// PrependToSection inserts text right after the heading line.
func PrependToSection(content string, h Heading, text string) string {
	eol := sectionEOL(content, h)
	return insertAt(content, h.BodyStart, withEOL(text, eol), eol, h.BodyStart)
}

func endsWithBlankLine(s string) bool {
	if !strings.HasSuffix(s, "\n") {
		return false
	}
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s == "" || strings.HasSuffix(s, "\n")
}
