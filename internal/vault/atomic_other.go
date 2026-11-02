//go:build !windows

package vault

import "os"

// renameReplace atomically replaces newRel with oldRel, through r.
func renameReplace(r *os.Root, oldRel, newRel string) error { return r.Rename(oldRel, newRel) }

// removeFile removes the file at rel, through r.
func removeFile(r *os.Root, rel string) error { return r.Remove(rel) }
