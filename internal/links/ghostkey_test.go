package links_test

import (
	"reflect"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

func TestGhostKey(t *testing.T) {
	for in, want := range map[string]string{
		"Ghost":            "ghost",
		"Ghost.md":         "ghost",
		"./Ghost":          "ghost",
		" ./ /Ghost.MD ":   "ghost",
		"/Ghost":           "ghost",
		"a//b/../Ghost.md": "a/ghost",
		"missing.png":      "missing.png",
		"../../outside.md": "../../outside",
		"./../x/Ghost":     "../x/ghost",
		"x/../../outside":  "../outside",
		"./sub/Ghost":      "sub/ghost",
		"café":             "café",
	} {
		if got := links.GhostKey(in); got != want {
			t.Errorf("GhostKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		in     string
		key    string
		rooted bool
	}{
		{"Index", "index", false},
		{" ./ ./INDEX.md ", "./index.md", false},
		{"././index", "./index", false},
		{"/Index", "index", true},
		{"./ /a//b/../Index", "./a/index", false},
		{"../a/Index", "./../a/index", false},
		{"./../a/Index", "./../a/index", false},
		{"x/../../A", "../a", false},
		{"a/./b/../Index", "a/index", false},
		{"./a/..", "", false},
		{"/", "", true},
		{"./", "", false},
		{"", "", false},
		{"café", "café", false},
	}
	for _, tt := range tests {
		k, rooted := links.NormalizeTarget(tt.in)
		if k != tt.key || rooted != tt.rooted {
			t.Errorf("NormalizeTarget(%q) = %q, %v; want %q, %v", tt.in, k, rooted, tt.key, tt.rooted)
		}
	}
}

// "./x", "x" and "/x" resolve differently, so they never share a key.
func TestNormalizeTargetKeepsAnchors(t *testing.T) {
	type id struct {
		key    string
		rooted bool
	}
	seen := map[id]string{}
	for _, s := range []string{"Index", "./Index", "/Index", "../Index", "x/../../Index"} {
		k, rooted := links.NormalizeTarget(s)
		if prev, ok := seen[id{k, rooted}]; ok {
			t.Errorf("NormalizeTarget(%q) = NormalizeTarget(%q) = %q, %v", s, prev, k, rooted)
		}
		seen[id{k, rooted}] = s
	}
}

// Spellings with the same normalized form resolve alike, for every kind.
func TestNormalizedSpellingsResolveAlike(t *testing.T) {
	r := links.NewResolver([]string{"x/Index.md", "y/z/index.md", "x/Rel.md"}, []string{"x/pic.png"})
	groups := [][]string{
		{"Index", " INDEX", "a/../index", "a /../InDeX"},
		{"./Index", "./INDEX", "./ ./index", " ././InDeX", "./a/../index"},
		{"Index.md", "index.MD", "a/../INDEX.md"},
		{"Rel.md", "rel.MD", "y/../Rel.md"},
		{"./Rel.md", "./rel.MD", "./ y/../Rel.md"},
		{"../x/Rel.md", "./../x/rel.MD", "./ ../y/../x/Rel.md"},
		{"/x/Rel", "/./x/REL", "//x/rel"},
		{"pic.png", "PIC.png", "b/../pic.PNG"},
		{"./pic.png", "./PIC.png", "./ ./pic.PNG"},
	}
	for _, g := range groups {
		for _, kind := range []links.Kind{links.KindWikilink, links.KindEmbed, links.KindMarkdown} {
			want := r.Resolve("x/From.md", links.Ref{Kind: kind, Target: g[0]})
			wk, wr := links.NormalizeTarget(g[0])
			for _, s := range g[1:] {
				if k, rooted := links.NormalizeTarget(s); k != wk || rooted != wr {
					t.Errorf("NormalizeTarget(%q) = %q, %v; want %q, %v", s, k, rooted, wk, wr)
				}
				if got := r.Resolve("x/From.md", links.Ref{Kind: kind, Target: s}); !reflect.DeepEqual(got, want) {
					t.Errorf("kind %v: Resolve(%q) = %+v, want %+v (as %q)", kind, s, got, want, g[0])
				}
			}
		}
	}
}
