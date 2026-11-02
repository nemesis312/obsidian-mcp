package links_test

import (
	"strings"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

func TestRewriteRawKeepsEverythingButTarget(t *testing.T) {
	tests := []struct {
		name    string
		content string
		target  string
		want    string
	}{
		{"wikilink", "[[Old]]", "New", "[[New]]"},
		{"heading and alias", "[[Old#H|a]]", "sub/New", "[[sub/New#H|a]]"},
		{"embed block", "![[Old#^b]]", "New", "![[New#^b]]"},
		{"table escaped pipe", `[[Old\|a]]`, "New", `[[New\|a]]`},
		{"inner spaces", "[[ Old ]]", "New", "[[New]]"},
		{"hash in inline code is not a fragment", "[[`#`Old]]", "New", "[[New]]"},
		{"markdown", "[x](Old.md)", "sub/New Name.md", "[x](sub/New%20Name.md)"},
		{"markdown angle", "[x](<sub/Old Name.md>)", "a/New Name.md", "[x](<a/New Name.md>)"},
		{"markdown percent heading", "[x](sub/Old%20Name.md#h)", "New (1).md", "[x](New%20%281%29.md#h)"},
		{"markdown image", "![alt](Old.md)", "New.md", "![alt](New.md)"},
		{"frontmatter", "---\nrelated: \"[[Old]]\"\n---\nbody\n", "New", "[[New]]"},
		{"markdown encodes set", "[x](Old.md)", "a%b<c>d#e é.md", "[x](a%25b%3Cc%3Ed%23e%20é.md)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := links.Extract(tt.content)
			if len(refs) != 1 {
				t.Fatalf("Extract(%q) = %d refs, want 1", tt.content, len(refs))
			}
			ref := refs[0]
			got := links.RewriteRaw(ref, tt.target)
			if got != tt.want {
				t.Fatalf("RewriteRaw(%q, %q) = %q, want %q", ref.Raw, tt.target, got, tt.want)
			}
			back := links.Extract(got)
			if len(back) != 1 {
				t.Fatalf("Extract(%q) = %d refs, want 1", got, len(back))
			}
			b := back[0]
			if b.Kind != ref.Kind || b.Alias != ref.Alias || b.Heading != ref.Heading || b.Block != ref.Block {
				t.Errorf("re-extracted %+v, original %+v", b, ref)
			}
			if b.Target != tt.target {
				t.Errorf("re-extracted Target = %q, want %q", b.Target, tt.target)
			}
		})
	}
}

