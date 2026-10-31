package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// VersionOf returns the version token of a note's raw bytes: "sha256:"
// followed by the first 16 lowercase hex characters of their SHA-256. Any
// byte change, line endings or a BOM included, gives a new token.
func VersionOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:8])
}

// ErrVersionRequired is returned when a whole-note replace or a delete is
// called without an expected version.
var ErrVersionRequired = errors.New(`expected_version is required to replace or delete a whole note; read it with get_note and pass its "version"`)

// errBadVersion rejects an expected version that is not a token VersionOf
// could have produced.
var errBadVersion = errors.New("invalid expected_version: want sha256:<16 hex>")

// ConflictError reports that a note changed since the caller read it. Path
// is vault-relative with "/"; Current is the note's version now.
type ConflictError struct{ Path, Current string }

func (e *ConflictError) Error() string {
	return "version conflict: " + e.Path + " changed since expected_version; current version: " + e.Current + "; re-read the note and retry"
}

// checkVersion validates the form of a non-empty expected version.
func checkVersion(expected string) error {
	hexPart, ok := strings.CutPrefix(expected, "sha256:")
	if !ok || len(hexPart) != 16 {
		return errBadVersion
	}
	for i := 0; i < len(hexPart); i++ {
		if c := hexPart[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return errBadVersion
		}
	}
	return nil
}

// NoteSnapshot is a note's vault-relative "/" path, its full content and
// that content's version token.
type NoteSnapshot struct{ Path, Content, Version string }

func snapshot(rel, content string) NoteSnapshot {
	return NoteSnapshot{Path: rel, Content: content, Version: VersionOf([]byte(content))}
}

// ReadNoteVersioned reads the note arg addresses (see AddressNote) and
// returns it with its canonical path and version token.
func (v *Vault) ReadNoteVersioned(arg string) (NoteSnapshot, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return NoteSnapshot{}, err
	}
	content, err := v.ReadNote(rel)
	if err != nil {
		return NoteSnapshot{}, notFoundAs(arg, err)
	}
	return snapshot(rel, content), nil
}

// notFoundAs reports a missing file by the caller's argument.
func notFoundAs(arg string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &notFoundError{arg: arg}
	}
	return err
}

// writeLockHook, when set by a test, runs just before a modifying method
// takes the write guard.
var writeLockHook func()

// addressForWrite addresses an existing note for a modifying method and
// checks that it is a writable markdown note (containment, protected
// folders, .md symlink target) before any lock is taken.
func (v *Vault) addressForWrite(arg string) (string, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return "", err
	}
	if _, err := v.resolveNote(rel); err != nil {
		return "", err
	}
	return rel, nil
}

// modifyNote is the guarded read-check-write cycle shared by the modifying
// methods: it takes the write guard, re-reads rel from disk, compares its
// version with expected (when not empty), and writes edit's result. The
// guard is released before the index is updated.
func (v *Vault) modifyNote(arg, rel, expected string, edit func(current string) (string, error)) (NoteSnapshot, error) {
	snap, err := func() (NoteSnapshot, error) {
		if writeLockHook != nil {
			writeLockHook()
		}
		unlock, err := v.guard.lock()
		if err != nil {
			return NoteSnapshot{}, err
		}
		defer unlock()
		current, err := v.ReadNote(rel)
		if err != nil {
			return NoteSnapshot{}, notFoundAs(arg, err)
		}
		if expected != "" {
			if cur := VersionOf([]byte(current)); cur != expected {
				return NoteSnapshot{}, &ConflictError{Path: rel, Current: cur}
			}
		}
		updated, err := edit(current)
		if err != nil {
			return NoteSnapshot{}, err
		}
		if err := v.writeNoteLocked(rel, updated); err != nil {
			return NoteSnapshot{}, err
		}
		return snapshot(rel, updated), nil
	}()
	if err != nil {
		return NoteSnapshot{}, err
	}
	v.indexUpdate(rel)
	return snap, nil
}

