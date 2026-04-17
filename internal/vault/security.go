package vault

import (
	"fmt"
	"path/filepath"
	"strings"
)

type SecurityLayer struct {
	root string // absolute vault root
}

func NewSecurityLayer(root string) *SecurityLayer {
	return &SecurityLayer{root: root}
}

// ValidatePath ensures target path is within vault and not a protected directory
// Returns absolute path if valid, error otherwise
func (s *SecurityLayer) ValidatePath(targetPath string) (string, error) {
	// 1. Convert to absolute path
	var absPath string
	if filepath.IsAbs(targetPath) {
		absPath = filepath.Clean(targetPath)
	} else {
		absPath = filepath.Clean(filepath.Join(s.root, targetPath))
	}

	// 2. Ensure within vault root
	if !strings.HasPrefix(absPath, s.root+string(filepath.Separator)) && absPath != s.root {
		return "", fmt.Errorf("path_violation: path outside vault: %s", absPath)
	}

	// 3. Get relative path for protected directory checks
	relPath, err := filepath.Rel(s.root, absPath)
	if err != nil {
		return "", fmt.Errorf("path_violation: cannot compute relative path: %w", err)
	}

	// 4. Reject protected directories
	if strings.HasPrefix(relPath, ".obsidian") ||
		strings.Contains(relPath, string(filepath.Separator)+".obsidian"+string(filepath.Separator)) ||
		strings.HasPrefix(relPath, ".git") ||
		strings.Contains(relPath, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return "", fmt.Errorf("path_violation: protected directory: %s", relPath)
	}

	// 5. Reject upward traversal attempts (.. in relative path after cleaning)
	if strings.Contains(relPath, "..") {
		return "", fmt.Errorf("path_violation: traversal attempt: %s", relPath)
	}

	return absPath, nil
}
