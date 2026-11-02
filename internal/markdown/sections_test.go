package markdown

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type wantHeading struct {
	Level int
	Text  string
	Path  string
}

func TestHeadings(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []wantHeading
	}{
		{"atx closing hashes", "## Title ##\nbody\n", []wantHeading{{2, "Title", "Title"}}},
		{"atx closing hashes need space", "## C#\n", []wantHeading{{2, "C#", "C#"}}},
		{"atx empty", "#\n", []wantHeading{{1, "", ""}}},
		{"atx up to 3 spaces", "   # Ok\n    # code\n", []wantHeading{{1, "Ok", "Ok"}}},
		{"seven hashes is not a heading", "####### no\n", nil},
		{"setext equals", "Title\n=====\ntext\n", []wantHeading{{1, "Title", "Title"}}},
		{"setext dashes", "Sub\n---\ntext\n", []wantHeading{{2, "Sub", "Sub"}}},
		{"dashes after blank are not a heading", "text\n\n---\nmore\n", nil},
		{"frontmatter fence is not a heading", "---\ntitle: x\n---\n# Real\n", []wantHeading{{1, "Real", "Real"}}},
		{"frontmatter content skipped", "---\n# not: heading\n---\nbody\n", nil},
		{"bom frontmatter", "\uFEFF---\ntitle: x\n---\n# Real\n", []wantHeading{{1, "Real", "Real"}}},
		{"fenced block ignored", "# A\n```\n# x\n```\n", []wantHeading{{1, "A", "A"}}},
		{"inline code ignored", "`# x`\n# B\n", []wantHeading{{1, "B", "B"}}},
		{"tag is not a heading", "#tag\n", nil},
		{"nested paths", "# Plan\n## Risks\n### Deep\n## Other\n# Next\n## Risks\n", []wantHeading{
			{1, "Plan", "Plan"}, {2, "Risks", "Plan > Risks"}, {3, "Deep", "Plan > Risks > Deep"},
			{2, "Other", "Plan > Other"}, {1, "Next", "Next"}, {2, "Risks", "Next > Risks"}}},
		{"skipped level", "# A\n### C\n", []wantHeading{{1, "A", "A"}, {3, "C", "A > C"}}},
		{"inline code kept in text", "## Use `x`\n", []wantHeading{{2, "Use `x`", "Use `x`"}}},
		{"crlf", "# A\r\ntext\r\n## B\r\n", []wantHeading{{1, "A", "A"}, {2, "B", "A > B"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []wantHeading
			for _, h := range Headings(tt.content) {
				got = append(got, wantHeading{h.Level, h.Text, h.Path})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Headings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeadingOffsets(t *testing.T) {
	content := "intro\n# A\ntext\n## B\nmore\n# C\n"
	hs := Headings(content)
	if len(hs) != 3 {
		t.Fatalf("got %d headings", len(hs))
	}
	a, b, c := hs[0], hs[1], hs[2]
	if content[a.Start:a.BodyStart] != "# A\n" || a.End != c.Start {
		t.Errorf("A offsets: %+v", a)
	}
	if content[b.Start:b.BodyStart] != "## B\n" || b.End != c.Start {
		t.Errorf("B offsets: %+v", b)
	}
	if c.End != len(content) {
		t.Errorf("C.End = %d, want %d", c.End, len(content))
	}

	setext := "Title\r\n=====\r\ntext\r\n"
	s := Headings(setext)[0]
	if s.Start != 0 || setext[s.BodyStart:] != "text\r\n" || s.Text != "Title" {
		t.Errorf("setext offsets: %+v", s)
	}

	bom := "\uFEFF---\na: b\n---\n# Real\n"
	h := Headings(bom)[0]
	if bom[h.Start:h.BodyStart] != "# Real\n" {
		t.Errorf("bom offsets: %+v", h)
	}
}

func TestFindSection(t *testing.T) {
	content := "# A\n## Notes\none\n# B\n## Notes\ntwo\n## Risks\n# Café\n"

	t.Run("by path", func(t *testing.T) {
		h, err := FindSection(content, "B > Notes")
		if err != nil || h.Path != "B > Notes" {
			t.Fatalf("got %+v, %v", h, err)
		}
	})
	t.Run("by path loose spacing", func(t *testing.T) {
		h, err := FindSection(content, "b>notes")
		if err != nil || h.Path != "B > Notes" {
			t.Fatalf("got %+v, %v", h, err)
		}
	})
	t.Run("by text case-insensitive with hashes", func(t *testing.T) {
		h, err := FindSection(content, "## risks ")
		if err != nil || h.Path != "B > Risks" {
			t.Fatalf("got %+v, %v", h, err)
		}
	})
	t.Run("nfc", func(t *testing.T) {
		h, err := FindSection(content, "CAFÉ")
		if err != nil || h.Text != "Café" {
			t.Fatalf("got %+v, %v", h, err)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		_, err := FindSection(content, "Notes")
		var amb *AmbiguousSectionError
		if !errors.As(err, &amb) {
			t.Fatalf("err = %v, want *AmbiguousSectionError", err)
		}
		if !reflect.DeepEqual(amb.Matches, []string{"A > Notes", "B > Notes"}) {
			t.Errorf("Matches = %v", amb.Matches)
		}
		if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "Parent > Child") ||
			!strings.Contains(err.Error(), "A > Notes") {
			t.Errorf("Error() = %q", err.Error())
		}
	})
	t.Run("not found", func(t *testing.T) {
		_, err := FindSection(content, "Nope")
		var nf *SectionNotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("err = %v, want *SectionNotFoundError", err)
		}
		want := []string{"A", "A > Notes", "B", "B > Notes", "B > Risks", "Café"}
		if !reflect.DeepEqual(nf.Headings, want) {
			t.Errorf("Headings = %v", nf.Headings)
		}
		if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "Nope") {
			t.Errorf("Error() = %q", err.Error())
		}
	})
	t.Run("not found lists at most 10", func(t *testing.T) {
		var sb strings.Builder
		for i := 0; i < 15; i++ {
			sb.WriteString("# H" + string(rune('a'+i)) + "\n")
		}
		_, err := FindSection(sb.String(), "zzz")
		var nf *SectionNotFoundError
		if !errors.As(err, &nf) || len(nf.Headings) != 10 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("empty query", func(t *testing.T) {
		_, err := FindSection(content, " ## ")
		var nf *SectionNotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSectionEdits(t *testing.T) {
	const (
		lf   = "intro\n# A\nold a\n## Sub\nsub\n\n# B\nold b\n"
		crlf = "intro\r\n# A\r\nold a\r\n## Sub\r\nsub\r\n\r\n# B\r\n"
	)
	find := func(t *testing.T, content, q string) Heading {
		t.Helper()
		h, err := FindSection(content, q)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	t.Run("replace includes subsections and keeps blank line", func(t *testing.T) {
		h := find(t, lf, "A")
		got := ReplaceSection(lf, h, "new")
		want := "intro\n# A\nnew\n\n# B\nold b\n"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if got[:h.BodyStart] != lf[:h.BodyStart] || !strings.HasSuffix(got, lf[h.End:]) {
			t.Error("bytes outside the range changed")
		}
	})
	t.Run("replace without original blank line adds none", func(t *testing.T) {
		in := "# A\nold\n# B\n"
		h := find(t, in, "A")
		if got := ReplaceSection(in, h, "new\n"); got != "# A\nnew\n# B\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("replace empty-body section keeps blank line", func(t *testing.T) {
		for _, c := range []struct{ in, want string }{
			{"## Notes\n\n## Next\n", "## Notes\nnew\n\n## Next\n"},
			{"## Notes\r\n\r\n## Next\r\n", "## Notes\r\nnew\r\n\r\n## Next\r\n"},
		} {
			h := find(t, c.in, "Notes")
			if got := ReplaceSection(c.in, h, "new"); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		}
	})
	t.Run("replace last section", func(t *testing.T) {
		h := find(t, lf, "B")
		got := ReplaceSection(lf, h, "x")
		if got != lf[:h.BodyStart]+"x\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("append goes after subsections and before trailing blank lines", func(t *testing.T) {
		h := find(t, lf, "A")
		got := AppendToSection(lf, h, "tail")
		want := "intro\n# A\nold a\n## Sub\nsub\ntail\n\n# B\nold b\n"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		at := h.End - len("\n")
		if got[:at] != lf[:at] || !strings.HasSuffix(got, lf[at:]) {
			t.Error("bytes outside the insertion point changed")
		}
	})
	t.Run("append skips several trailing blank lines", func(t *testing.T) {
		in := "## A\ntext\n\n\n## B\n"
		h := find(t, in, "A")
		if got := AppendToSection(in, h, "new"); got != "## A\ntext\nnew\n\n\n## B\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("append to section that is only blank lines", func(t *testing.T) {
		in := "## A\n\n## B\n"
		h := find(t, in, "A")
		if got := AppendToSection(in, h, "new"); got != "## A\nnew\n\n## B\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("append without trailing blank inserts at End", func(t *testing.T) {
		in := "## A\ntext\n## B\n"
		h := find(t, in, "A")
		if got := AppendToSection(in, h, "new"); got != "## A\ntext\nnew\n## B\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("prepend goes right after the heading", func(t *testing.T) {
		h := find(t, lf, "A")
		got := PrependToSection(lf, h, "head\n")
		want := "intro\n# A\nhead\nold a\n## Sub\nsub\n\n# B\nold b\n"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if got[:h.BodyStart] != lf[:h.BodyStart] || !strings.HasSuffix(got, lf[h.BodyStart:]) {
			t.Error("bytes outside the range changed")
		}
	})
	t.Run("append to last section without final newline", func(t *testing.T) {
		in := "# A\nlast line"
		h := find(t, in, "A")
		got := AppendToSection(in, h, "more")
		if got != "# A\nlast line\nmore\n" {
			t.Fatalf("got %q", got)
		}
		if got[:h.End] != in[:h.End] {
			t.Error("prefix changed")
		}
	})
	t.Run("append to heading at EOF without newline", func(t *testing.T) {
		in := "# A"
		h := find(t, in, "A")
		if got := AppendToSection(in, h, "x"); got != "# A\nx\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("crlf keeps crlf on inserted lines", func(t *testing.T) {
		h := find(t, crlf, "A")
		got := AppendToSection(crlf, h, "l1\nl2")
		want := "intro\r\n# A\r\nold a\r\n## Sub\r\nsub\r\nl1\r\nl2\r\n\r\n# B\r\n"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
			t.Error("bare LF in CRLF file")
		}
		got = ReplaceSection(crlf, h, "x\ny")
		if got != "intro\r\n# A\r\nx\r\ny\r\n\r\n# B\r\n" {
			t.Fatalf("replace got %q", got)
		}
		got = PrependToSection(crlf, h, "p")
		if got != "intro\r\n# A\r\np\r\nold a\r\n## Sub\r\nsub\r\n\r\n# B\r\n" {
			t.Fatalf("prepend got %q", got)
		}
	})
	t.Run("crlf text converted in lf file", func(t *testing.T) {
		in := "# A\nx\n"
		h := find(t, in, "A")
		if got := AppendToSection(in, h, "a\r\nb"); got != "# A\nx\na\nb\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("bom is preserved", func(t *testing.T) {
		in := "\uFEFF---\nk: v\n---\n# A\nold\n"
		h := find(t, in, "A")
		for name, got := range map[string]string{
			"replace": ReplaceSection(in, h, "new"),
			"append":  AppendToSection(in, h, "new"),
			"prepend": PrependToSection(in, h, "new"),
		} {
			if !strings.HasPrefix(got, "\uFEFF---\nk: v\n---\n# A\n") {
				t.Errorf("%s: prefix changed: %q", name, got)
			}
		}
	})
	t.Run("empty text leaves content unchanged", func(t *testing.T) {
		h := find(t, lf, "A")
		if AppendToSection(lf, h, "") != lf || PrependToSection(lf, h, "") != lf {
			t.Error("content changed")
		}
	})
	t.Run("setext heading edit", func(t *testing.T) {
		in := "Title\n=====\nold\n\nSub\n---\nx\n"
		h := find(t, in, "Title")
		got := ReplaceSection(in, h, "new")
		if got != "Title\n=====\nnew\n" {
			t.Fatalf("got %q", got)
		}
		h = find(t, in, "Title > Sub")
		if got := PrependToSection(in, h, "p"); got != "Title\n=====\nold\n\nSub\n---\np\nx\n" {
			t.Fatalf("got %q", got)
		}
	})
}
