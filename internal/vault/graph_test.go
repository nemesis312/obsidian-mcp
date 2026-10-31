package vault

import (
	"os"
	"path/filepath"
	"runtime"
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

// An unreadable subdirectory is skipped: the rest of the vault is indexed.
func TestUnreadableSubdirSkipped(t *testing.T) {
	skipNoChmod(t)
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "[[B]]")
	writeRaw(t, v, "B.md", "")
	writeRaw(t, v, "Lonely.md", "")
	locked := filepath.Join(v.Root(), "locked")
	writeRaw(t, v, "locked/n.md", "[[A]]")
	lock(t, locked)

	orphans, err := v.GetOrphanedNotes()
	if err != nil {
		t.Fatalf("GetOrphanedNotes with a locked subdir: %v", err)
	}
	if strings.Join(orphans, ",") != "Lonely.md" {
		t.Errorf("orphans = %v, want Lonely.md", orphans)
	}
	if _, edges, err := v.BuildLinkGraph(); err != nil || len(edges) != 1 {
		t.Errorf("BuildLinkGraph = %+v, %v; want one edge A -> B", edges, err)
	}
}

// A failed first Build must not be remembered: once the cause is gone the
// next graph call builds the index. An unreadable vault root still fails.
func TestLinkIndexRetriesAfterBuildFailure(t *testing.T) {
	skipNoChmod(t)
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "x")
	writeRaw(t, v, "sub/n.md", "x")
	lock(t, v.Root())

	_, err := v.GetOrphanedNotes()
	if err == nil {
		t.Fatal("want a build error while the vault root is unreadable")
	}
	if strings.Contains(err.Error(), v.Root()) {
		t.Errorf("build error leaks root: %q", err)
	}

	if err := os.Chmod(v.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	orphans, err := v.GetOrphanedNotes()
	if err != nil {
		t.Fatalf("second call after restoring the mode: %v", err)
	}
	if strings.Join(orphans, ",") != "A.md,sub/n.md" {
		t.Errorf("orphans = %v", orphans)
	}
}

func skipNoChmod(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 does not block directory reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// lock makes dir unreadable until the test ends.
func lock(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// ".MD" is a note in any case, as the resolver treats it: listed by
// StatNotes, not StatAttachments, and [[X]] resolves to it as a note.
func TestUppercaseMDIsANote(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "X.MD", "upper")
	writeRaw(t, v, "A.md", "[[X]]")
	writeRaw(t, v, "b.Canvas", "{}")

	notes, err := v.StatNotes()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, n := range notes {
		paths = append(paths, n.Path)
	}
	sort.Strings(paths)
	if strings.Join(paths, ",") != "A.md,X.MD" {
		t.Errorf("StatNotes = %v, want A.md,X.MD", paths)
	}
	atts, err := v.StatAttachments()
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Errorf("StatAttachments = %+v, want none (X.MD is a note, b.Canvas a canvas)", atts)
	}

	out, err := v.GetOutgoingLinks("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].To != "X.MD" || out[0].ToKind != index.NodeNote {
		t.Fatalf("outgoing = %+v, want note edge to X.MD", out)
	}
	// A write through the vault and the next Refresh agree on the kind.
	if err := v.WriteNote("X.MD", "changed"); err != nil {
		t.Fatal(err)
	}
	out, err = v.GetOutgoingLinks("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ToKind != index.NodeNote {
		t.Fatalf("after write+refresh outgoing = %+v", out)
	}
}

// ReadNote must not follow a symlink that appears after ValidatePath ran
// (symlink-flip race): the open itself is confined to the vault.
func TestReadNoteSymlinkFlipAfterValidate(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "dir/n.md", "INSIDE")
	writeRaw(t, v, "plain.md", "plain")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "n.md"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(outside, "n.md"), filepath.Join(v.Root(), "esc.md"))

	if got, err := v.ReadNote("plain.md"); err != nil || got != "plain" {
		t.Fatalf("ReadNote(plain.md) = %q, %v", got, err)
	}
	if _, err := v.ReadNote("esc.md"); err == nil {
		t.Fatal("ReadNote(esc.md): an escaping symlink must be rejected")
	}

	dir := filepath.Join(v.Root(), "dir")
	readNoteHook = func() {
		if err := os.Rename(dir, dir+"-old"); err != nil {
			t.Error(err)
			return
		}
		if err := os.Symlink(outside, dir); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { readNoteHook = nil })
	got, err := v.ReadNote("dir/n.md")
	if err == nil || strings.Contains(got, "SECRET") {
		t.Fatalf("ReadNote after flip = %q, %v; want an error and no outside content", got, err)
	}
	if strings.Contains(err.Error(), v.Root()) {
		t.Errorf("error leaks root: %q", err)
	}
}
