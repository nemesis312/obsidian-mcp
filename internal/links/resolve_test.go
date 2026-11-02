package links_test

import (
	"reflect"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

func newTestResolver() *links.Resolver {
	return links.NewResolver(
		[]string{
			"Note.md", "sub/dir/Note2.md", "a/Dup.md", "b/c/Dup.md", "CaseTarget.md",
			"café.md", "v1.2 notes.md", "x/Rel.md", "x/y/Deep.md", "x/Note.md",
			"s/t/Twin.md", "q/Twin.md",
		},
		[]string{"assets/img.png", "p/dup.png", "q/dup.png", "doc.pdf"},
	)
}

func TestResolve(t *testing.T) {
	wiki := func(target string) links.Ref { return links.Ref{Kind: links.KindWikilink, Target: target} }
	embed := func(target string) links.Ref { return links.Ref{Kind: links.KindEmbed, Target: target} }
	md := func(target string) links.Ref { return links.Ref{Kind: links.KindMarkdown, Target: target} }

	tests := []struct {
		name string
		from string
		ref  links.Ref
		want links.Resolution
	}{
		// Rule 1: an empty target is the source note, for every kind.
		{"heading only", "Note.md", wiki(""), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"markdown heading only", "x/Other.md", md(""), links.Resolution{Status: links.Resolved, Path: "x/Other.md"}},

		// Rule 2: "./" and "../" are relative to the source folder, with no fallback.
		{"dot slash at root", "z.md", wiki("./Note"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"dot slash in folder", "x/Other.md", wiki("./Note"), links.Resolution{Status: links.Resolved, Path: "x/Note.md"}},
		{"dot slash markdown", "x/Other.md", md("./Note.md"), links.Resolution{Status: links.Resolved, Path: "x/Note.md"}},
		{"dot slash no fallback", "sub/Other.md", wiki("./Note"), links.Resolution{Status: links.Unresolved}},
		{"dot slash no basename fallback", "z.md", md("./Rel.md"), links.Resolution{Status: links.Unresolved}},
		{"dot slash case", "x/Other.md", wiki("./REL"), links.Resolution{Status: links.Resolved, Path: "x/Rel.md"}},
		{"dot dot", "x/y/Other.md", md("../Rel.md"), links.Resolution{Status: links.Resolved, Path: "x/Rel.md"}},
		{"dot dot wikilink", "x/y/Other.md", wiki("../Rel"), links.Resolution{Status: links.Resolved, Path: "x/Rel.md"}},
		{"dot dot no fallback", "x/y/Other.md", wiki("../Note2"), links.Resolution{Status: links.Unresolved}},
		{"dot slash attachment", "assets/N.md", embed("./img.png"), links.Resolution{Status: links.Resolved, Path: "assets/img.png", Attachment: true}},
		{"markdown escape", "Note.md", md("../Note.md"), links.Resolution{Status: links.Unresolved}},
		{"escape", "Note.md", wiki("../outside"), links.Resolution{Status: links.Unresolved}},
		{"escape existing basename", "Note.md", wiki("../Note"), links.Resolution{Status: links.Unresolved}},
		{"escape bare", "Note.md", wiki(".."), links.Resolution{Status: links.Unresolved}},
		{"escape after cleaning", "x/Other.md", wiki("a/../../Note"), links.Resolution{Status: links.Unresolved}},

		// Rule 3: a leading "/" is an exact path from the vault root.
		{"leading slash is vault root", "x/Other.md", wiki("/Note"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"leading slash markdown", "x/Other.md", md("/Note.md"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"leading slash no basename", "Note.md", wiki("/Note2"), links.Resolution{Status: links.Unresolved}},
		{"leading slash no suffix", "Note.md", wiki("/c/Dup"), links.Resolution{Status: links.Unresolved}},
		{"only slash", "Note.md", wiki("/"), links.Resolution{Status: links.Unresolved}},

		// Rule 4: an exact path from the vault root wins.
		{"path match", "Note.md", wiki("b/c/Dup"), links.Resolution{Status: links.Resolved, Path: "b/c/Dup.md"}},
		{"path match case", "Note.md", wiki("B/C/dup.MD"), links.Resolution{Status: links.Resolved, Path: "b/c/Dup.md"}},
		{"clean inside", "Note.md", wiki("a/../b/c/Dup"), links.Resolution{Status: links.Resolved, Path: "b/c/Dup.md"}},
		{"root note beats same folder", "x/Other.md", wiki("Note"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"root note beats same folder markdown", "x/Other.md", md("Note.md"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"case", "Note.md", wiki("casetarget"), links.Resolution{Status: links.Resolved, Path: "CaseTarget.md"}},
		{"nfd", "Note.md", wiki("café"), links.Resolution{Status: links.Resolved, Path: "café.md"}},
		{"dotted name", "Note.md", wiki("v1.2 notes"), links.Resolution{Status: links.Resolved, Path: "v1.2 notes.md"}},
		{"dotted name with md", "Note.md", wiki("v1.2 notes.md"), links.Resolution{Status: links.Resolved, Path: "v1.2 notes.md"}},
		{"spaces", "z.md", wiki(" Note "), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"md ext", "z.md", wiki("Note.md"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"MD ext", "z.md", wiki("Note.MD"), links.Resolution{Status: links.Resolved, Path: "Note.md"}},
		{"markdown path attachment", "Note.md", md("assets/img.png"), links.Resolution{Status: links.Resolved, Path: "assets/img.png", Attachment: true}},
		{"attachment path", "Note.md", embed("assets/img.png"), links.Resolution{Status: links.Resolved, Path: "assets/img.png", Attachment: true}},

		// Rule 5: path-suffix match, then same folder, fewest segments, Key.
		{"basename", "Note.md", wiki("Note2"), links.Resolution{Status: links.Resolved, Path: "sub/dir/Note2.md"}},
		{"markdown basename", "y.md", md("Rel.md"), links.Resolution{Status: links.Resolved, Path: "x/Rel.md"}},
		{"path suffix", "Note.md", wiki("c/Dup"), links.Resolution{Status: links.Resolved, Path: "b/c/Dup.md"}},
		{"path suffix markdown", "Note.md", md("c/Dup.md"), links.Resolution{Status: links.Resolved, Path: "b/c/Dup.md"}},
		{"path suffix two segments", "Note.md", wiki("SUB/dir/note2"), links.Resolution{Status: links.Resolved, Path: "sub/dir/Note2.md"}},
		{"path suffix one segment", "Note.md", wiki("dir/Note2"), links.Resolution{Status: links.Resolved, Path: "sub/dir/Note2.md"}},
		{"path suffix attachment", "Note.md", embed("Assets/img.png"), links.Resolution{Status: links.Resolved, Path: "assets/img.png", Attachment: true}},
		{"ambiguous same folder", "a/Other.md", wiki("Dup"), links.Resolution{Status: links.Ambiguous, Path: "a/Dup.md", Candidates: []string{"a/Dup.md", "b/c/Dup.md"}}},
		{"ambiguous same folder wins over fewer segments", "b/c/Other.md", wiki("Dup"), links.Resolution{Status: links.Ambiguous, Path: "b/c/Dup.md", Candidates: []string{"b/c/Dup.md", "a/Dup.md"}}},
		{"ambiguous fewer segments", "z.md", wiki("Dup"), links.Resolution{Status: links.Ambiguous, Path: "a/Dup.md", Candidates: []string{"a/Dup.md", "b/c/Dup.md"}}},
		{"twin same folder", "s/t/P.md", wiki("Twin"), links.Resolution{Status: links.Ambiguous, Path: "s/t/Twin.md", Candidates: []string{"s/t/Twin.md", "q/Twin.md"}}},
		{"twin same folder markdown", "s/t/P.md", md("Twin.md"), links.Resolution{Status: links.Ambiguous, Path: "s/t/Twin.md", Candidates: []string{"s/t/Twin.md", "q/Twin.md"}}},
		{"twin fewer segments", "P.md", wiki("Twin"), links.Resolution{Status: links.Ambiguous, Path: "q/Twin.md", Candidates: []string{"q/Twin.md", "s/t/Twin.md"}}},
		{"embed attachment", "Note.md", embed("img.png"), links.Resolution{Status: links.Resolved, Path: "assets/img.png", Attachment: true}},
		{"attachment case", "Note.md", embed("DOC.PDF"), links.Resolution{Status: links.Resolved, Path: "doc.pdf", Attachment: true}},
		{"attachment ambiguous", "q/Note.md", embed("dup.png"), links.Resolution{Status: links.Ambiguous, Path: "q/dup.png", Candidates: []string{"q/dup.png", "p/dup.png"}, Attachment: true}},

		// No match.
		{"missing attachment", "Note.md", embed("nope.png"), links.Resolution{Status: links.Unresolved}},
		{"ghost", "Note.md", wiki("Ghost"), links.Resolution{Status: links.Unresolved}},
		{"suffix not a whole segment", "Note.md", wiki("ub/dir/Note2"), links.Resolution{Status: links.Unresolved}},
	}
	r := newTestResolver()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.from, tt.ref)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Resolve(%q, %q)\n got %+v\nwant %+v", tt.from, tt.ref.Target, got, tt.want)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	r := links.NewResolver([]string{"Notes/A.md", "café.md"}, []string{"Assets/Pic.PNG"})
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"notes/a", "Notes/A.md", true},
		{"NOTES/A.MD", "Notes/A.md", true},
		{"café", "café.md", true},
		{"assets/pic.png", "Assets/Pic.PNG", true},
		{"missing", "", false},
		{"A", "", false},
	}
	for _, tt := range tests {
		got, ok := r.Lookup(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Lookup(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestKey(t *testing.T) {
	if links.Key("Café") != "café" {
		t.Fatalf("Key must NFC-normalize and lowercase, got %q", links.Key("Café"))
	}
}

// Case-only duplicates (same Key, different spelling) are never silently
// picked: path and basename matches are Ambiguous, with every spelling as a
// candidate and the lexicographically smallest as Path. Lookup still returns
// the smallest, deterministically.
func TestResolveCaseOnlyDuplicates(t *testing.T) {
	r := links.NewResolver([]string{"k/Same.md", "K/SAME.md", "k/same.md", "m/Q.md", "M/q.md", "solo.md"}, []string{"i/A.PNG", "I/a.png"})
	same := []string{"K/SAME.md", "k/Same.md", "k/same.md"}
	for _, tc := range []struct {
		from, target string
		kind         links.Kind
		want         links.Resolution
	}{
		{"z.md", "Same", links.KindWikilink, links.Resolution{Status: links.Ambiguous, Path: "K/SAME.md", Candidates: same}},
		{"z.md", "k/same", links.KindWikilink, links.Resolution{Status: links.Ambiguous, Path: "K/SAME.md", Candidates: same}},
		// "K/" is another folder than "k/": the same-folder tie-break compares raw folders.
		{"k/x.md", "same.md", links.KindMarkdown, links.Resolution{Status: links.Ambiguous, Path: "k/Same.md", Candidates: []string{"k/Same.md", "k/same.md", "K/SAME.md"}}},
		{"z.md", "q", links.KindWikilink, links.Resolution{Status: links.Ambiguous, Path: "M/q.md", Candidates: []string{"M/q.md", "m/Q.md"}}},
		{"z.md", "SOLO", links.KindWikilink, links.Resolution{Status: links.Resolved, Path: "solo.md"}},
		{"z.md", "a.png", links.KindEmbed, links.Resolution{Status: links.Ambiguous, Path: "I/a.png", Candidates: []string{"I/a.png", "i/A.PNG"}, Attachment: true}},
		{"z.md", "i/a.png", links.KindEmbed, links.Resolution{Status: links.Ambiguous, Path: "I/a.png", Candidates: []string{"I/a.png", "i/A.PNG"}, Attachment: true}},
	} {
		got := r.Resolve(tc.from, links.Ref{Kind: tc.kind, Target: tc.target})
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%q, %q) = %+v, want %+v", tc.from, tc.target, got, tc.want)
		}
	}
	for arg, want := range map[string]string{"k/same": "K/SAME.md", "K/same.md": "K/SAME.md", "m/q": "M/q.md", "i/a.png": "I/a.png"} {
		if got, ok := r.Lookup(arg); !ok || got != want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", arg, got, ok, want)
		}
	}
}

// The same-folder tie-break compares folders as written: "K/" and "k/" are
// different folders, so neither is the source note's folder for the other.
func TestResolveSameFolderIsCaseSensitive(t *testing.T) {
	r := links.NewResolver([]string{"K/Dup.md", "a/Dup.md"}, nil)
	for _, tc := range []struct {
		from string
		want links.Resolution
	}{
		{"k/x.md", links.Resolution{Status: links.Ambiguous, Path: "a/Dup.md", Candidates: []string{"a/Dup.md", "K/Dup.md"}}},
		{"K/x.md", links.Resolution{Status: links.Ambiguous, Path: "K/Dup.md", Candidates: []string{"K/Dup.md", "a/Dup.md"}}},
	} {
		if got := r.Resolve(tc.from, links.Ref{Kind: links.KindWikilink, Target: "Dup"}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%q, Dup) = %+v, want %+v", tc.from, got, tc.want)
		}
	}
}

// Path-suffix matching compares whole segments, and several suffix matches
// are Ambiguous with the usual tie-break.
func TestResolvePathSuffix(t *testing.T) {
	r := links.NewResolver([]string{"b/c/Dup.md", "d/e/c/Dup.md", "xc/Dup.md", "c/Dup.md", "Dup.md"}, nil)
	for _, tc := range []struct {
		from, target string
		want         links.Resolution
	}{
		// "c/Dup" is an exact root path: it wins.
		{"z.md", "c/Dup", links.Resolution{Status: links.Resolved, Path: "c/Dup.md"}},
		// "e/c/Dup" matches one file only.
		{"z.md", "e/c/Dup", links.Resolution{Status: links.Resolved, Path: "d/e/c/Dup.md"}},
		// "Dup" is a root path too, so the same folder does not win.
		{"b/c/x.md", "Dup", links.Resolution{Status: links.Resolved, Path: "Dup.md"}},
	} {
		if got := r.Resolve(tc.from, links.Ref{Kind: links.KindWikilink, Target: tc.target}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%q, %q) = %+v, want %+v", tc.from, tc.target, got, tc.want)
		}
	}

	// No exact path: every whole-segment suffix match is a candidate;
	// "xc/Dup.md" never matches "c/Dup".
	r = links.NewResolver([]string{"b/c/Dup.md", "d/e/c/Dup.md", "xc/Dup.md", "f/c/Dup.md"}, nil)
	for _, tc := range []struct {
		from string
		want links.Resolution
	}{
		{"z.md", links.Resolution{Status: links.Ambiguous, Path: "b/c/Dup.md", Candidates: []string{"b/c/Dup.md", "f/c/Dup.md", "d/e/c/Dup.md"}}},
		{"d/e/c/x.md", links.Resolution{Status: links.Ambiguous, Path: "d/e/c/Dup.md", Candidates: []string{"d/e/c/Dup.md", "b/c/Dup.md", "f/c/Dup.md"}}},
	} {
		if got := r.Resolve(tc.from, links.Ref{Kind: links.KindMarkdown, Target: "c/Dup.md"}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Resolve(%q, c/Dup.md) = %+v, want %+v", tc.from, got, tc.want)
		}
	}
}
