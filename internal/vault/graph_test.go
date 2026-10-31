package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/index"
)

func writeRaw(t *testing.T, v *Vault, rel, content string) {
	t.Helper()
	p := filepath.Join(v.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func froms(edges []index.Edge) []string {
	out := []string{}
	for _, e := range edges {
		out = append(out, e.From)
	}
	sort.Strings(out)
	return out
}

func TestVaultGraphAgainstDisk(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "see [[B]]")
	writeRaw(t, v, "sub/B.md", "body")

	for _, arg := range []string{"sub/B.md", filepath.FromSlash("sub/B.md"), "SUB/b", `sub\B.md`, "./sub/B.md"} {
		bl, err := v.GetBacklinks(arg)
		if err != nil {
			t.Fatalf("GetBacklinks(%q): %v", arg, err)
		}
		if got := froms(bl); len(got) != 1 || got[0] != "A.md" {
			t.Errorf("GetBacklinks(%q) froms = %v, want [A.md]", arg, got)
		}
	}

	out, err := v.GetOutgoingLinks("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].To != "sub/B.md" || out[0].ToKind != index.NodeNote {
		t.Fatalf("GetOutgoingLinks(A.md) = %+v, want one note edge to sub/B.md", out)
	}
}

func TestVaultExternalEditVisible(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "see [[B]]")
	writeRaw(t, v, "sub/B.md", "body")
	if _, err := v.GetBacklinks("sub/B.md"); err != nil {
		t.Fatal(err) // builds the index
	}
	writeRaw(t, v, "C.md", "also [[B]]") // bypasses the vault
	bl, err := v.GetBacklinks("sub/B.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(froms(bl), ","); got != "A.md,C.md" {
		t.Errorf("backlinks = %s, want A.md,C.md", got)
	}
}

func TestVaultWriteUpdatesIndex(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "start\n")
	if _, err := v.GetOutgoingLinks("A.md"); err != nil {
		t.Fatal(err) // builds the index
	}
	if err := v.AppendToNote("A.md", "[[New]]"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateNote("New.md", "hi", nil); err != nil {
		t.Fatal(err)
	}
	out, err := v.GetOutgoingLinks("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].To != "New.md" || out[0].ToKind != index.NodeNote {
		t.Fatalf("outgoing = %+v, want note edge to New.md", out)
	}
}

// The write paths must update the index directly (no Refresh involved), keyed
// by the normalized vault-relative path, not the raw argument.
func TestWritesUpdateIndexByNormalizedPath(t *testing.T) {
	v := newTestVault(t)
	if _, err := v.GetOrphanedNotes(); err != nil {
		t.Fatal(err) // builds the (empty) index
	}
	ix := v.idx.Load()
	if ix == nil {
		t.Fatal("index not built")
	}
	if err := v.WriteNote("./sub/../c.md", "1"); err != nil {
		t.Fatal(err)
	}
	if c, ok := ix.Canonical("c"); !ok || c != "c.md" {
		t.Errorf("after WriteNote Canonical(c) = %q, %v", c, ok)
	}
	if _, err := v.CreateNote("d/e", "x", nil); err != nil {
		t.Fatal(err)
	}
	if c, ok := ix.Canonical("d/e"); !ok || c != "d/e.md" {
		t.Errorf("after CreateNote Canonical(d/e) = %q, %v", c, ok)
	}
	if err := v.DeleteNote("./sub/../c"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Canonical("c"); ok {
		t.Error("after DeleteNote c.md is still indexed")
	}
}

func TestVaultOrphansAndGraph(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "[[B]] [[Missing]] ![[img.png]]")
	writeRaw(t, v, "B.md", "")
	writeRaw(t, v, "Lonely.md", "")
	writeRaw(t, v, "img.png", "png")

	orphans, err := v.GetOrphanedNotes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(orphans, ",") != "Lonely.md" {
		t.Errorf("orphans = %v", orphans)
	}
	nodes, edges, err := v.BuildLinkGraph()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]index.NodeKind{}
	for _, n := range nodes {
		kinds[n.ID] = n.Kind
	}
	if kinds["img.png"] != index.NodeAttachment || kinds["missing"] != index.NodeGhost || kinds["Lonely.md"] != index.NodeNote {
		t.Errorf("nodes = %+v", nodes)
	}
	if len(edges) != 3 {
		t.Errorf("edges = %+v", edges)
	}
}

// No graph error may leak the absolute vault root.
func TestVaultGraphErrorsNoAbsoluteRoot(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "x")
	for _, arg := range []string{"nope", "../x", "../../etc/passwd", "sub/../../x"} {
		for name, fn := range map[string]func(string) ([]index.Edge, error){
			"GetBacklinks":     v.GetBacklinks,
			"GetOutgoingLinks": v.GetOutgoingLinks,
		} {
			_, err := fn(arg)
			if err == nil {
				t.Fatalf("%s(%q): want error", name, arg)
			}
			if want := "note not found: " + arg; err.Error() != want {
				t.Errorf("%s(%q) err = %q, want %q", name, arg, err, want)
			}
			if strings.Contains(err.Error(), v.Root()) {
				t.Errorf("%s(%q) leaks root: %q", name, arg, err)
			}
		}
	}
}

func TestListFilesAndStatsUseSlashPaths(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a/b/n.md", "x")
	writeRaw(t, v, "a/b/pic.png", "p")
	writeRaw(t, v, "a/b/board.canvas", "{}")
	writeRaw(t, v, ".obsidian/app.md", "x")
	writeRaw(t, v, ".hidden/h.png", "x")
	writeRaw(t, v, "a/.secret.png", "x")
	writeRaw(t, v, ".git/config", "x")

	files, err := v.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "a/b/n.md" {
		t.Errorf("ListFiles = %v", files)
	}
	notes, err := v.StatNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Path != "a/b/n.md" || notes[0].Size != 1 || notes[0].ModTime.IsZero() {
		t.Errorf("StatNotes = %+v", notes)
	}
	atts, err := v.StatAttachments()
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Path != "a/b/pic.png" {
		t.Errorf("StatAttachments = %+v", atts)
	}
}

func TestStatAttachmentsSkipsSymlinks(t *testing.T) {
	v := newTestVault(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, v, "real.png", "r")
	mustSymlink(t, filepath.Join(outside, "secret.png"), filepath.Join(v.Root(), "file-link.png"))
	mustSymlink(t, outside, filepath.Join(v.Root(), "dir-link"))
	mustSymlink(t, filepath.Join(v.Root(), "real.png"), filepath.Join(v.Root(), "inner-link.png"))

	atts, err := v.StatAttachments()
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Path != "real.png" {
		t.Errorf("StatAttachments = %+v, want only real.png", atts)
	}
}

// ReadNote as index.Source keeps the security checks: a .md symlink that
// escapes the vault is listed by StatNotes but cannot be read.
func TestSourceReadNoteKeepsContainment(t *testing.T) {
	v := newTestVault(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.md"), []byte("[[Leak]]"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(outside, "o.md"), filepath.Join(v.Root(), "esc.md"))
	var src index.Source = v
	if _, err := src.ReadNote("esc.md"); err == nil {
		t.Fatal("reading an escaping symlink must fail")
	}
	out, err := v.GetOutgoingLinks("esc.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("escaping note content was indexed: %+v", out)
	}
}
