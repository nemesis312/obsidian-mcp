package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/index"
	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

var _ index.Source = (*Vault)(nil)

// walkVault walks the vault without following symlinks (info is from Lstat),
// skipping every hidden file or directory, which covers .obsidian and .git.
// fn gets each non-directory entry with its vault-relative "/" path. A
// subdirectory that cannot be read or searched for lack of permission is
// skipped; any error on the root, including an entry of the root that cannot
// be stat'ed, fails the walk. Walk errors never contain the absolute vault
// root.
func (v *Vault) walkVault(fn func(rel string, info os.FileInfo)) error {
	return filepath.Walk(v.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if p != v.root && errors.Is(err, fs.ErrNotExist) {
				return nil // removed while walking
			}
			if p != v.root && errors.Is(err, fs.ErrPermission) && (info != nil && info.IsDir() || filepath.Dir(p) != v.root) {
				// An unreadable subdirectory (info is the dir), or an entry
				// of a non-searchable one that cannot be stat'ed (info is
				// nil): filepath.Walk drops that entry and goes on. The
				// parent is never the root: a non-searchable root fails.
				return filepath.SkipDir
			}
			return v.walkError(p, err)
		}
		if p == v.root {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil || !relInside(rel) {
			return v.walkError(p, errInvalid)
		}
		fn(filepath.ToSlash(rel), info)
		return nil
	})
}

