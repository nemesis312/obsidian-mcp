package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func readFile(t *testing.T, v *Vault, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVersionOf(t *testing.T) {
	re := regexp.MustCompile(`^sha256:[0-9a-f]{16}$`)
	got := VersionOf([]byte("a\n"))
	if !re.MatchString(got) {
		t.Fatalf("VersionOf = %q, want sha256:<16 hex>", got)
	}
	sum := sha256.Sum256([]byte("a\n"))
	if want := "sha256:" + hex.EncodeToString(sum[:])[:16]; got != want {
		t.Errorf("VersionOf = %q, want %q", got, want)
	}
	if VersionOf([]byte("a\n")) != got {
		t.Error("VersionOf is not deterministic")
	}
	if VersionOf([]byte("a\r\n")) == got {
		t.Error("LF and CRLF must give different versions")
	}
	if VersionOf([]byte("\uFEFFa\n")) == got {
		t.Error("a leading BOM must change the version")
	}
	if !re.MatchString(VersionOf(nil)) {
		t.Error("empty content must still give a well-formed version")
	}
}

func TestReadNoteVersioned(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "sub/Deep Note.md", "body\r\n")
	snap, err := v.ReadNoteVersioned("deep note")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Path != "sub/Deep Note.md" {
		t.Errorf("Path = %q, want sub/Deep Note.md", snap.Path)
	}
	if snap.Content != "body\r\n" {
		t.Errorf("Content = %q", snap.Content)
	}
	if want := VersionOf(readFile(t, v, "sub/Deep Note.md")); snap.Version != want {
		t.Errorf("Version = %q, want %q", snap.Version, want)
	}
	if _, err := v.ReadNoteVersioned("nope"); err == nil || !strings.Contains(err.Error(), "note not found: nope") {
		t.Errorf("missing note: err = %v", err)
	}
}

func TestReplaceNote(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "dir/n.md", "old\n")
	cur := VersionOf([]byte("old\n"))

	if _, err := v.ReplaceNote("dir/n", "x", ""); !errors.Is(err, ErrVersionRequired) {
		t.Errorf("no token: err = %v, want ErrVersionRequired", err)
	}
	if _, err := v.ReplaceNote("dir/n", "x", "bogus"); err == nil || !strings.Contains(err.Error(), "invalid expected_version") {
		t.Errorf("bogus token: err = %v", err)
	}
	if _, err := v.ReplaceNote("dir/n", "x", "sha256:0123456789ABCDEF"); err == nil || !strings.Contains(err.Error(), "invalid expected_version") {
		t.Errorf("uppercase token: err = %v", err)
	}
	stale := VersionOf([]byte("something else"))
	_, err := v.ReplaceNote("dir/n", "x", stale)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("stale token: err = %v, want *ConflictError", err)
	}
	if ce.Path != "dir/n.md" || ce.Current != cur {
		t.Errorf("conflict = %+v, want path dir/n.md current %s", ce, cur)
	}
	if msg := err.Error(); !strings.Contains(msg, "dir/n.md") || !strings.Contains(msg, cur) || strings.Contains(msg, v.Root()) {
		t.Errorf("conflict message = %q", msg)
	}
	if got := string(readFile(t, v, "dir/n.md")); got != "old\n" {
		t.Fatalf("file changed by a refused replace: %q", got)
	}

	snap, err := v.ReplaceNote("dir/n", "new\n", cur)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, v, "dir/n.md")); got != "new\n" {
		t.Errorf("content = %q", got)
	}
	if snap.Path != "dir/n.md" || snap.Content != "new\n" || snap.Version != VersionOf([]byte("new\n")) {
		t.Errorf("snapshot = %+v", snap)
	}

	if _, err := v.ReplaceNote("dir/missing", "x", cur); err == nil || !strings.Contains(err.Error(), "note not found: dir/missing") {
		t.Errorf("missing note: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "dir", "missing.md")); err == nil {
		t.Error("ReplaceNote created a missing note")
	}
}

