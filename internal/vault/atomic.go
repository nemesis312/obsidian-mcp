package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const defaultNoteMode fs.FileMode = 0o644

// tempCreateAttempts bounds how many temp names writeTemp tries when a name
// is already taken.
const tempCreateAttempts = 3

// errNoteExists is returned by createExclusive when target already exists.
var errNoteExists = errors.New("note already exists")

// replaceFile is the rename writeFileAtomic publishes the temp file with.
// Tests swap it to make the rename fail.
var replaceFile = renameReplace

// writeTemp writes data to a hidden temp file
// (".obsidian-mcp-<16 hex>.tmp") in targetRel's directory, through r, and
// returns its root-relative name. The caller owns cleanup on success of the
// following step.
func writeTemp(r *os.Root, targetRel string, data []byte, mode fs.FileMode) (tmpRel string, err error) {
	dir := filepath.Dir(targetRel)
	var f *os.File
	for attempt := 0; ; attempt++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		tmpRel = filepath.Join(dir, ".obsidian-mcp-"+hex.EncodeToString(b[:])+".tmp")
		f, err = r.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || attempt+1 >= tempCreateAttempts {
			return "", err
		}
	}
	fail := func(err error) (string, error) {
		f.Close()
		r.Remove(tmpRel)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		r.Remove(tmpRel)
		return "", err
	}
	_ = r.Chmod(tmpRel, mode) // best effort: not every platform has POSIX modes
	return tmpRel, nil
}

// writeFileAtomic replaces rel with data via temp file + rename, through r,
// keeping the existing file's mode (0644 for new files).
func writeFileAtomic(r *os.Root, rel string, data []byte) error {
	mode := defaultNoteMode
	if info, err := r.Stat(rel); err == nil {
		if info.IsDir() {
			return &fs.PathError{Op: "write", Path: rel, Err: errors.New("is a directory")}
		}
		mode = info.Mode().Perm()
	}
	tmp, err := writeTemp(r, rel, data, mode)
	if err != nil {
		return err
	}
	if err := replaceFile(r, tmp, rel); err != nil {
		r.Remove(tmp)
		return err
	}
	return nil
}

// createExclusive creates rel with data through r, failing with
// errNoteExists if it already exists.
func createExclusive(r *os.Root, rel string, data []byte) error {
	tmp, err := writeTemp(r, rel, data, defaultNoteMode)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)

	err = r.Link(tmp, rel)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return errNoteExists
	}

	// Hard links unavailable (e.g. some network/FAT filesystems): O_EXCL fallback.
	f, oerr := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, defaultNoteMode)
	if oerr != nil {
		if errors.Is(oerr, fs.ErrExist) {
			return errNoteExists
		}
		return oerr
	}
	if _, werr := f.Write(data); werr != nil {
		f.Close()
		r.Remove(rel)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		r.Remove(rel)
		return cerr
	}
	return nil
}