// CreateNote creates a new note, failing if it already exists. Frontmatter
// (if any) is merged into content before the single write. It takes no
// version token: exclusive creation is its guard.
//
// Exclusivity: the full content is first written to a temp file in the same
// directory, then published with os.Link, which fails with EEXIST atomically.
// Readers therefore never observe a partially written note and concurrent
// creators cannot both succeed. If the filesystem has no hard links, it falls
// back to open(O_CREATE|O_EXCL) on the final path (still race-free for
// exclusivity, but not for partial reads).
func (v *Vault) CreateNote(path, content string, frontmatter map[string]any) (NoteSnapshot, error) {
	rel, err := v.relArg(path)
	if err != nil {
		return NoteSnapshot{}, err
	}
	norm, err := NormalizeNotePath(rel)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if _, err := v.resolveNote(norm); err != nil {
		return NoteSnapshot{}, err
	}
	out, ok := v.noteRel(norm)
	if !ok {
		return NoteSnapshot{}, errInvalid
	}

	if len(frontmatter) > 0 {
		content, err = markdown.UpdateFrontmatter(content, frontmatter)
		if err != nil {
			return NoteSnapshot{}, fmt.Errorf("failed to add frontmatter: %w", err)
		}
	}

	err = func() error {
		if writeLockHook != nil {
			writeLockHook()
		}
		unlock, err := v.guard.lock()
		if err != nil {
			return err
		}
		defer unlock()
		// Resolve again under the guard: a symlinked folder may have been
		// retargeted while this call waited.
		safePath, err := v.resolveNote(norm)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
			return v.fsError("create directory for", out, err)
		}
		if err := createExclusive(safePath, []byte(content)); err != nil {
			if errors.Is(err, errNoteExists) {
				return err
			}
			return v.fsError("create note", out, err)
		}
		return nil
	}()
	if err != nil {
		return NoteSnapshot{}, err
	}
	v.indexUpdate(out)
	return snapshot(out, content), nil
}

// AppendToNote appends content to the note arg addresses, after a newline
// when the note does not end with one. expected is optional: when given,
// the note must still have that version.
func (v *Vault) AppendToNote(arg, content, expected string) (NoteSnapshot, error) {
	rel, err := v.addressForWrite(arg)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if expected != "" {
		if err := checkVersion(expected); err != nil {
			return NoteSnapshot{}, err
		}
	}
	return v.modifyNote(arg, rel, expected, func(current string) (string, error) {
		if !strings.HasSuffix(current, "\n") {
			current += "\n"
		}
		return current + content, nil
	})
}

// ReplaceNote replaces the whole content of the existing note arg
// addresses. expected is required and must be the note's current version.
// A missing note is "note not found"; it is never created.
func (v *Vault) ReplaceNote(arg, content, expected string) (NoteSnapshot, error) {
	rel, err := v.addressForWrite(arg)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if expected == "" {
		return NoteSnapshot{}, ErrVersionRequired
	}
	if err := checkVersion(expected); err != nil {
		return NoteSnapshot{}, err
	}
	return v.modifyNote(arg, rel, expected, func(string) (string, error) { return content, nil })
}

// UpdateFrontmatter merges updates into the frontmatter of the note arg
// addresses, keeping the body. expected is optional.
func (v *Vault) UpdateFrontmatter(arg string, updates map[string]any, expected string) (NoteSnapshot, error) {
	rel, err := v.addressForWrite(arg)
	if err != nil {
		return NoteSnapshot{}, err
	}
	if expected != "" {
		if err := checkVersion(expected); err != nil {
			return NoteSnapshot{}, err
		}
	}
	return v.modifyNote(arg, rel, expected, func(current string) (string, error) {
		return markdown.UpdateFrontmatter(current, updates)
	})
}

// DeleteNote deletes the note arg addresses and returns its path. expected
// is required and must be the note's current version.
func (v *Vault) DeleteNote(arg, expected string) (string, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return "", err
	}
	if _, err := v.resolveNote(rel); err != nil {
		return "", err
	}
	if expected == "" {
		return "", ErrVersionRequired
	}
	if err := checkVersion(expected); err != nil {
		return "", err
	}
	err = func() error {
		if writeLockHook != nil {
			writeLockHook()
		}
		unlock, err := v.guard.lock()
		if err != nil {
			return err
		}
		defer unlock()
		current, err := v.ReadNote(rel)
		if err != nil {
			return notFoundAs(arg, err)
		}
		if cur := VersionOf([]byte(current)); cur != expected {
			return &ConflictError{Path: rel, Current: cur}
		}
		// Resolve again under the guard, so the file removed is the one
		// whose version was just checked, not the target rel had before
		// this call waited (a symlink may have been retargeted).
		safePath, err := v.resolveNote(rel)
		if err != nil {
			return err
		}
		if err := removeFile(safePath); err != nil {
			return notFoundAs(arg, v.fsError("delete note", rel, err))
		}
		return nil
	}()
	if err != nil {
		return "", err
	}
	v.indexRemove(rel)
	return rel, nil
}
