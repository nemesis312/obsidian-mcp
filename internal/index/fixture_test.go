package index_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/nemesis312/obsidian-mcp/internal/index"
	"github.com/nemesis312/obsidian-mcp/internal/links"
	"github.com/nemesis312/obsidian-mcp/internal/vault"
)

// copyFixture copies testdata/linkvault into a temp dir and adds the one file
// that cannot be committed: a note whose name is in NFD, because git may
// normalize file names.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "linkvault")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	nfd := filepath.Join(dst, "nfd", "Café.md")
	if err := os.MkdirAll(filepath.Dir(nfd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfd, []byte("Name stored in NFD. No links.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func buildFixture(t *testing.T) *index.Index {
	t.Helper()
	v, err := vault.New(copyFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Build(v)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

type wantEdge struct {
	from, to string
	toKind   index.NodeKind
	kind     links.Kind
	ambig    bool
}

func (e wantEdge) String() string {
	return fmt.Sprintf("%s -> %s kind=%d to=%d ambiguous=%v", e.from, e.to, e.kind, e.toKind, e.ambig)
}

// nfc normalizes paths so the test also passes on file systems that store
// names in a different Unicode form than the one that was written.
func nfc(s string) string { return norm.NFC.String(s) }

// The table is sorted by (from, toKind, to), the order Graph() returns.
// It must agree with testdata/linkvault/README.md.
var fixtureEdges = []wantEdge{
	{"Code.md", "Home.md", index.NodeNote, links.KindWikilink, false},
	{"CaseLinks.md", "Home.md", index.NodeNote, links.KindWikilink, false},
	{"CaseLinks.md", "Upper/Shout.MD", index.NodeNote, links.KindWikilink, false},
	{"CaseLinks.md", "deep/sub/Deep Note.md", index.NodeNote, links.KindWikilink, false},
	{"CaseLinks.md", "ghost target", index.NodeGhost, links.KindWikilink, false},
	{"Dups.md", "a/Dup.md", index.NodeNote, links.KindWikilink, true},
	{"Dups.md", "b/c/Dup.md", index.NodeNote, links.KindWikilink, false},
	{"Embeds.md", "Home.md", index.NodeNote, links.KindEmbed, false},
	{"Embeds.md", "assets/pic.png", index.NodeAttachment, links.KindEmbed, false},
	{"Embeds.md", "docs/doc.pdf", index.NodeAttachment, links.KindEmbed, false},
	{"Embeds.md", "missing.png", index.NodeGhost, links.KindEmbed, false},
	{"Front.md", "Home.md", index.NodeNote, links.KindWikilink, false},
	{"Front.md", "deep/sub/Deep Note.md", index.NodeNote, links.KindWikilink, false},
	{"Home.md", "deep/sub/Deep Note.md", index.NodeNote, links.KindWikilink, false},
	{"Home.md", "v1.2 notes.md", index.NodeNote, links.KindWikilink, false},
	{"Home.md", "ghost target", index.NodeGhost, links.KindWikilink, false},
	{"NFC.md", "nfd/Café.md", index.NodeNote, links.KindWikilink, false},
	{"SelfLink.md", "SelfLink.md", index.NodeNote, links.KindWikilink, false},
	{"Table.md", "deep/sub/Deep Note.md", index.NodeNote, links.KindWikilink, false},
	{"b/Linker.md", "b/Dup.md", index.NodeNote, links.KindWikilink, true},
	{"md/Forms.md", "Home.md", index.NodeNote, links.KindMarkdown, false},
	{"md/Forms.md", "a/Dup.md", index.NodeNote, links.KindMarkdown, false},
	{"md/Forms.md", "md/Peer.md", index.NodeNote, links.KindMarkdown, false},
	{"md/Forms.md", "md/Percent Note.md", index.NodeNote, links.KindMarkdown, false},
	{"md/Forms.md", "md/Spaced Note.md", index.NodeNote, links.KindMarkdown, false},
	{"md/Forms.md", "assets/pic.png", index.NodeAttachment, links.KindMarkdown, false},
	{"md/Forms.md", "../../outside", index.NodeGhost, links.KindMarkdown, false},
}

func TestFixtureEdges(t *testing.T) {
	ix := buildFixture(t)
	_, edges := ix.Graph()

	got := make([]wantEdge, 0, len(edges))
	for _, e := range edges {
		got = append(got, wantEdge{nfc(e.From), nfc(e.To), e.ToKind, e.Kind, e.Ambiguous})
	}
	want := make([]wantEdge, 0, len(fixtureEdges))
	for _, e := range fixtureEdges {
		e.from, e.to = nfc(e.from), nfc(e.to)
		want = append(want, e)
	}
	// Compare as sets keyed by the full edge so a failure names the edge.
	missing, extra := diffEdges(want, got)
	for _, e := range missing {
		t.Errorf("missing edge: %v", e)
	}
	for _, e := range extra {
		t.Errorf("unexpected edge: %v", e)
	}
	if len(edges) != len(fixtureEdges) {
		t.Errorf("edge count = %d, want %d", len(edges), len(fixtureEdges))
	}
}

func diffEdges(want, got []wantEdge) (missing, extra []wantEdge) {
	count := map[wantEdge]int{}
	for _, e := range want {
		count[e]++
	}
	for _, e := range got {
		if count[e] > 0 {
			count[e]--
		} else {
			extra = append(extra, e)
		}
	}
	for _, e := range want {
		if count[e] > 0 {
			count[e]--
			missing = append(missing, e)
		}
	}
	return missing, extra
}

func TestFixtureNodesAndOrphans(t *testing.T) {
	ix := buildFixture(t)
	nodes, _ := ix.Graph()

	var others []string
	notes := 0
	for _, n := range nodes {
		switch n.Kind {
		case index.NodeNote:
			notes++
		case index.NodeAttachment:
			others = append(others, "attachment:"+n.ID)
		case index.NodeGhost:
			others = append(others, "ghost:"+n.ID)
		}
	}
	// Ghosts are keyed by links.Key, so "Ghost Target" and "ghost TARGET"
	// share one node. Links inside code are never ghosts.
	wantOthers := []string{
		"attachment:assets/pic.png",
		"attachment:docs/doc.pdf",
		"ghost:../../outside",
		"ghost:ghost target",
		"ghost:missing.png",
	}
	if !reflect.DeepEqual(others, wantOthers) {
		t.Errorf("non-note nodes = %v, want %v", others, wantOthers)
	}
	if notes != 24 {
		t.Errorf("note nodes = %d, want 24", notes)
	}
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.ID), "not a link") {
			t.Errorf("code-block link leaked into the graph: %q", n.ID)
		}
	}

	wantOrphans := []string{"Lonely.md", "Peer.md", "README.md"}
	if got := ix.Orphans(); !reflect.DeepEqual(got, wantOrphans) {
		t.Errorf("Orphans = %v, want %v", got, wantOrphans)
	}
}