// walkError reports a walk failure with a vault-relative path only.
func (v *Vault) walkError(p string, err error) error {
	rel, rerr := filepath.Rel(v.root, p)
	if rerr != nil || !relInside(rel) {
		rel = "."
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Errorf("vault walk failed at %s: %w", filepath.ToSlash(rel), err)
}

// hasExtFold reports whether rel ends in ext, ignoring case, as the link
// resolver does.
func hasExtFold(rel, ext string) bool {
	return len(rel) >= len(ext) && strings.EqualFold(rel[len(rel)-len(ext):], ext)
}

func isNotePath(rel string) bool { return hasExtFold(rel, ".md") }

func isCanvasPath(rel string) bool { return hasExtFold(rel, ".canvas") }

// ListFiles returns all markdown files in the vault, as vault-relative paths
// using "/".
func (v *Vault) ListFiles() ([]string, error) {
	var files []string
	err := v.walkVault(func(rel string, _ os.FileInfo) {
		if isNotePath(rel) {
			files = append(files, rel)
		}
	})
	return files, err
}

// StatNotes implements index.Source: the same set as ListFiles, with stat data.
// Size and ModTime come from Lstat, so edits to the target of a symlinked
// note are not detected until the link itself changes. Entries that are
// neither regular files nor symlinks (FIFOs, sockets, devices) are skipped.
func (v *Vault) StatNotes() ([]index.FileStat, error) {
	var out []index.FileStat
	err := v.walkVault(func(rel string, info os.FileInfo) {
		if m := info.Mode(); !m.IsRegular() && m&fs.ModeSymlink == 0 {
			return
		}
		if isNotePath(rel) {
			out = append(out, index.FileStat{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		}
	})
	return out, err
}

// StatAttachments implements index.Source: every regular file that is not a
// note or a canvas. Symlinks are skipped entirely, so nothing outside the
// vault is ever listed.
func (v *Vault) StatAttachments() ([]index.FileStat, error) {
	var out []index.FileStat
	err := v.walkVault(func(rel string, info os.FileInfo) {
		if !info.Mode().IsRegular() || isNotePath(rel) || isCanvasPath(rel) {
			return
		}
		out = append(out, index.FileStat{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
	})
	return out, err
}

// ListCanvases returns all .canvas files in the vault, as vault-relative
// paths using "/".
func (v *Vault) ListCanvases() ([]string, error) {
	var canvases []string
	err := v.walkVault(func(rel string, _ os.FileInfo) {
		if isCanvasPath(rel) {
			canvases = append(canvases, rel)
		}
	})
	return canvases, err
}

// readNoteHook, when set by a test, runs between validation and open.
var readNoteHook func()

// ReadNote reads a markdown note by path. After ValidatePath, the file is
// opened through an os.Root on the vault, so a symlink swapped in after the
// check cannot lead outside the vault, and only a regular file is read (a
// FIFO named *.md is rejected without blocking).
func (v *Vault) ReadNote(path string) (string, error) {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return "", err
	}
	if readNoteHook != nil {
		readNoteHook()
	}
	rel, err := filepath.Rel(v.security.root, safePath)
	if err != nil || !relInside(rel) {
		return "", errOutside
	}

	root, err := os.OpenRoot(v.security.root)
	if err != nil {
		return "", v.fsError("read note", path, err)
	}
	defer root.Close()
	f, err := root.OpenFile(rel, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		return "", v.fsError("read note", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", v.fsError("read note", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("read note %s: not a regular file", filepath.ToSlash(rel))
	}
	content, err := io.ReadAll(f)
	if err != nil {
		return "", v.fsError("read note", path, err)
	}
	return string(content), nil
}

// NormalizeNotePath enforces that write operations only target markdown notes.
// A path without extension gets ".md" appended; any other extension is an error.
func NormalizeNotePath(path string) (string, error) {
	if path == "" || strings.HasSuffix(path, "/") {
		return "", fmt.Errorf("invalid note path: %q", path)
	}
	switch ext := filepath.Ext(path); {
	case ext == "":
		return path + ".md", nil
	case strings.EqualFold(ext, ".md"):
		return path, nil
	default:
		return "", fmt.Errorf("only .md notes are supported (got %q extension)", ext)
	}
}

// resolveNote normalizes the extension and validates the path against the vault.
func (v *Vault) resolveNote(path string) (string, error) {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return "", err
	}
	safe, err := v.security.ValidatePath(norm)
	if err != nil {
		return "", err
	}
	return safe, requireMarkdownTarget(safe)
}

// requireMarkdownTarget ensures the symlink-resolved target is a .md file, so
// a "note.md" symlink cannot be used to overwrite or delete e.g. a script.
func requireMarkdownTarget(resolved string) error {
	if !strings.EqualFold(filepath.Ext(resolved), ".md") {
		return fmt.Errorf("path_violation: note resolves to a non-markdown file")
	}
	return nil
}

// noteRel returns the vault-relative "/" path of a validated note path. It
// is lexical, so a symlinked note keeps its own path, as the walk lists it.
func (v *Vault) noteRel(norm string) (string, bool) {
	abs := norm
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(v.root, norm)
	}
	rel, err := filepath.Rel(v.root, filepath.Clean(abs))
	if err != nil || !relInside(rel) || rel == "." {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// indexUpdate refreshes one note in the link index, if it was built. If it
// was not, the lazy Build sees the write. An Update error is ignored: the
// next Refresh heals it.
func (v *Vault) indexUpdate(norm string) {
	ix := v.idx.Load()
	if ix == nil {
		return
	}
	if rel, ok := v.noteRel(norm); ok {
		_ = ix.Update(rel)
	}
}

// indexRemove drops one note from the link index, if it was built.
func (v *Vault) indexRemove(norm string) {
	ix := v.idx.Load()
	if ix == nil {
		return
	}
	if rel, ok := v.noteRel(norm); ok {
		ix.Remove(rel)
	}
}

// writeNoteLocked writes content to the note at path (created or
// overwritten) atomically. The caller holds the write guard and updates the
// index after releasing it.
func (v *Vault) writeNoteLocked(path, content string) error {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return err
	}
	safePath, err := v.resolveNote(norm)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
		return v.fsError("create directory for", norm, err)
	}

	if err := writeFileAtomic(safePath, []byte(content)); err != nil {
		return v.fsError("write note", norm, err)
	}
	return nil
}

// ReadCanvas reads a canvas file
func (v *Vault) ReadCanvas(path string) (*markdown.Canvas, error) {
	path, err := v.relArg(path)
	if err != nil {
		return nil, err
	}
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(safePath)
	if err != nil {
		return nil, v.fsError("read canvas", path, err)
	}

	return markdown.ParseCanvas(content)
}

// SearchVault searches for notes containing a query string
func (v *Vault) SearchVault(query string) ([]string, error) {
	files, err := v.ListFiles()
	if err != nil {
		return nil, err
	}

	var matches []string
	lowerQuery := strings.ToLower(query)

	for _, file := range files {
		content, err := v.ReadNote(file)
		if err != nil {
			continue // Skip files we can't read
		}

		if strings.Contains(strings.ToLower(content), lowerQuery) {
			matches = append(matches, file)
		}
	}

	return matches, nil
}

// GetFrontmatter extracts frontmatter from the note arg addresses (see
// AddressNote).
func (v *Vault) GetFrontmatter(arg string) (map[string]interface{}, error) {
	rel, err := v.AddressNote(arg)
	if err != nil {
		return nil, err
	}
	content, err := v.ReadNote(rel)
	if err != nil {
		return nil, err
	}

	fm, _, err := markdown.ParseFrontmatter(content)
	if err != nil {
		return nil, err
	}

	if fm == nil {
		return make(map[string]interface{}), nil
	}

	return fm.Data, nil
}

// NoteExists checks if a note exists
func (v *Vault) NoteExists(path string) bool {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return false
	}

	info, err := os.Stat(safePath)
	return err == nil && !info.IsDir()
}

// ResolvePath resolves a note name to a full path
func (v *Vault) ResolvePath(name string) (string, error) {
	// If already ends with .md, try as-is
	if strings.HasSuffix(name, ".md") {
		if v.NoteExists(name) {
			return name, nil
		}
	}

	// Try adding .md
	withExt := name
	if !strings.HasSuffix(name, ".md") {
		withExt = name + ".md"
	}

	if v.NoteExists(withExt) {
		return withExt, nil
	}

	return "", fmt.Errorf("note not found: %s", name)
}
