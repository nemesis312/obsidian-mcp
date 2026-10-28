package markdown_test

import (
	"strings"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

func TestRenameTag_Body(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"consecutive", "#a #a", "#new #new"},
		{"comma", "x #a, y", "x #new, y"},
		{"period", "end #a.", "end #new."},
		{"paren", "(#a)", "(#new)"},
		{"not prefix", "#ab #a-b #a_b", "#ab #a-b #a_b"},
		{"heading", "# a\n## a\n#a", "# a\n## a\n#new"},
		{"nested", "#a/b and #a/b/c and #ab/c", "#new/b and #new/b/c and #ab/c"},
		{"suffix of other", "#x/a #xa", "#x/a #xa"},
		{"url fragment", "http://x/#a", "http://x/#a"},
		{"start of line", "#a\n#a", "#new\n#new"},
		{"inline code", "`#a` and #a", "`#a` and #new"},
		{"double backtick", "``#a`b`` #a", "``#a`b`` #new"},
		{"unclosed backtick", "` #a", "` #new"},
		{"fenced", "#a\n```go\n#a\n```\n#a", "#new\n```go\n#a\n```\n#new"},
		{"tilde fence", "~~~\n#a\n~~~\n#a", "~~~\n#a\n~~~\n#new"},
		{"fence longer close", "````\n```\n#a\n````\n#a", "````\n```\n#a\n````\n#new"},
		{"crlf", "#a\r\n#a\r\n", "#new\r\n#new\r\n"},
		{"unicode", "#año #a", "#año #new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := markdown.RenameTag(c.in, "a", "new"); got != c.want {
				t.Errorf("RenameTag(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRenameTagsInNote_Frontmatter(t *testing.T) {
	r := markdown.NewTagRenamer("a", "new")

	t.Run("list tags nested and body", func(t *testing.T) {
		in := "---\ntitle: T\ntags:\n  - a\n  - a/b\n  - other\n---\nBody #a here\n"
		got, changed, err := r.RenameNote(in)
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		for _, want := range []string{"- new\n", "- new/b\n", "- other\n", "title: T", "Body #new here\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in %q", want, got)
			}
		}
		if strings.Contains(got, "null") {
			t.Errorf("must not write null: %q", got)
		}
	})

	t.Run("tag key preserved, no merge into tags", func(t *testing.T) {
		in := "---\ntag: a\n---\nbody"
		got, changed, err := r.RenameNote(in)
		if err != nil || !changed {
			t.Fatal(changed, err)
		}
		if !strings.Contains(got, "tag: new") || strings.Contains(got, "tags") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("no tags key is not added", func(t *testing.T) {
		in := "---\ntitle: T\n---\nBody #a\n"
		got, changed, err := r.RenameNote(in)
		if err != nil || !changed {
			t.Fatal(changed, err)
		}
		if strings.Contains(got, "tags") || strings.Contains(got, "null") {
			t.Errorf("got %q", got)
		}
		if got != "---\ntitle: T\n---\nBody #new\n" {
			t.Errorf("frontmatter must be untouched, got %q", got)
		}
	})

	t.Run("tags null stays untouched", func(t *testing.T) {
		in := "---\ntags:\n---\nbody"
		got, changed, err := r.RenameNote(in)
		if err != nil || changed || got != in {
			t.Errorf("got=%q changed=%v err=%v", got, changed, err)
		}
	})

	t.Run("flow list and hash prefix and csv string", func(t *testing.T) {
		got, changed, _ := r.RenameNote("---\ntags: [a, b, \"#a\"]\n---\nx")
		if !changed || !strings.Contains(got, "new") || strings.Contains(got, "[a") {
			t.Errorf("flow: %q", got)
		}
		got, changed, _ = r.RenameNote("---\ntags: a, b, a/c\n---\nx")
		if !changed || !strings.Contains(got, "tags: new, b, new/c") {
			t.Errorf("csv: %q", got)
		}
	})

	t.Run("body exact when only frontmatter changes", func(t *testing.T) {
		body := "\n\n  weird   spacing\r\n\ttabs\n\n\n"
		got, _, err := r.RenameNote("---\ntags: [a]\n---\n" + body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, "---\n"+body) {
			t.Errorf("body altered: %q", got)
		}
	})

	t.Run("no change", func(t *testing.T) {
		in := "---\ntags: [x]\n---\n#ab"
		got, changed, err := r.RenameNote(in)
		if err != nil || changed || got != in {
			t.Errorf("got=%q changed=%v err=%v", got, changed, err)
		}
	})

	t.Run("no frontmatter", func(t *testing.T) {
		got, changed, err := r.RenameNote("#a text")
		if err != nil || !changed || got != "#new text" {
			t.Errorf("got=%q changed=%v err=%v", got, changed, err)
		}
	})

	t.Run("invalid yaml errors", func(t *testing.T) {
		if _, _, err := r.RenameNote("---\ntags: [a\n---\nx"); err == nil {
			t.Error("expected error")
		}
		if _, _, err := r.RenameNote("---\ntags: [a]\nno close"); err == nil {
			t.Error("expected error for unclosed frontmatter")
		}
	})

	t.Run("hash in frontmatter comment/other keys untouched", func(t *testing.T) {
		in := "---\ntitle: \"#a\"\n---\nx"
		got, changed, _ := r.RenameNote(in)
		if changed || got != in {
			t.Errorf("got=%q", got)
		}
	})
}

func TestNormalizeTagName(t *testing.T) {
	ok := map[string]string{"foo": "foo", "#foo": "foo", "a/b": "a/b", "año": "año"}
	for in, want := range ok {
		got, err := markdown.NormalizeTagName(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "#", " ", "a b", " a", "a\tb", "a#b", "##a", "a,b", "a.b"} {
		if _, err := markdown.NormalizeTagName(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}
