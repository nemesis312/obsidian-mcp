package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"obsidian-mcp/internal/markdown"
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

// WriteNote writes content to a note (creates or overwrites)
func (v *Vault) WriteNote(path, content string) error {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(safePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write note
	if err := os.WriteFile(safePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write note: %w", err)
	}

	// Invalidate cache
	if v.cache != nil {
		v.cache.Invalidate(path)
	}

	return nil
}

// AppendToNote appends content to an existing note
func (v *Vault) AppendToNote(path, content string) error {
	existing, err := v.ReadNote(path)
	if err != nil {
		return err
	}

	updated := existing
	if !strings.HasSuffix(existing, "\n") {
		updated += "\n"
	}
	updated += content

	return v.WriteNote(path, updated)
}

// DeleteNote removes a note from the vault
func (v *Vault) DeleteNote(path string) error {
	safePath, err := v.security.ValidatePath(path)
	if err != nil {
		return err
	}

	if err := os.Remove(safePath); err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}

	// Invalidate cache
	if v.cache != nil {
		v.cache.Invalidate(path)
	}

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
