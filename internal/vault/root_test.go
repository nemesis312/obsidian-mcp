package vault

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requireDirSymlinks skips the test on Windows when creating a directory
// symlink needs a privilege the test process does not have.
func requireDirSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "d"), filepath.Join(dir, "l")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

// swapSubAfterValidation sets preWriteHook to replace the vault folder sub
// with a symlink to outside, once validation has passed.
func swapSubAfterValidation(t *testing.T, v *Vault, outside string) {
	t.Helper()
	sub := filepath.Join(v.Root(), "sub")
	called := false
	preWriteHook = func(string) {
		if called {
			return
		}
		called = true
		if err := os.RemoveAll(sub); err != nil {
			t.Errorf("remove sub: %v", err)
			return
		}
		if err := os.Symlink(outside, sub); err != nil {
			t.Errorf("symlink sub: %v", err)
		}
	}
	t.Cleanup(func() {
		preWriteHook = nil
		if !called {
			t.Error("preWriteHook was not called")
		}
	})
}

// assertNoPathLeak fails when err names outside or the vault root.
func assertNoPathLeak(t *testing.T, v *Vault, outside string, err error) {
	t.Helper()
	msg := err.Error()
	for _, p := range []string{outside, v.Root(), v.rawRoot} {
		if strings.Contains(msg, p) {
			t.Errorf("error %q contains absolute path %q", msg, p)
		}
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteRefusesSymlinkSwappedAfterValidation(t *testing.T) {
	requireDirSymlinks(t)
	v := newTestVault(t)
	writeRaw(t, v, "sub/n.md", "hello\n")
	outside := t.TempDir()
	swapSubAfterValidation(t, v, outside)

	_, err := v.AppendToNote("sub/n.md", "x", "")
	if err == nil {
		t.Fatal("AppendToNote through a swapped symlink succeeded")
	}
	if names := dirEntries(t, outside); len(names) != 0 {
		t.Errorf("outside holds %v, want empty", names)
	}
	assertNoPathLeak(t, v, outside, err)
}

func TestCreateRefusesSymlinkSwappedAfterValidation(t *testing.T) {
	requireDirSymlinks(t)
	v := newTestVault(t)
	writeRaw(t, v, "sub/n.md", "hello\n")
	outside := t.TempDir()
	swapSubAfterValidation(t, v, outside)

	_, err := v.CreateNote("sub/new.md", "fresh\n", nil)
	if err == nil {
		t.Fatal("CreateNote through a swapped symlink succeeded")
	}
	if names := dirEntries(t, outside); len(names) != 0 {
		t.Errorf("outside holds %v, want empty", names)
	}
	assertNoPathLeak(t, v, outside, err)
}

func TestDeleteRefusesSymlinkSwappedAfterValidation(t *testing.T) {
	requireDirSymlinks(t)
	v := newTestVault(t)
	writeRaw(t, v, "sub/n.md", "hello\n")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "n.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapSubAfterValidation(t, v, outside)

	_, err := v.DeleteNote("sub/n.md", VersionOf([]byte("hello\n")))
	if err == nil {
		t.Fatal("DeleteNote through a swapped symlink succeeded")
	}
	if _, serr := os.Stat(filepath.Join(outside, "n.md")); serr != nil {
		t.Errorf("outside/n.md is gone: %v", serr)
	}
	assertNoPathLeak(t, v, outside, err)
}

func TestTempFileNameIsHiddenAndCleaned(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "sub/n.md", "hello\n")
	failed := errors.New("rename failed")
	var tmpSeen string
	replaceFile = func(r *os.Root, oldRel, newRel string) error {
		tmpSeen = oldRel
		return failed
	}
	t.Cleanup(func() { replaceFile = renameReplace })

	if _, err := v.AppendToNote("sub/n.md", "x", ""); !errors.Is(err, failed) {
		t.Fatalf("AppendToNote error = %v, want the rename failure", err)
	}
	base := filepath.Base(tmpSeen)
	if !strings.HasPrefix(base, ".obsidian-mcp-") || !strings.HasSuffix(base, ".tmp") || len(base) != len(".obsidian-mcp-")+16+len(".tmp") {
		t.Errorf("temp name %q, want .obsidian-mcp-<16 hex>.tmp", base)
	}
	if filepath.Dir(tmpSeen) != "sub" {
		t.Errorf("temp dir %q, want sub (relative to the vault root)", filepath.Dir(tmpSeen))
	}
	for _, name := range dirEntries(t, filepath.Join(v.Root(), "sub")) {
		if strings.HasPrefix(name, ".obsidian-mcp-") {
			t.Errorf("temp file %s left behind", name)
		}
	}
	if got, err := v.ReadNote("sub/n.md"); err != nil || got != "hello\n" {
		t.Errorf("note = %q, %v; want it unchanged", got, err)
	}
}
