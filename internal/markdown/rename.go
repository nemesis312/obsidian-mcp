package markdown

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// tagToken finds inline tags: a '#' not preceded by a word character, '/', '#',
// '&' or '-' (so URL fragments, HTML entities and "a#b" are ignored), followed
// by tag characters. Headings ("# a") never match because a space follows '#'.
// The tag ends where tag characters stop, so punctuation after a tag and
// consecutive tags both work (nothing after the tag is consumed).
var tagToken = regexp.MustCompile(`(^|[^\p{L}\p{N}_/#&-])#([\p{L}\p{N}_/-]+)`)

// listToken splits a scalar frontmatter value ("a, b c") into tag tokens.
var listToken = regexp.MustCompile(`[^,\s]+`)

// NormalizeTagName validates a tag name given to rename_tag: it strips one
// leading '#', and rejects empty names, whitespace, any other '#', and
// characters that cannot be part of an Obsidian tag.
func NormalizeTagName(name string) (string, error) {
	name = strings.TrimPrefix(name, "#")
	if name == "" {
		return "", fmt.Errorf("tag must not be empty")
	}
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			return "", fmt.Errorf("tag %q must not contain whitespace", name)
		case r == '#':
			return "", fmt.Errorf("tag %q must not contain '#' (other than one leading '#')", name)
		case !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/'):
			return "", fmt.Errorf("tag %q contains invalid character %q", name, r)
		}
	}
	return name, nil
}

// TagRenamer renames one tag (and its nested children) in note content.
// Obsidian semantics: renaming "a" also renames "a/b" to "new/b".
type TagRenamer struct {
	old, new string
}

// NewTagRenamer returns a renamer; both names must already be normalized.
func NewTagRenamer(oldTag, newTag string) *TagRenamer {
	return &TagRenamer{old: oldTag, new: newTag}
}

// rename maps a bare tag name to its new name.
func (r *TagRenamer) rename(tag string) (string, bool) {
	if tag == r.old {
		return r.new, true
	}
	if strings.HasPrefix(tag, r.old+"/") {
		return r.new + tag[len(r.old):], true
	}
	return tag, false
}

// RenameTag renames inline tags in content (convenience wrapper).
func RenameTag(content, oldTag, newTag string) string {
	return NewTagRenamer(oldTag, newTag).RenameInline(content)
}

// RenameInline renames inline tags in markdown text, skipping fenced code
// blocks and inline code spans. Line endings and all other text are preserved.
func (r *TagRenamer) RenameInline(content string) string {
	lines := strings.Split(content, "\n")
	var fenceChar byte
	var fenceLen int
	for i, line := range lines {
		stripped := strings.TrimLeft(line, " ")
		indent := len(line) - len(stripped)
		trimmed := strings.TrimRight(stripped, "\r")
		if fenceLen > 0 {
			if indent < 4 && isClosingFence(trimmed, fenceChar, fenceLen) {
				fenceLen = 0
			}
			continue
		}
		if c, n := openingFence(trimmed); indent < 4 && n >= 3 {
			fenceChar, fenceLen = c, n
			continue
		}
		lines[i] = r.renameOutsideInlineCode(line)
	}
	return strings.Join(lines, "\n")
}

func openingFence(s string) (byte, int) {
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0
	}
	c := s[0]
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if c == '`' && strings.Contains(s[n:], "`") {
		return 0, 0 // backtick info strings cannot contain backticks: inline code
	}
	return c, n
}

func isClosingFence(s string, c byte, minLen int) bool {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n >= minLen && strings.TrimSpace(s[n:]) == ""
}

func (r *TagRenamer) renameOutsideInlineCode(line string) string {
	if !strings.Contains(line, "#") {
		return line
	}
	var out strings.Builder
	start := 0 // start of current non-code chunk
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		n := j - i
		// find a closing run of exactly n backticks
		closeAt := -1
		for k := j; k < len(line); {
			if line[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(line) && line[m] == '`' {
				m++
			}
			if m-k == n {
				closeAt = m
				break
			}
			k = m
		}
		if closeAt < 0 {
			i = j // unmatched run is literal text
			continue
		}
		out.WriteString(r.renameText(line[start:i]))
		out.WriteString(line[i:closeAt])
		start, i = closeAt, closeAt
	}
	out.WriteString(r.renameText(line[start:]))
	return out.String()
}

func (r *TagRenamer) renameText(text string) string {
	if !strings.Contains(text, "#") {
		return text
	}
	matches := tagToken.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		return text
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		tagStart, tagEnd := m[4], m[5]
		if nt, ok := r.rename(text[tagStart:tagEnd]); ok {
			out.WriteString(text[last:tagStart])
			out.WriteString(nt)
			last = tagEnd
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

// RenameNote renames the tag in a whole note: frontmatter "tags"/"tag" keys
// (only if present and non-null, keeping which key was used) and inline tags in
// the body. It returns the new content and whether anything changed. Text that
// is not modified is preserved byte for byte.
func (r *TagRenamer) RenameNote(content string) (string, bool, error) {
	lines := strings.Split(content, "\n")

	if strings.TrimSpace(lines[0]) != frontmatterDelimiter {
		out := r.RenameInline(content)
		return out, out != content, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontmatterDelimiter {
			end = i
			break
		}
	}
	if end == -1 {
		return content, false, fmt.Errorf("unclosed frontmatter delimiter")
	}

	fmLines := lines[1:end]
	newFM, fmChanged, err := r.renameFrontmatter(strings.Join(fmLines, "\n"))
	if err != nil {
		return content, false, err
	}

	body := strings.Join(lines[end+1:], "\n")
	newBody := r.RenameInline(body)
	if !fmChanged && newBody == body {
		return content, false, nil
	}

	var b strings.Builder
	b.WriteString(lines[0])
	b.WriteString("\n")
	if fmChanged {
		b.WriteString(newFM)
	} else {
		b.WriteString(strings.Join(fmLines, "\n"))
	}
	if len(fmLines) > 0 || fmChanged {
		b.WriteString("\n")
	}
	b.WriteString(lines[end])
	if end+1 < len(lines) {
		b.WriteString("\n")
		b.WriteString(newBody)
	}
	return b.String(), true, nil
}

// renameFrontmatter edits only the tags/tag values inside the YAML node tree so
// order, comments and every other key survive. Nothing is added or merged.
func (r *TagRenamer) renameFrontmatter(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, false, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, false, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return raw, false, nil
	}
	changed := false
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if key.Value != "tags" && key.Value != "tag" {
			continue
		}
		if r.renameValueNode(val) {
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return raw, false, fmt.Errorf("failed to encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return raw, false, fmt.Errorf("failed to encode frontmatter: %w", err)
	}
	return strings.TrimRight(sb.String(), "\n"), true, nil
}

func (r *TagRenamer) renameValueNode(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag != "!!str" {
			return false // null, numbers, bools
		}
		nv := listToken.ReplaceAllStringFunc(n.Value, func(tok string) string {
			hash := strings.HasPrefix(tok, "#")
			name := strings.TrimPrefix(tok, "#")
			if nt, ok := r.rename(name); ok {
				if hash {
					return "#" + nt
				}
				return nt
			}
			return tok
		})
		if nv != n.Value {
			n.Value = nv
			return true
		}
	case yaml.SequenceNode:
		changed := false
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && r.renameValueNode(c) {
				changed = true
			}
		}
		return changed
	}
	return false
}
