package vault

import (
	"errors"
	"fmt"
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
// fn gets each non-directory entry with its vault-relative "/" path. Walk
// errors never contain the absolute vault root.
func (v *Vault) walkVault(fn func(rel string, info os.FileInfo)) error {
	return filepath.Walk(v.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if p != v.root && errors.Is(err, fs.ErrNotExist) {
				return nil // removed while walking
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
// note are not detected until the link itself changes.
func (v *Vault) StatNotes() ([]index.FileStat, error) {
	var out []index.FileStat
	err := v.walkVault(func(rel string, info os.FileInfo) {
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

// ReadNote reads a markdown note by path
func (v *Vault) ReadNote(path string) (string, error) {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(safePath)
	if err != nil {
		return "", fmt.Errorf("failed to read note: %w", err)
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

// WriteNote writes content to a note (creates or overwrites) atomically.
func (v *Vault) WriteNote(path, content string) error {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return err
	}
	safePath, err := v.resolveNote(norm)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := writeFileAtomic(safePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write note: %w", err)
	}

	v.indexUpdate(norm)
	return nil
}

// CreateNote creates a new note, failing if it already exists. Frontmatter (if
// any) is merged into content before the single write. It returns the
// normalized vault path of the note.
//
// Exclusivity: the full content is first written to a temp file in the same
// directory, then published with os.Link, which fails with EEXIST atomically.
// Readers therefore never observe a partially written note and concurrent
// creators cannot both succeed. If the filesystem has no hard links, it falls
// back to open(O_CREATE|O_EXCL) on the final path (still race-free for
// exclusivity, but not for partial reads).
func (v *Vault) CreateNote(path, content string, frontmatter map[string]interface{}) (string, error) {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return "", err
	}
	safePath, err := v.resolveNote(norm)
	if err != nil {
		return "", err
	}

	if len(frontmatter) > 0 {
		content, err = markdown.UpdateFrontmatter(content, frontmatter)
		if err != nil {
			return "", fmt.Errorf("failed to add frontmatter: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := createExclusive(safePath, []byte(content)); err != nil {
		return "", err
	}

	v.indexUpdate(norm)
	return norm, nil
}

// AppendToNote appends content to an existing note
func (v *Vault) AppendToNote(path, content string) error {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return err
	}
	existing, err := v.ReadNote(norm)
	if err != nil {
		return err
	}

	updated := existing
	if !strings.HasSuffix(existing, "\n") {
		updated += "\n"
	}
	updated += content

	return v.WriteNote(norm, updated)
}

// DeleteNote removes a note from the vault
func (v *Vault) DeleteNote(path string) error {
	norm, err := NormalizeNotePath(path)
	if err != nil {
		return err
	}
	safePath, err := v.resolveNote(norm)
	if err != nil {
		return err
	}

	if info, err := os.Stat(safePath); err == nil && info.IsDir() {
		return fmt.Errorf("failed to delete note: %s is a directory", path)
	}

	if err := os.Remove(safePath); err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}

	v.indexRemove(norm)
	return nil
}

// ReadCanvas reads a canvas file
func (v *Vault) ReadCanvas(path string) (*markdown.Canvas, error) {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read canvas: %w", err)
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

// GetFrontmatter extracts frontmatter from a note
func (v *Vault) GetFrontmatter(path string) (map[string]interface{}, error) {
	content, err := v.ReadNote(path)
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

// UpdateFrontmatter updates frontmatter fields in a note
func (v *Vault) UpdateFrontmatter(path string, updates map[string]interface{}) error {
	path, err := NormalizeNotePath(path)
	if err != nil {
		return err
	}
	content, err := v.ReadNote(path)
	if err != nil {
		return err
	}

	updated, err := markdown.UpdateFrontmatter(content, updates)
	if err != nil {
		return err
	}

	return v.WriteNote(path, updated)
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
