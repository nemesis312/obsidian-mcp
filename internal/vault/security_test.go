package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempRoot returns a temp dir with symlinks already resolved (macOS /var -> /private/var).
func tempRoot(t *testing.T) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestValidatePath_ValidPath(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)
	validPath := filepath.Join(root, "notes", "test.md")

	result, err := sec.ValidatePath(validPath)
	if err != nil {
		t.Fatalf("Expected no error for valid path, got %v", err)
	}
	if result != validPath {
		t.Errorf("Expected %s, got %s", validPath, result)
	}
}

func TestValidatePath_RelativePath(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)

	result, err := sec.ValidatePath("notes/test.md")
	if err != nil {
		t.Fatalf("Expected no error for relative path, got %v", err)
	}
	if expected := filepath.Join(root, "notes", "test.md"); result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestValidatePath_TraversalAttempt(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)

	for _, p := range []string{
		filepath.Join(root, "..", "..", "..", "etc", "passwd"),
		"../outside.md",
		"a/../../outside.md",
		"..",
	} {
		_, err := sec.ValidatePath(p)
		if err == nil {
			t.Fatalf("Expected error for traversal %q", p)
		}
		if !strings.Contains(err.Error(), "path_violation") {
			t.Errorf("Expected path_violation error for %q, got %v", p, err)
		}
	}
}

func TestValidatePath_DotDotInName(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)
	for _, p := range []string{"notes..md", "a..b/c.md", "..hidden.md"} {
		if _, err := sec.ValidatePath(p); err != nil {
			t.Errorf("%q should be allowed, got %v", p, err)
		}
	}
}

func TestValidatePath_ProtectedSegments(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)

	blocked := []string{
		".obsidian/app.json",
		".obsidian",
		".git/config",
		".git",
		"sub/.git",
		"sub/.git/config",
		"a/b/.obsidian/plugins/x.json",
		"sub/../.git/config",
		".GIT/config",
	}
	for _, p := range blocked {
		_, err := sec.ValidatePath(p)
		if err == nil {
			t.Errorf("%q should be blocked", p)
			continue
		}
		if !strings.Contains(err.Error(), "protected directory") {
			t.Errorf("%q: expected protected directory error, got %v", p, err)
		}
	}

	allowed := []string{
		".github/x.md",
		".gitignore",
		"notes/.gitkeep-ish.md",
		".obsidian-backup/x.md",
		"notes/git.md",
		"my.git/x.md",
	}
	for _, p := range allowed {
		if _, err := sec.ValidatePath(p); err != nil {
			t.Errorf("%q should be allowed, got %v", p, err)
		}
	}
}

func TestValidatePath_AbsoluteProtected(t *testing.T) {
	root := tempRoot(t)
	sec := NewSecurityLayer(root)
	_, err := sec.ValidatePath(filepath.Join(root, ".obsidian", "config.json"))
	if err == nil || !strings.Contains(err.Error(), "protected directory") {
		t.Fatalf("expected protected directory error, got %v", err)
	}
}

func TestValidatePath_DirSymlinkEscape(t *testing.T) {
	root := tempRoot(t)
	outside := tempRoot(t)
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, outside, filepath.Join(root, "link"))
	sec := NewSecurityLayer(root)

	// existing file through the dir symlink
	if _, err := sec.ValidatePath("link/secret.md"); err == nil || !strings.Contains(err.Error(), "path_violation") {
		t.Fatalf("expected path_violation, got %v", err)
	}
	// not-yet-existing file (create) through the dir symlink
	if _, err := sec.ValidatePath("link/new/created.md"); err == nil || !strings.Contains(err.Error(), "path_violation") {
		t.Fatalf("expected path_violation for create, got %v", err)
	}
}

func TestValidatePath_FileSymlinkEscape(t *testing.T) {
	root := tempRoot(t)
	outside := tempRoot(t)
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, secret, filepath.Join(root, "evil.md"))
	sec := NewSecurityLayer(root)

	if _, err := sec.ValidatePath("evil.md"); err == nil || !strings.Contains(err.Error(), "path_violation") {
		t.Fatalf("expected path_violation, got %v", err)
	}
}

func TestValidatePath_DanglingSymlinkRejected(t *testing.T) {
	root := tempRoot(t)
	outside := tempRoot(t)
	mustSymlink(t, filepath.Join(outside, "not-yet.md"), filepath.Join(root, "dangling.md"))
	sec := NewSecurityLayer(root)

	if _, err := sec.ValidatePath("dangling.md"); err == nil {
		t.Fatal("expected error for dangling symlink pointing outside")
	}
}

func TestValidatePath_InternalSymlinkAllowed(t *testing.T) {
	root := tempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "n.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(root, "real"), filepath.Join(root, "alias"))
	mustSymlink(t, filepath.Join(root, "real", "n.md"), filepath.Join(root, "f.md"))
	sec := NewSecurityLayer(root)

	got, err := sec.ValidatePath("alias/n.md")
	if err != nil {
		t.Fatalf("internal dir symlink should be allowed: %v", err)
	}
	if want := filepath.Join(root, "real", "n.md"); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if _, err := sec.ValidatePath("f.md"); err != nil {
		t.Errorf("internal file symlink should be allowed: %v", err)
	}
}

