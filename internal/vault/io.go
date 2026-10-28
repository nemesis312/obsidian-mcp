package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// ListFiles returns all markdown files in the vault
func (v *Vault) ListFiles() ([]string, error) {
	var files []string

	err := filepath.Walk(v.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .obsidian directory
		if info.IsDir() && info.Name() == ".obsidian" {
			return filepath.SkipDir
		}

		// Skip hidden files/directories
		if strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Collect .md files
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			relPath, err := filepath.Rel(v.root, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
}

// ListCanvases returns all .canvas files in the vault
func (v *Vault) ListCanvases() ([]string, error) {
	var canvases []string

	err := filepath.Walk(v.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && (info.Name() == ".obsidian" || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		if !info.IsDir() && strings.HasSuffix(path, ".canvas") {
			relPath, err := filepath.Rel(v.root, path)
			if err != nil {
				return err
			}
			canvases = append(canvases, relPath)
		}

		return nil
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

func (v *Vault) invalidate(safePath string) {
	if v.cache != nil {
		v.cache.Invalidate(safePath)
	}
}

// WriteNote writes content to a note (creates or overwrites) atomically.
func (v *Vault) WriteNote(path, content string) error {
	safePath, err := v.resolveNote(path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := writeFileAtomic(safePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write note: %w", err)
	}

	v.invalidate(safePath)
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

	v.invalidate(safePath)
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
	safePath, err := v.resolveNote(path)
	if err != nil {
		return err
	}

	if info, err := os.Stat(safePath); err == nil && info.IsDir() {
		return fmt.Errorf("failed to delete note: %s is a directory", path)
	}

	if err := os.Remove(safePath); err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}

	v.invalidate(safePath)
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
