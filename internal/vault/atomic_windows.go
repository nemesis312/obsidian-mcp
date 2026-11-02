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

// renameReplace atomically replaces newRel with oldRel, through r, retrying
// while newRel is held open by another handle.
func renameReplace(r *os.Root, oldRel, newRel string) error {
	return retryShared(func() error { return r.Rename(oldRel, newRel) })
}

// removeFile removes the file at rel, through r, retrying while it is held
// open by another handle.
func removeFile(r *os.Root, rel string) error {
	return retryShared(func() error { return r.Remove(rel) })
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