func TestReplaceAfterExternalEdit(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "n.md", "line one\nline two\n")
	snap, err := v.ReadNoteVersioned("n")
	if err != nil {
		t.Fatal(err)
	}
	human := []byte("line one\r\nline two\r\n") // line endings only
	if err := os.WriteFile(filepath.Join(v.Root(), "n.md"), human, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = v.ReplaceNote("n", "agent text\n", snap.Version)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ConflictError", err)
	}
	if ce.Current != VersionOf(human) {
		t.Errorf("Current = %s, want %s", ce.Current, VersionOf(human))
	}
	if got := readFile(t, v, "n.md"); string(got) != string(human) {
		t.Errorf("human edit lost: %q", got)
	}
}

// A note deleted after addressing but before the locked read is reported
// by the caller's argument.
func TestReplaceNoteVanishedBeforeLock(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "gone.md", "x")
	writeLockHook = func() { _ = os.Remove(filepath.Join(v.Root(), "gone.md")) }
	t.Cleanup(func() { writeLockHook = nil })
	_, err := v.ReplaceNote("gone", "y", VersionOf([]byte("x")))
	if err == nil || err.Error() != "note not found: gone" {
		t.Errorf("err = %v, want note not found: gone", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "gone.md")); err == nil {
		t.Error("vanished note was recreated")
	}
}

func TestDeleteNote(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "a.md", "keep me")
	if _, err := v.GetOrphanedNotes(); err != nil {
		t.Fatal(err) // builds the index
	}
	if _, err := v.DeleteNote("a", ""); !errors.Is(err, ErrVersionRequired) {
		t.Errorf("no token: err = %v", err)
	}
	if _, err := v.DeleteNote("a", "nope"); err == nil || !strings.Contains(err.Error(), "invalid expected_version") {
		t.Errorf("bad token: err = %v", err)
	}
	var ce *ConflictError
	if _, err := v.DeleteNote("a", VersionOf([]byte("other"))); !errors.As(err, &ce) || ce.Current != VersionOf([]byte("keep me")) {
		t.Errorf("stale token: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "a.md")); err != nil {
		t.Fatal("file deleted despite a conflict")
	}
	p, err := v.DeleteNote("a", VersionOf([]byte("keep me")))
	if err != nil {
		t.Fatal(err)
	}
	if p != "a.md" {
		t.Errorf("path = %q, want a.md", p)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "a.md")); err == nil {
		t.Error("file still exists")
	}
	if _, ok := v.idx.Load().Canonical("a.md"); ok {
		t.Error("deleted note is still indexed")
	}
}

func TestAppendVersionOptional(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "n.md", "first")
	snap, err := v.AppendToNote("n", "second", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := VersionOf(readFile(t, v, "n.md")); snap.Version != want || snap.Content != "first\nsecond" {
		t.Errorf("snapshot = %+v, want version %s", snap, want)
	}
	var ce *ConflictError
	if _, err := v.AppendToNote("n", "x", VersionOf([]byte("first"))); !errors.As(err, &ce) {
		t.Errorf("stale token: err = %v", err)
	}
	if got := string(readFile(t, v, "n.md")); got != "first\nsecond" {
		t.Errorf("file changed by a refused append: %q", got)
	}
	snap, err = v.AppendToNote("n", "third", snap.Version)
	if err != nil {
		t.Fatal(err)
	}
	if want := VersionOf(readFile(t, v, "n.md")); snap.Version != want || snap.Path != "n.md" {
		t.Errorf("snapshot = %+v, want version %s", snap, want)
	}
}