func TestValidatePath_SymlinkIntoProtectedDir(t *testing.T) {
	root := tempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(root, ".obsidian"), filepath.Join(root, "cfg"))
	sec := NewSecurityLayer(root)
	if _, err := sec.ValidatePath("cfg/app.json"); err == nil || !strings.Contains(err.Error(), "protected directory") {
		t.Fatalf("expected protected directory error, got %v", err)
	}
}

func TestValidatePath_SymlinkedRoot(t *testing.T) {
	real := tempRoot(t)
	linkParent := tempRoot(t)
	linkRoot := filepath.Join(linkParent, "vault-link")
	mustSymlink(t, real, linkRoot)

	sec := NewSecurityLayer(linkRoot)
	got, err := sec.ValidatePath("n.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(real, "n.md"); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// absolute path through the unresolved alias must also be accepted
	if _, err := sec.ValidatePath(filepath.Join(linkRoot, "n.md")); err != nil {
		t.Errorf("absolute path via root alias should be allowed: %v", err)
	}
}

func TestValidatePath_OutsideRejectedBeforeFSAccess(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission-based check needs POSIX non-root")
	}
	root := tempRoot(t)
	outsideParent := tempRoot(t)
	locked := filepath.Join(outsideParent, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)
	sec := NewSecurityLayer(root)

	rel, _ := filepath.Rel(root, filepath.Join(locked, "sub", "x.md"))
	for _, p := range []string{filepath.Join(locked, "sub", "x.md"), rel, filepath.Join(outsideParent, "does-not-exist", "x.md")} {
		_, err := sec.ValidatePath(p)
		if err == nil {
			t.Fatalf("%q must be rejected", p)
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "outside the vault") {
			t.Errorf("%q: error should be generic outside-vault, got %v", p, err)
		}
		for _, leak := range []string{"permission", "no such file", "not exist", "lstat", "stat "} {
			if strings.Contains(msg, leak) {
				t.Errorf("%q: error leaks FS detail %q: %v", p, leak, err)
			}
		}
	}
}

func TestValidatePath_WindowsUNCAndVolumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	root := tempRoot(t)
	sec := NewSecurityLayer(root)
	for _, p := range []string{`\\attacker\share\x.md`, `//attacker/share/x.md`, `\\?\C:\x.md`, `D:\other\x.md`, `Z:x.md`} {
		_, err := sec.ValidatePath(p)
		if err == nil || !strings.Contains(err.Error(), "outside the vault") {
			t.Errorf("%q: expected outside-vault rejection, got %v", p, err)
		}
	}
}

// Windows strips a trailing "." or space from a name, so ".git." and
// ".obsidian-mcp " reach the protected folder there. They are blocked on
// every OS.
func TestHasProtectedSegmentTrailingDotSpace(t *testing.T) {
	for _, rel := range []string{
		".git.", ".obsidian-mcp ", ".obsidian. .", filepath.Join("sub", ".GIT. ", "x.md"),
		filepath.Join(".obsidian-mcp.", "x.md"),
	} {
		if !hasProtectedSegment(rel) {
			t.Errorf("hasProtectedSegment(%q) = false, want true", rel)
		}
	}
	for _, rel := range []string{".github", ".gitx.", "my.git", "notes.", "..."} {
		if hasProtectedSegment(rel) {
			t.Errorf("hasProtectedSegment(%q) = true, want false", rel)
		}
	}
}

// The Windows-only lexical segment check: ':' (alternate data streams), a
// trailing "." or space, and the 8.3 short-name form "~<digit>".
func TestWindowsUnsafeSegment(t *testing.T) {
	for _, rel := range []string{
		filepath.Join(".git::$INDEX_ALLOCATION", "x.md"), "a.md:stream", filepath.Join("dir.", "x.md"),
		"x.md ", filepath.Join("OBSIDI~1", "x.md"), "note~2.md",
	} {
		if !hasWindowsUnsafeSegment(rel) {
			t.Errorf("hasWindowsUnsafeSegment(%q) = false, want true", rel)
		}
	}
	for _, rel := range []string{".", "x.md", filepath.Join("a b", "c.d.md"), "tilde~x.md", "~.md", "v1.2 notes.md"} {
		if hasWindowsUnsafeSegment(rel) {
			t.Errorf("hasWindowsUnsafeSegment(%q) = true, want false", rel)
		}
	}
}

// On Windows, ValidatePath rejects those segments before any filesystem call.
func TestValidatePath_WindowsUnsafeSegments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	sec := NewSecurityLayer(tempRoot(t))
	for _, p := range []string{`.obsidian-mcp.\x.md`, `.git.\x.md`, `.obsidian \x.md`, `.git::$INDEX_ALLOCATION\x.md`, `OBSIDI~1\x.md`, `a.md:s`} {
		if _, err := sec.ValidatePath(p); err == nil || !strings.Contains(err.Error(), "path_violation") {
			t.Errorf("%q: err = %v, want path_violation", p, err)
		}
	}
}
