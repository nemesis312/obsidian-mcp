package vault

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	guardHelperEnv      = "OBSIDIAN_MCP_GUARD_HELPER"
	guardHelperVaultEnv = "OBSIDIAN_MCP_GUARD_VAULT"
)

// TestGuardHelperProcess is not a real test: it is the body of the helper
// process that the cross-process tests start. It holds the vault's write
// lock until its stdin is closed.
func TestGuardHelperProcess(t *testing.T) {
	if os.Getenv(guardHelperEnv) != "hold" {
		t.Skip("helper process only")
	}
	g := newWriteGuard(os.Getenv(guardHelperVaultEnv), 5*time.Second)
	unlock, err := g.lock()
	if err != nil {
		os.Stdout.WriteString("error: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Stdout.WriteString("locked\n")
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second): // never outlive a broken parent
	}
	unlock()
	os.Exit(0)
}

// startGuardHolder starts a helper process that holds root's write lock and
// returns once it reports "locked". The helper is always killed in Cleanup.
func startGuardHolder(t *testing.T, root string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestGuardHelperProcess$")
	cmd.Env = append(os.Environ(), guardHelperEnv+"=hold", guardHelperVaultEnv+"="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- s
		_, _ = io.Copy(io.Discard, stdout) // keep the pipe drained
	}()
	select {
	case s := <-line:
		if s != "locked\n" {
			t.Fatalf("helper did not lock: %q", s)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("helper did not report locked in time")
	}
	return cmd, stdin
}

func TestGuardExcludesOtherProcess(t *testing.T) {
	root := tempRoot(t)
	_, stdin := startGuardHolder(t, root)

	start := time.Now()
	unlock, err := newWriteGuard(root, 200*time.Millisecond).lock()
	elapsed := time.Since(start)
	if err == nil {
		unlock()
		t.Fatal("lock succeeded while another process holds it")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("gave up after %v, want >= 200ms", elapsed)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	unlock, err = newWriteGuard(root, time.Second).lock()
	if err != nil {
		t.Fatalf("lock after holder released: %v", err)
	}
	unlock()
}

func TestGuardReleasedWhenHolderKilled(t *testing.T) {
	root := tempRoot(t)
	cmd, _ := startGuardHolder(t, root)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	unlock, err := newWriteGuard(root, time.Second).lock()
	if err != nil {
		t.Fatalf("lock after holder was killed: %v", err)
	}
	unlock()
}

func TestGuardInProcessSerializes(t *testing.T) {
	root := tempRoot(t)
	g := newWriteGuard(root, defaultLockTimeout)
	counter := 0
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				unlock, err := g.lock()
				if err != nil {
					errs <- err
					return
				}
				counter++
				unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if counter != 1600 {
		t.Fatalf("counter = %d, want 1600", counter)
	}
}

func TestGuardUnlockTwiceIsSafe(t *testing.T) {
	root := tempRoot(t)
	g := newWriteGuard(root, time.Second)
	unlock, err := g.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock() // must neither panic nor release someone else's lock

	unlock2, err := g.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock() // stale unlock from the first holder
	done := make(chan error, 1)
	go func() {
		u, err := newWriteGuard(root, 100*time.Millisecond).lock()
		if err == nil {
			u()
		}
		done <- err
	}()
	// The second holder still owns the in-process and the OS lock, so a
	// separate guard on the same vault must be refused.
	if err := <-done; !errors.Is(err, ErrBusy) {
		t.Fatalf("stale unlock released the current holder: %v", err)
	}
	unlock2()
}

func TestGuardInProcessTimeout(t *testing.T) {
	root := tempRoot(t)
	g := newWriteGuard(root, 100*time.Millisecond)
	unlock, err := g.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	start := time.Now()
	u, err := g.lock()
	if err == nil {
		u()
		t.Fatal("second in-process lock succeeded while held")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("in-process wait not bounded by the timeout: %v", d)
	}
}

func assertNoRoot(t *testing.T, err error, roots ...string) {
	t.Helper()
	for _, r := range roots {
		if strings.Contains(err.Error(), r) {
			t.Fatalf("error leaks an absolute path %q: %v", r, err)
		}
	}
}

func TestGuardLockDirSymlinkRefused(t *testing.T) {
	t.Run("symlink outside", func(t *testing.T) {
		root := tempRoot(t)
		outside := tempRoot(t)
		mustSymlink(t, outside, filepath.Join(root, lockDirName))

		unlock, err := newWriteGuard(root, 200*time.Millisecond).lock()
		if err == nil {
			unlock()
			t.Fatal("lock followed a symlink out of the vault")
		}
		assertNoRoot(t, err, root, outside)
		entries, rerr := os.ReadDir(outside)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if len(entries) != 0 {
			t.Fatalf("outside dir was written: %v", entries)
		}
	})

	t.Run("symlink inside", func(t *testing.T) {
		root := tempRoot(t)
		if err := os.Mkdir(filepath.Join(root, "notes"), 0o755); err != nil {
			t.Fatal(err)
		}
		mustSymlink(t, "notes", filepath.Join(root, lockDirName))

		unlock, err := newWriteGuard(root, 200*time.Millisecond).lock()
		if err == nil {
			unlock()
			t.Fatal("lock followed a symlink to a vault folder")
		}
		assertNoRoot(t, err, root)
		if _, serr := os.Lstat(filepath.Join(root, "notes", lockFileName)); !errors.Is(serr, os.ErrNotExist) {
			t.Fatalf("lock file created through the symlink: %v", serr)
		}
	})

	t.Run("regular file", func(t *testing.T) {
		root := tempRoot(t)
		if err := os.WriteFile(filepath.Join(root, lockDirName), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		unlock, err := newWriteGuard(root, 200*time.Millisecond).lock()
		if err == nil {
			unlock()
			t.Fatal("lock succeeded with .obsidian-mcp as a regular file")
		}
		assertNoRoot(t, err, root)
		if !strings.Contains(err.Error(), "write lock .obsidian-mcp/write.lock") {
			t.Fatalf("unexpected error shape: %v", err)
		}
	})
}

func TestGuardMissingRootHasNoAbsolutePath(t *testing.T) {
	root := filepath.Join(tempRoot(t), "gone")
	unlock, err := newWriteGuard(root, 100*time.Millisecond).lock()
	if err == nil {
		unlock()
		t.Fatal("lock succeeded on a missing vault")
	}
	assertNoRoot(t, err, root)
}

func TestLockFileNeverListed(t *testing.T) {
	root := tempRoot(t)
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := v.guard.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(filepath.Join(root, lockDirName, lockFileName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	files, err := v.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	notes, err := v.StatNotes()
	if err != nil {
		t.Fatal(err)
	}
	atts, err := v.StatAttachments()
	if err != nil {
		t.Fatal(err)
	}
	paths := append([]string{}, files...)
	for _, s := range notes {
		paths = append(paths, s.Path)
	}
	for _, s := range atts {
		paths = append(paths, s.Path)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, lockDirName) {
			t.Fatalf("lock path listed as vault content: %s", p)
		}
	}

	_, err = v.security.ValidatePath(lockDirName + "/x.md")
	if err == nil || !strings.Contains(err.Error(), "path_violation") {
		t.Fatalf("ValidatePath(%s/x.md) = %v, want path_violation", lockDirName, err)
	}
}
