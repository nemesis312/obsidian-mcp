package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

// The write lock lives at <vault>/.obsidian-mcp/write.lock. The directory is
// a protected segment, so it is never listed or addressable as vault content.
const lockDirName, lockFileName = ".obsidian-mcp", "write.lock"

// defaultLockTimeout bounds how long a write waits for the vault's lock.
const defaultLockTimeout = 5 * time.Second

// ErrBusy is returned when the write lock cannot be taken within the timeout.
var ErrBusy = errors.New("vault is busy: another obsidian-mcp process is writing; retry")

const (
	lockMinBackoff = 5 * time.Millisecond
	lockMaxBackoff = 50 * time.Millisecond
	// lockOpenRetries bounds the retries of a lock file open that failed
	// with ENOENT (see openLockFile).
	lockOpenRetries = 10
)

// writeGuard serializes writes to one vault, within this process and across
// processes. In-process callers queue on mu; across processes an OS advisory
// lock on the lock file is used, which the OS releases when the holder dies,
// so there is no stale-lock recovery. The lock file is never deleted:
// deleting it would let two processes lock different inodes. Readers never
// lock. Advisory locks over network filesystems (NFS, SMB) are not reliable.
type writeGuard struct {
	root    string
	timeout time.Duration
	// mu is a one-slot semaphore rather than a sync.Mutex so that waiting
	// for it is bounded by timeout too.
	mu chan struct{}
}

// newWriteGuard makes a guard for the vault at root. It does no filesystem
// call; the lock directory and file are created on the first lock.
func newWriteGuard(root string, timeout time.Duration) *writeGuard {
	return &writeGuard{root: root, timeout: timeout, mu: make(chan struct{}, 1)}
}

// lock takes the in-process lock, then the OS lock on the lock file. It
// returns ErrBusy if either is not acquired within the guard's timeout. The
// returned unlock releases both and is safe to call more than once.
func (g *writeGuard) lock() (unlock func(), err error) {
	deadline := time.Now().Add(g.timeout)

	timer := time.NewTimer(g.timeout)
	select {
	case g.mu <- struct{}{}:
		timer.Stop()
	case <-timer.C:
		return nil, ErrBusy
	}
	release := func() { <-g.mu }

	f, err := g.openLockFile()
	if err != nil {
		release()
		return nil, err
	}

	backoff := lockMinBackoff
	for {
		ok, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			release()
			return nil, lockError(err)
		}
		if ok {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = f.Close()
			release()
			return nil, ErrBusy
		}
		time.Sleep(min(backoff, remaining))
		backoff = min(backoff*2, lockMaxBackoff)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unlockFile(f)
			_ = f.Close()
			release()
		})
	}, nil
}

// openLockFile creates (if needed) and opens the lock file through os.Root,
// so no symlink or junction can redirect it outside the vault. The lock
// directory must be a real directory, and the lock file a regular file, so
// the lock never lands in (or opens) vault content either.
func (g *writeGuard) openLockFile() (*os.File, error) {
	r, err := os.OpenRoot(g.root)
	if err != nil {
		return nil, lockError(err)
	}
	defer r.Close()

	if err := r.Mkdir(lockDirName, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, lockError(err)
	}
	fi, err := r.Lstat(lockDirName)
	if err != nil {
		return nil, lockError(err)
	}
	if fi.Mode().Type() != fs.ModeDir {
		return nil, lockError(fmt.Errorf("%s is not a directory", lockDirName))
	}

	name := lockDirName + "/" + lockFileName
	// On darwin, openat(O_CREAT) can fail with ENOENT while another process
	// creates the same file, so a fresh vault's first concurrent locks need
	// a few bounded retries.
	var f *os.File
	for attempt := 0; ; attempt++ {
		f, err = r.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || attempt == lockOpenRetries {
			return nil, lockError(err)
		}
		time.Sleep(lockMinBackoff)
	}
	fi, err = r.Lstat(name)
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", lockFileName)
	}
	if err != nil {
		_ = f.Close()
		return nil, lockError(err)
	}
	return f, nil
}

// lockError reports a lock setup failure with the vault-relative lock path
// only: the cause is unwrapped from any *fs.PathError, whose path may be
// absolute.
func lockError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		err = le.Err
	}
	return fmt.Errorf("write lock %s/%s: %w", lockDirName, lockFileName, err)
}
