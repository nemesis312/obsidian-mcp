//go:build !windows

package vault

import "os"

// renameReplace atomically replaces newpath with oldpath.
func renameReplace(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

// removeFile removes the file at name.
func removeFile(name string) error { return os.Remove(name) }
