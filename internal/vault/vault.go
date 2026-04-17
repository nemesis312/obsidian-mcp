package vault

import (
	"fmt"
	"path/filepath"
	"time"
)

// Vault represents an Obsidian vault with secure file operations
type Vault struct {
	root     string
	security *SecurityLayer
	cache    *Cache
}

func New(root string) (*Vault, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("invalid vault path: %w", err)
	}

	return &Vault{
		root:     absRoot,
		security: NewSecurityLayer(absRoot),
		cache:    NewCache(60 * time.Second), // 60s TTL
	}, nil
}

func (v *Vault) Root() string {
	return v.root
}
