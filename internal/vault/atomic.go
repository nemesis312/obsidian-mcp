package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const defaultNoteMode fs.FileMode = 0o644

// writeTemp writes data to a hidden temp file next to target and returns its
// path. The caller owns cleanup on success of the following step.
func writeTemp(target string, data []byte, mode fs.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(target), ".obsidian-mcp-*.tmp")
	if err != nil {
		return "", err
	}
	name := f.Name()
	fail := func(err error) (string, error) {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	_ = f.Chmod(mode) // best effort: not every platform has POSIX modes
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// writeFileAtomic replaces target with data via temp file + rename, keeping the
// existing file's mode (0644 for new files).
func writeFileAtomic(target string, data []byte) error {
	mode := defaultNoteMode
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", target)
		}
		mode = info.Mode().Perm()
	}
	tmp, err := writeTemp(target, data, mode)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// createExclusive creates target with data, failing if it already exists.
func createExclusive(target string, data []byte) error {
	tmp, err := writeTemp(target, data, defaultNoteMode)
	if err != nil {
		return fmt.Errorf("failed to create note: %w", err)
	}
	defer os.Remove(tmp)

	err = os.Link(tmp, target)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("note already exists")
	}

	// Hard links unavailable (e.g. some network/FAT filesystems): O_EXCL fallback.
	f, oerr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, defaultNoteMode)
	if oerr != nil {
		if errors.Is(oerr, fs.ErrExist) {
			return fmt.Errorf("note already exists")
		}
		return fmt.Errorf("failed to create note: %w", oerr)
	}
	if _, werr := f.Write(data); werr != nil {
		f.Close()
		os.Remove(target)
		return fmt.Errorf("failed to create note: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(target)
		return fmt.Errorf("failed to create note: %w", cerr)
	}
	return nil
}
