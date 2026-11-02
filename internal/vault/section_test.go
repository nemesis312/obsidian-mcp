package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

const sectionNote = "---\ntitle: T\n---\nintro\n\n# A\na body\n\n## A1\nsub\n\n# B\nb body\n\ntrailing\n"

func readRaw(t *testing.T, v *Vault, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEditSectionReplaceNeedsVersion(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	_, err := v.EditSection("n.md", "A", SectionReplace, "x", "")
	if !errors.Is(err, ErrSectionVersionRequired) {
		t.Fatalf("err = %v, want ErrSectionVersionRequired", err)
	}
	if got := readRaw(t, v, "n.md"); got != sectionNote {
		t.Fatalf("file changed: %q", got)
	}
}

func TestEditSectionInvalidMode(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	if _, err := v.EditSection("n.md", "A", SectionMode("nope"), "x", ""); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("err = %v, want invalid mode", err)
	}
}

func TestEditSectionStaleVersionConflict(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	stale := VersionOf([]byte("something else"))
	_, err := v.EditSection("n.md", "A", SectionReplace, "x", stale)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if ce.Current != VersionOf([]byte(sectionNote)) {
		t.Fatalf("current = %q", ce.Current)
	}
	if got := readRaw(t, v, "n.md"); got != sectionNote {
		t.Fatalf("file changed: %q", got)
	}
}

func TestEditSectionAppendWithoutVersion(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	snap, err := v.EditSection("n.md", "B", SectionAppend, "more", "")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sectionNote, "b body\n\ntrailing\n", "b body\n\ntrailing\nmore\n", 1)
	if got := readRaw(t, v, "n.md"); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if snap.Version != VersionOf([]byte(want)) {
		t.Fatalf("snapshot version %q does not match file", snap.Version)
	}
}

func TestEditSectionOtherBytesUnchanged(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	ver := VersionOf([]byte(sectionNote))
	if _, err := v.EditSection("n.md", "A > A1", SectionReplace, "new sub", ver); err != nil {
		t.Fatal(err)
	}
	got := readRaw(t, v, "n.md")
	h, err := markdown.FindSection(sectionNote, "A > A1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, sectionNote[:h.BodyStart]) {
		t.Fatalf("bytes before the section changed: %q", got)
	}
	if !strings.HasSuffix(got, sectionNote[h.End:]) {
		t.Fatalf("bytes after the section changed: %q", got)
	}
	if !strings.Contains(got, "## A1\nnew sub\n") {
		t.Fatalf("body not replaced: %q", got)
	}
}

func TestEditSectionAmbiguousListsPaths(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", "# X\n## Dup\na\n# Y\n## Dup\nb\n")
	_, err := v.EditSection("n.md", "Dup", SectionAppend, "z", "")
	var ae *markdown.AmbiguousSectionError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want AmbiguousSectionError", err)
	}
	if !strings.Contains(err.Error(), "X > Dup") || !strings.Contains(err.Error(), "Y > Dup") {
		t.Fatalf("paths missing: %v", err)
	}
}

func TestEditSectionNotFound(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", sectionNote)
	_, err := v.EditSection("n.md", "Nope", SectionPrepend, "z", "")
	var nf *markdown.SectionNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want SectionNotFoundError", err)
	}
}

func TestEditSectionListBeforeThematicBreak(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "n.md", "## Tasks\n- a\n- b\n---\n## Next\nx\n")
	if _, err := v.EditSection("n.md", "Tasks", SectionAppend, "- c", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.ReadNote("n.md"); got != "## Tasks\n- a\n- b\n---\n- c\n## Next\nx\n" {
		t.Errorf("n.md = %q, want - c after the section's last line", got)
	}
}