func TestStyleOf(t *testing.T) {
	tests := []struct {
		content string
		want    links.LinkStyle
	}{
		{"[[Note]]", links.StyleName},
		{"[[Note#H]]", links.StyleName},
		{"[x](Note.md)", links.StyleName},
		{"[[a/Note]]", links.StylePath},
		{"[x](a/Note.md)", links.StylePath},
		{"[[/a/Note]]", links.StyleRooted},
		{"[x](</Note.md>)", links.StyleRooted},
		{"[[./Note]]", links.StyleRelative},
		{"[x](../a/Note.md)", links.StyleRelative},
	}
	for _, tt := range tests {
		ref := links.Extract(tt.content)[0]
		if got := links.StyleOf(ref); got != tt.want {
			t.Errorf("StyleOf(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestTargetForStyles(t *testing.T) {
	r := links.NewResolver(
		[]string{"a/Dup.md", "b/Dup.md", "b/c/Dup.md", "Note.md", "md/Peer.md"},
		[]string{"img/pic.png"},
	)
	tests := []struct {
		name    string
		from    string
		to      string
		style   links.LinkStyle
		keepExt bool
		want    string
	}{
		{"name unique", "Note.md", "md/Peer.md", links.StyleName, false, "Peer"},
		{"name keep ext", "Note.md", "md/Peer.md", links.StyleName, true, "Peer.md"},
		{"name self", "md/Peer.md", "Note.md", links.StyleName, false, "Note"},
		{"name ambiguous though it wins tie-break", "Note.md", "a/Dup.md", links.StyleName, false, "a/Dup"},
		{"name ambiguous falls back to suffix", "Note.md", "b/c/Dup.md", links.StyleName, false, "c/Dup"},
		{"name suffix keep ext", "Note.md", "b/c/Dup.md", links.StyleName, true, "c/Dup.md"},
		{"name falls back to full path", "Note.md", "b/Dup.md", links.StyleName, false, "b/Dup"},
		{"name same folder still ambiguous", "b/x.md", "b/Dup.md", links.StyleName, false, "b/Dup"},
		{"name ambiguous keep ext", "b/x.md", "b/Dup.md", links.StyleName, true, "b/Dup.md"},
		{"name attachment keeps its ext", "Note.md", "img/pic.png", links.StyleName, false, "pic.png"},
		{"path", "Note.md", "b/c/Dup.md", links.StylePath, false, "b/c/Dup"},
		{"path keep ext", "Note.md", "b/c/Dup.md", links.StylePath, true, "b/c/Dup.md"},
		{"path at root", "md/Peer.md", "Note.md", links.StylePath, false, "Note"},
		{"rooted", "Note.md", "b/c/Dup.md", links.StyleRooted, false, "/b/c/Dup"},
		{"rooted keep ext", "md/Peer.md", "Note.md", links.StyleRooted, true, "/Note.md"},
		{"relative up", "md/Peer.md", "b/c/Dup.md", links.StyleRelative, false, "../b/c/Dup"},
		{"relative up to root", "md/Peer.md", "Note.md", links.StyleRelative, true, "../Note.md"},
		{"relative down", "b/x.md", "b/c/Dup.md", links.StyleRelative, false, "./c/Dup"},
		{"relative same folder", "b/c/x.md", "b/c/Dup.md", links.StyleRelative, false, "./Dup"},
		{"relative from root", "Note.md", "md/Peer.md", links.StyleRelative, false, "./md/Peer"},
		{"relative two up", "b/c/x.md", "a/Dup.md", links.StyleRelative, false, "../../a/Dup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := links.TargetFor(r, tt.from, tt.to, tt.style, tt.keepExt)
			if got != tt.want {
				t.Fatalf("TargetFor(%q -> %q) = %q, want %q", tt.from, tt.to, got, tt.want)
			}
			res := r.Resolve(tt.from, links.Ref{Kind: links.KindWikilink, Target: got})
			if res.Path != tt.to || res.Status != links.Resolved {
				t.Errorf("%q from %q resolves to %+v, want %q", got, tt.from, res, tt.to)
			}
		})
	}
}

func TestApplyEditsVerifiesOld(t *testing.T) {
	content := "see [[Old]] and [x](Old.md) end"
	ok := []links.Edit{
		{Offset: 4, Old: "[[Old]]", New: "[[New]]"},
		{Offset: 16, Old: "[x](Old.md)", New: "[x](a/New.md)"},
	}
	got, err := links.ApplyEdits(content, ok)
	if err != nil {
		t.Fatalf("ApplyEdits: %v", err)
	}
	if want := "see [[New]] and [x](a/New.md) end"; got != want {
		t.Fatalf("ApplyEdits = %q, want %q", got, want)
	}

	if got, err := links.ApplyEdits(content, nil); err != nil || got != content {
		t.Errorf("no edits = %q, %v; want content unchanged", got, err)
	}

	bad := map[string][]links.Edit{
		"mismatched old": {{Offset: 4, Old: "[[Odd]]", New: "x"}},
		"overlapping": {
			{Offset: 4, Old: "[[Old]]", New: "x"},
			{Offset: 6, Old: "Old", New: "y"},
		},
		"out of range":    {{Offset: len(content) - 2, Old: "end", New: "x"}},
		"negative offset": {{Offset: -1, Old: "", New: "x"}},
	}
	for name, edits := range bad {
		if got, err := links.ApplyEdits(content, edits); err == nil {
			t.Errorf("%s: ApplyEdits = %q, want error", name, got)
		}
	}
}

func TestApplyEditsUnsortedInput(t *testing.T) {
	content := "[[A]] [[B]]"
	got, err := links.ApplyEdits(content, []links.Edit{
		{Offset: 6, Old: "[[B]]", New: "[[BB]]"},
		{Offset: 0, Old: "[[A]]", New: "[[AA]]"},
	})
	if err != nil || got != "[[AA]] [[BB]]" {
		t.Fatalf("ApplyEdits = %q, %v", got, err)
	}
}

func TestApplyEditsCRLF(t *testing.T) {
	content := "---\r\nrel: \"[[Old]]\"\r\n---\r\n\r\nLine [[Old#H]]\r\nend\r\n"
	var edits []links.Edit
	for _, ref := range links.Extract(content) {
		edits = append(edits, links.Edit{Offset: ref.Offset, Old: ref.Raw, New: links.RewriteRaw(ref, "dir/New")})
	}
	got, err := links.ApplyEdits(content, edits)
	if err != nil {
		t.Fatalf("ApplyEdits: %v", err)
	}
	want := "---\r\nrel: \"[[dir/New]]\"\r\n---\r\n\r\nLine [[dir/New#H]]\r\nend\r\n"
	if got != want {
		t.Fatalf("ApplyEdits = %q, want %q", got, want)
	}
	if strings.Count(got, "\r\n") != strings.Count(content, "\r\n") {
		t.Errorf("CRLF count changed")
	}
}

// fuzzTarget reports whether t is a normalized target TargetFor could
// produce: only [A-Za-z0-9 /._-], and every segment non-empty, not "." or
// "..", with no leading or trailing space.
func fuzzTarget(t string) bool {
	if t == "" {
		return false
	}
	for _, c := range t {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == ' ' || c == '/' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	for _, seg := range strings.Split(t, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.TrimSpace(seg) != seg {
			return false
		}
	}
	return true
}

func FuzzRewriteRaw(f *testing.F) {
	seeds := []string{
		"[[Old]]", "[[Old#H|a]]", "![[Old#^b]]", `[[Old\|a]]`, "[[ Old ]]", "[[#H]]",
		"[x](Old.md)", "[x](<sub/Old Name.md>)", "[x](sub/Old%20Name.md#h)", "![alt](Old.md)",
		"[x](#h)", "---\nrelated: \"[[Old]]\"\n---\n",
	}
	for _, s := range seeds {
		f.Add(s, "sub/New Name.md")
	}
	f.Fuzz(func(t *testing.T, content, target string) {
		if !fuzzTarget(target) {
			t.Skip()
		}
		for _, ref := range links.Extract(content) {
			if alone := links.Extract(ref.Raw); len(alone) != 1 {
				continue // the link only reads this way in its context
			}
			out := links.RewriteRaw(ref, target)
			back := links.Extract(out)
			if len(back) != 1 {
				t.Fatalf("RewriteRaw(%q, %q) = %q: %d refs, want 1", ref.Raw, target, out, len(back))
			}
			b := back[0]
			if b.Target != target {
				t.Fatalf("RewriteRaw(%q, %q) = %q: Target %q", ref.Raw, target, out, b.Target)
			}
			if b.Kind != ref.Kind || b.Alias != ref.Alias || b.Heading != ref.Heading || b.Block != ref.Block {
				t.Fatalf("RewriteRaw(%q, %q) = %q: got %+v, original %+v", ref.Raw, target, out, b, ref)
			}
		}
	})
}
