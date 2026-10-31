package links_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []links.Ref
	}{
		{"plain wikilink", "[[Note]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[Note]]", Target: "Note"}}},
		{"alias", "[[Note|Al]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[Note|Al]]", Target: "Note", Alias: "Al"}}},
		{"heading", "[[Note#H]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[Note#H]]", Target: "Note", Heading: "H"}}},
		{"block", "[[Note#^b1]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[Note#^b1]]", Target: "Note", Block: "b1"}}},
		{"heading and alias", "[[Note#H|Al]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[Note#H|Al]]", Target: "Note", Heading: "H", Alias: "Al"}}},
		{"embed", "![[Note]]", []links.Ref{{Kind: links.KindEmbed, Raw: "![[Note]]", Target: "Note"}}},
		{"embed image", "![[img.png]]", []links.Ref{{Kind: links.KindEmbed, Raw: "![[img.png]]", Target: "img.png"}}},
		{"table escaped pipe", `| [[T4\|Alias]] |`, []links.Ref{{Kind: links.KindWikilink, Raw: `[[T4\|Alias]]`, Target: "T4", Alias: "Alias", Offset: 2}}},
		{"markdown", "[t](Note.md)", []links.Ref{{Kind: links.KindMarkdown, Raw: "[t](Note.md)", Target: "Note.md", Alias: "t"}}},
		{"markdown subdir", "[t](sub/Note.md)", []links.Ref{{Kind: links.KindMarkdown, Raw: "[t](sub/Note.md)", Target: "sub/Note.md", Alias: "t"}}},
		{"markdown percent", "[t](Note%20Name.md)", []links.Ref{{Kind: links.KindMarkdown, Raw: "[t](Note%20Name.md)", Target: "Note Name.md", Alias: "t"}}},
		{"markdown angle", "[t](<Note Name.md>)", []links.Ref{{Kind: links.KindMarkdown, Raw: "[t](<Note Name.md>)", Target: "Note Name.md", Alias: "t"}}},
		{"markdown heading", "[t](Note.md#Sec)", []links.Ref{{Kind: links.KindMarkdown, Raw: "[t](Note.md#Sec)", Target: "Note.md", Heading: "Sec", Alias: "t"}}},
		{"markdown local image", "![alt](local.png)", []links.Ref{{Kind: links.KindMarkdown, Raw: "![alt](local.png)", Target: "local.png", Alias: "alt"}}},
		{"http skipped", "[t](https://x.y)", nil},
		{"mailto skipped", "[t](mailto:a@b)", nil},
		{"anchor skipped", "[t](#local)", nil},
		{"external image skipped", "![alt](https://x/i.png)", nil},
		{"frontmatter", "---\nrelated: \"[[FM1]]\"\ntags:\n  - \"[[FM2]]\"\n---\n", []links.Ref{
			{Kind: links.KindWikilink, Raw: "[[FM1]]", Target: "FM1", Offset: 14},
			{Kind: links.KindWikilink, Raw: "[[FM2]]", Target: "FM2", Offset: 34},
		}},
		{"inline code", "`[[InCode]]`", nil},
		{"fenced code", "```\n[[InCode]]\n```\n", nil},
		{"trimmed", "[[ Note ]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[ Note ]]", Target: "Note"}}},
		{"empty", "[[]]", nil},
		{"same note heading", "[[#H]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[#H]]", Heading: "H"}}},
		{"source order", "[a](A.md) [[B]] ![[C]]", []links.Ref{
			{Kind: links.KindMarkdown, Raw: "[a](A.md)", Target: "A.md", Alias: "a"},
			{Kind: links.KindWikilink, Raw: "[[B]]", Target: "B", Offset: 10},
			{Kind: links.KindEmbed, Raw: "![[C]]", Target: "C", Offset: 16},
		}},
		{"raw from original after code", "`x` [[A]]", []links.Ref{{Kind: links.KindWikilink, Raw: "[[A]]", Target: "A", Offset: 4}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := links.Extract(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Extract(%q)\n got  %+v\n want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractRobust(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"10MB unclosed", strings.Repeat("[[", 5<<20), nil},
		{"unclosed fence", "```\n[[A]]\n[[B]]", nil},
		{"invalid utf8", "\xff[[A]]", []string{"A"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			refs := links.Extract(tc.in)
			if d := time.Since(start); d > time.Second {
				t.Fatalf("took %v", d)
			}
			var got []string
			for _, r := range refs {
				got = append(got, r.Target)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets = %v, want %v", got, tc.want)
			}
		})
	}
}
