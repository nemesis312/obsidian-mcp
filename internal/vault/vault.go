package vault

import (
	"fmt"
	"path/filepath"
)

// Vault represents an Obsidian vault with secure file operations
type Vault struct {
	root     string
	security *SecurityLayer
}

func New(root string) (*Vault, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("invalid vault path: %w", err)
	}

	return &Vault{
		root:     absRoot,
		security: NewSecurityLayer(absRoot),
	}, nil
}

func (v *Vault) Root() string {
	return v.root
}
