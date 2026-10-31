package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, v *Vault, rel, content string) {
	t.Helper()
	p := filepath.Join(v.Root(), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, v *Vault, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root(), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenameTag_Vault(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "one.md", "---\ntags:\n  - a\n  - a/x\n---\nText #a #a, (#a) #ab\n")
	put(t, v, "sub/two.md", "no tags here\n```\n#a\n```\n")
	put(t, v, "three.md", "---\ntag: a\n---\nbody")
	put(t, v, ".obsidian/skip.md", "#a")

	res, err := v.RenameTag("#a", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}
	if len(res.Renamed) != 2 {
		t.Fatalf("renamed = %v", res.Renamed)
	}
	if got := get(t, v, "one.md"); !strings.Contains(got, "- new\n") || !strings.Contains(got, "- new/x\n") ||
		!strings.Contains(got, "Text #new #new, (#new) #ab\n") {
		t.Errorf("one.md = %q", got)
	}
	if got := get(t, v, "sub/two.md"); got != "no tags here\n```\n#a\n```\n" {
		t.Errorf("two.md modified: %q", got)
	}
	if got := get(t, v, "three.md"); got != "---\ntag: new\n---\nbody" {
		t.Errorf("three.md = %q", got)
	}
	if got := get(t, v, ".obsidian/skip.md"); got != "#a" {
		t.Errorf("protected file touched: %q", got)
	}
}

func TestRenameTag_ValidatesNames(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "n.md", "#a")
	for _, c := range [][2]string{{"a", ""}, {"a", "#"}, {"a", "x y"}, {"a", "x#y"}, {"a", "##x"}, {"", "x"}, {"a", "a"}, {"#a", "a"}} {
		if _, err := v.RenameTag(c[0], c[1]); err == nil {
			t.Errorf("RenameTag(%q,%q) should fail", c[0], c[1])
		}
	}
	if got := get(t, v, "n.md"); got != "#a" {
		t.Errorf("note modified: %q", got)
	}
}

func TestRenameTag_CollectsPerFileErrors(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "good.md", "#a")
	put(t, v, "badyaml.md", "---\ntags: [a\n---\n#a")
	put(t, v, "unclosed.md", "---\ntags: [a]\n#a")

	res, err := v.RenameTag("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renamed) != 1 || res.Renamed[0] != "good.md" {
		t.Errorf("renamed = %v", res.Renamed)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("failed = %+v", res.Failed)
	}
	for _, f := range res.Failed {
		if f.Path == "" || f.Error == "" {
			t.Errorf("incomplete failure entry: %+v", f)
		}
	}
	if got := get(t, v, "badyaml.md"); got != "---\ntags: [a\n---\n#a" {
		t.Errorf("failed file was modified: %q", got)
	}
}

func TestRenameTag_ReadOnlyFileReported(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions not enforced")
	}
	v := newTestVault(t)
	put(t, v, "ro/n.md", "#a")
	dir := filepath.Join(v.Root(), "ro")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	res, err := v.RenameTag("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || len(res.Renamed) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestRenameTag_SymlinkToNonMarkdownReported(t *testing.T) {
	v := newTestVault(t)
	js := filepath.Join(v.Root(), "Scripts", "tp_user.js")
	if err := os.MkdirAll(filepath.Dir(js), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(js, []byte("#a // js"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, js, filepath.Join(v.Root(), "idea.md"))
	put(t, v, "ok.md", "#a")

	res, err := v.RenameTag("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Path != "idea.md" {
		t.Errorf("failed = %+v", res.Failed)
	}
	if len(res.Renamed) != 1 || res.Renamed[0] != "ok.md" {
		t.Errorf("renamed = %v", res.Renamed)
	}
	if got := get(t, v, "Scripts/tp_user.js"); got != "#a // js" {
		t.Errorf("js modified: %q", got)
	}
}

// Once the guard is busy, rename_tag stops waiting for it: every remaining
// tagged note is reported failed with the same reason, in well under the
// 20 × timeout that waiting on each note would take.
func TestRenameTag_StopsAfterBusy(t *testing.T) {
	v := newTestVault(t)
	for i := 0; i < 20; i++ {
		put(t, v, fmt.Sprintf("n%02d.md", i), "#a body\n")
	}
	put(t, v, "untagged.md", "no tag\n")
	v.guard = newWriteGuard(v.root, 100*time.Millisecond)
	unlock, err := v.guard.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	start := time.Now()
	res, err := v.RenameTag("a", "b")
	if d := time.Since(start); d > time.Second {
		t.Errorf("RenameTag took %v, want < 1s", d)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renamed) != 0 || len(res.Failed) != 20 {
		t.Fatalf("renamed %d, failed %d; want 0 and 20: %+v", len(res.Renamed), len(res.Failed), res.Failed)
	}
	for _, f := range res.Failed {
		if f.Path == "untagged.md" || !strings.Contains(f.Error, "vault is busy") {
			t.Errorf("failure %+v, want a tagged note and vault is busy", f)
		}
	}
}

// A cancelled request stops rename_tag before it touches another note.
func TestRenameTag_HonoursContext(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "n.md", "#a body\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := v.RenameTagContext(ctx, "a", "b")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if got := get(t, v, "n.md"); got != "#a body\n" {
		t.Errorf("n.md changed after cancel: %q", got)
	}
}
