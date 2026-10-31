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
	// guard serializes writes to this vault across goroutines and processes.
	guard *writeGuard

	// The link index is built lazily on the first graph call. idx is nil
	// until a Build succeeds; writers update it only once it exists. idxMu
	// serializes Build attempts; a failed attempt is not remembered.
	idxMu sync.Mutex
	idx   atomic.Pointer[index.Index]
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
		guard:    newWriteGuard(resolved, defaultLockTimeout),
	}, nil
}

func (v *Vault) Root() string {
	return v.root
}
