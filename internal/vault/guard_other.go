//go:build !windows && (!unix || aix || (solaris && !illumos))

package vault

// Platforms without flock in package syscall (aix, solaris) and non-unix
// platforms other than Windows land here.

import (
	"errors"
	"os"
)

var errLockUnsupported = errors.New("file locking is not supported on this platform")

// tryLockFile fails closed: without an OS lock, writes are refused.
func tryLockFile(*os.File) (acquired bool, err error) { return false, errLockUnsupported }

func unlockFile(*os.File) error { return errLockUnsupported }
