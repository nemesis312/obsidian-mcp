package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	v, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNormalizeNotePath(t *testing.T) {
	cases := map[string]string{
		"a.md":     "a.md",
		"a":        "a.md",
		"dir/a":    "dir/a.md",
		"dir/a.MD": "dir/a.MD",
		"dir.x/a":  "dir.x/a.md",
	}
	for in, want := range cases {
		got, err := NormalizeNotePath(in)
		if err != nil || got != want {
			t.Errorf("NormalizeNotePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"a.txt", "x.canvas", "dir/a.json", "a.md.bak", ""} {
		if _, err := NormalizeNotePath(in); err == nil {
			t.Errorf("NormalizeNotePath(%q) should fail", in)
		}
	}
}

func TestWriteNote_RejectsNonMarkdown(t *testing.T) {
	v := newTestVault(t)
	if err := v.WriteNote("evil.txt", "x"); err == nil {
		t.Fatal("expected error for .txt")
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "evil.txt")); err == nil {
		t.Fatal("file must not be created")
	}
	if err := v.DeleteNote("evil.sh"); err == nil {
		t.Fatal("expected error deleting non-md")
	}
}

func TestWriteNote_AppendsMdAndIsAtomic(t *testing.T) {
	v := newTestVault(t)
	if err := v.WriteNote("sub/dir/note", "hello"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(v.Root(), "sub", "dir", "note.md"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("read back: %q %v", b, err)
	}
	entries, _ := os.ReadDir(filepath.Join(v.Root(), "sub", "dir"))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestWriteNote_PreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes not meaningful on windows")
	}
	v := newTestVault(t)
	p := filepath.Join(v.Root(), "n.md")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.WriteNote("n.md", "new"); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Errorf("content = %q", b)
	}
}

func TestWriteNote_ThroughInternalFileSymlinkKeepsLink(t *testing.T) {
	v := newTestVault(t)
	real := filepath.Join(v.Root(), "real.md")
	if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(v.Root(), "link.md")
	mustSymlink(t, real, link)
	if err := v.WriteNote("link.md", "new"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Errorf("target content = %q", b)
	}
}

func TestWriteNote_SymlinkEscapeRejected(t *testing.T) {
	v := newTestVault(t)
	outside := tempRoot(t)
	mustSymlink(t, outside, filepath.Join(v.Root(), "out"))
	if err := v.WriteNote("out/pwn.md", "x"); err == nil {
		t.Fatal("expected escape to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn.md")); err == nil {
		t.Fatal("file written outside vault")
	}
}

func TestCreateNote_ExistsAndFrontmatterSingleWrite(t *testing.T) {
	v := newTestVault(t)
	p, err := v.CreateNote("folder/new", "body\n", map[string]interface{}{"title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	if p != "folder/new.md" {
		t.Errorf("normalized path = %q", p)
	}
	b, _ := os.ReadFile(filepath.Join(v.Root(), "folder", "new.md"))
	if !strings.HasPrefix(string(b), "---\ntitle: T\n---\n") || !strings.HasSuffix(string(b), "body\n") {
		t.Errorf("unexpected content %q", b)
	}
	if _, err := v.CreateNote("folder/new.md", "other", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already exists, got %v", err)
	}
	if b2, _ := os.ReadFile(filepath.Join(v.Root(), "folder", "new.md")); string(b2) != string(b) {
		t.Error("existing note was modified")
	}
	entries, _ := os.ReadDir(filepath.Join(v.Root(), "folder"))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestCreateNote_Concurrent(t *testing.T) {
	v := newTestVault(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.CreateNote("race.md", "x", nil); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected exactly 1 successful create, got %d", wins)
	}
}

func TestCreateNote_RejectsNonMarkdownAndProtected(t *testing.T) {
	v := newTestVault(t)
	if _, err := v.CreateNote("a.txt", "x", nil); err == nil {
		t.Error("expected error for .txt")
	}
	if _, err := v.CreateNote(".obsidian/x.md", "x", nil); err == nil {
		t.Error("expected error for protected dir")
	}
}

func TestAppendAndUpdateFrontmatter_NoExtension(t *testing.T) {
	v := newTestVault(t)
	if err := v.WriteNote("n", "line"); err != nil {
		t.Fatal(err)
	}
	if err := v.AppendToNote("n", "more"); err != nil {
		t.Fatal(err)
	}
	if err := v.UpdateFrontmatter("n", map[string]interface{}{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(v.Root(), "n.md"))
	if !strings.Contains(string(b), "k: v") || !strings.Contains(string(b), "line\nmore") {
		t.Errorf("content = %q", b)
	}
	if err := v.AppendToNote("n.txt", "x"); err == nil {
		t.Error("append to non-md must fail")
	}
}

func TestWrite_SymlinkToNonMarkdownRejected(t *testing.T) {
	v := newTestVault(t)
	js := filepath.Join(v.Root(), "Scripts", "tp_user.js")
	if err := os.MkdirAll(filepath.Dir(js), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(js, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, js, filepath.Join(v.Root(), "idea.md"))

	if err := v.WriteNote("idea.md", "pwn"); err == nil {
		t.Error("WriteNote through .md -> .js symlink must fail")
	}
	if err := v.AppendToNote("idea.md", "pwn"); err == nil {
		t.Error("AppendToNote through .md -> .js symlink must fail")
	}
	if err := v.UpdateFrontmatter("idea.md", map[string]interface{}{"a": 1}); err == nil {
		t.Error("UpdateFrontmatter through .md -> .js symlink must fail")
	}
	if err := v.DeleteNote("idea.md"); err == nil {
		t.Error("DeleteNote through .md -> .js symlink must fail")
	}
	if _, err := v.CreateNote("idea.md", "x", nil); err == nil {
		t.Error("CreateNote over .md -> .js symlink must fail")
	}
	if b, err := os.ReadFile(js); err != nil || string(b) != "orig" {
		t.Errorf("js file changed or removed: %q %v", b, err)
	}
}

func TestWrite_SymlinkToMarkdownAllowed(t *testing.T) {
	v := newTestVault(t)
	real := filepath.Join(v.Root(), "real.md")
	if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, real, filepath.Join(v.Root(), "alias.md"))
	if err := v.WriteNote("alias.md", "new"); err != nil {
		t.Fatalf("symlink to .md must work: %v", err)
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Errorf("content = %q", b)
	}
}