func TestUpdateFrontmatterVersionOptional(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "n.md", "---\nk: 1\n---\nbody\n")
	snap, err := v.UpdateFrontmatter("n", map[string]any{"k": 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := VersionOf(readFile(t, v, "n.md")); snap.Version != want || !strings.Contains(snap.Content, "k: 2") {
		t.Errorf("snapshot = %+v, want version %s", snap, want)
	}
	before := string(readFile(t, v, "n.md"))
	var ce *ConflictError
	if _, err := v.UpdateFrontmatter("n", map[string]any{"k": 3}, VersionOf([]byte("stale"))); !errors.As(err, &ce) {
		t.Errorf("stale token: err = %v", err)
	}
	if got := string(readFile(t, v, "n.md")); got != before {
		t.Errorf("file changed by a refused update: %q", got)
	}
	snap, err = v.UpdateFrontmatter("n", map[string]any{"k": 4}, snap.Version)
	if err != nil {
		t.Fatal(err)
	}
	if want := VersionOf(readFile(t, v, "n.md")); snap.Version != want || !strings.Contains(snap.Content, "k: 4") {
		t.Errorf("snapshot = %+v, want version %s", snap, want)
	}
}

func TestCreateNoteReturnsVersion(t *testing.T) {
	v := newTestVault(t)
	snap, err := v.CreateNote("/folder/new", "body\n", map[string]any{"title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	b := readFile(t, v, "folder/new.md")
	if snap.Path != "folder/new.md" || snap.Content != string(b) || snap.Version != VersionOf(b) {
		t.Errorf("snapshot = %+v, file %q", snap, b)
	}
	if _, err := v.CreateNote(filepath.Join(v.Root(), "abs.md"), "x", nil); !errors.Is(err, ErrAbsolutePath) {
		t.Errorf("absolute path: err = %v, want ErrAbsolutePath", err)
	}
}

func TestWritesWaitForGuard(t *testing.T) {
	v := newTestVault(t)
	v.guard = newWriteGuard(v.Root(), 100*time.Millisecond)
	put(t, v, "n.md", "---\ntags: [old]\n---\nbody #old\n")
	put(t, v, "m.md", "#old here\n")
	tok := VersionOf([]byte("---\ntags: [old]\n---\nbody #old\n"))

	unlock, err := v.guard.lock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)

	if _, err := v.CreateNote("fresh", "x", nil); !errors.Is(err, ErrBusy) {
		t.Errorf("CreateNote: err = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "fresh.md")); err == nil {
		t.Error("CreateNote wrote while the guard was held")
	}
	if _, err := v.AppendToNote("n", "x", ""); !errors.Is(err, ErrBusy) {
		t.Errorf("AppendToNote: err = %v, want ErrBusy", err)
	}
	if _, err := v.ReplaceNote("n", "x", tok); !errors.Is(err, ErrBusy) {
		t.Errorf("ReplaceNote: err = %v, want ErrBusy", err)
	}
	if _, err := v.UpdateFrontmatter("n", map[string]any{"a": 1}, ""); !errors.Is(err, ErrBusy) {
		t.Errorf("UpdateFrontmatter: err = %v, want ErrBusy", err)
	}
	if _, err := v.DeleteNote("n", tok); !errors.Is(err, ErrBusy) {
		t.Errorf("DeleteNote: err = %v, want ErrBusy", err)
	}
	res, err := v.RenameTag("old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renamed) != 0 || len(res.Failed) != 2 {
		t.Fatalf("RenameTag = %+v, want 2 failures", res)
	}
	for _, f := range res.Failed {
		if !strings.Contains(f.Error, "vault is busy") {
			t.Errorf("failure %+v, want vault is busy", f)
		}
	}
	if got := string(readFile(t, v, "n.md")); got != "---\ntags: [old]\n---\nbody #old\n" {
		t.Errorf("n.md changed: %q", got)
	}
	if got := string(readFile(t, v, "m.md")); got != "#old here\n" {
		t.Errorf("m.md changed: %q", got)
	}
}

func TestConcurrentAppendsInProcess(t *testing.T) {
	v := newTestVault(t)
	put(t, v, "log.md", "start\n")
	const goroutines, appends = 2, 100
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*appends)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < appends; i++ {
				if _, err := v.AppendToNote("log", fmt.Sprintf("g%d-%d\n", g, i), ""); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	content := string(readFile(t, v, "log.md"))
	for g := 0; g < goroutines; g++ {
		for i := 0; i < appends; i++ {
			line := fmt.Sprintf("\ng%d-%d\n", g, i)
			if n := strings.Count(content, line); n != 1 {
				t.Fatalf("line %q appears %d times", strings.TrimSpace(line), n)
			}
		}
	}
}

func TestReplaceWhileReaderOpenWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows sharing violations only")
	}
	v := newTestVault(t)
	put(t, v, "n.md", "old")
	f, err := os.Open(filepath.Join(v.Root(), "n.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	done := make(chan error, 1)
	go func() {
		_, err := v.ReplaceNote("n", "new", VersionOf([]byte("old")))
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	f.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReplaceNote with a reader open: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReplaceNote did not return")
	}
	if got := string(readFile(t, v, "n.md")); got != "new" {
		t.Errorf("content = %q", got)
	}
}
