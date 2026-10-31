//go:build unix && !aix && !(solaris && !illumos)

package vault

// aix and solaris (but not illumos) have no syscall.Flock; they use
// guard_other.go and fail closed.

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes a non-blocking exclusive flock on f. It reports false,
// with no error, when another open file holds the lock.
func tryLockFile(f *os.File) (acquired bool, err error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return false, err
	}
	var lerr error
	if err := rc.Control(func(fd uintptr) {
		lerr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
	}); err != nil {
		return false, err
	}
	switch {
	case lerr == nil:
		return true, nil
	case errors.Is(lerr, syscall.EWOULDBLOCK), errors.Is(lerr, syscall.EINTR):
		return false, nil
	default:
		return false, lerr
	}
}

// unlockFile releases the flock taken by tryLockFile.
func unlockFile(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var uerr error
	if err := rc.Control(func(fd uintptr) {
		uerr = syscall.Flock(int(fd), syscall.LOCK_UN)
	}); err != nil {
		return err
	}
	return uerr
}
