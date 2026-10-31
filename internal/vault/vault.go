package vault

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/nemesis312/obsidian-mcp/internal/index"
)

// Vault represents an Obsidian vault with secure file operations
type Vault struct {
	root     string
	security *SecurityLayer

	// The link index is built lazily on the first graph call. idx is nil
	// until then; writers update it only once it exists.
	idxOnce sync.Once
	idxErr  error
	idx     atomic.Pointer[index.Index]
}

func New(root string) (*Vault, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("invalid vault path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid vault path: %w", err)
	}

	return &Vault{
		root:     resolved,
		security: NewSecurityLayer(resolved),
	}, nil
}

func (v *Vault) Root() string {
	return v.root
}
