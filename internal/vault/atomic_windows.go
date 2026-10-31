//go:build windows

package vault

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Retry bounds for renames and removes that fail because another process
// (an editor, a sync client, an antivirus, another reader) has the file
// open without FILE_SHARE_DELETE.
const (
	shareRetryMin   = 10 * time.Millisecond
	shareRetryMax   = 100 * time.Millisecond
	shareRetryLimit = 2 * time.Second
)

// renameReplace atomically replaces newpath with oldpath, retrying while
// newpath is held open by another handle.
func renameReplace(oldpath, newpath string) error {
	return retryShared(func() error { return os.Rename(oldpath, newpath) })
}

// removeFile removes the file at name, retrying while it is held open by
// another handle.
func removeFile(name string) error {
	return retryShared(func() error { return os.Remove(name) })
}

// retryShared runs op until it succeeds, fails with an error other than
// ERROR_ACCESS_DENIED or ERROR_SHARING_VIOLATION, or 2 s have passed, with
// a backoff doubling from 10 ms to 100 ms. The last error is returned.
func retryShared(op func() error) error {
	deadline := time.Now().Add(shareRetryLimit)
	backoff := shareRetryMin
	for {
		err := op()
		if err == nil || !isSharingError(err) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		time.Sleep(min(backoff, remaining))
		backoff = min(backoff*2, shareRetryMax)
	}
}

func isSharingError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
