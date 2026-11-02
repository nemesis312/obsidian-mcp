package vault

import (
	"os"
	"path/filepath"
)

// preWriteHook, when set by a test, runs after a write has been validated
// and before its first os.Root call, with the vault-relative path written.
var preWriteHook func(rel string)

// openRoot opens an os.Root on the vault. Every write goes through it, so a
// symlink swapped in after ValidatePath cannot lead outside the vault. The
// caller closes it.
func (v *Vault) openRoot() (*os.Root, error) {
	return os.OpenRoot(v.root)
}

// rootRel returns safeAbs, an absolute path ValidatePath returned, relative
// to the vault root with OS separators, for use with an os.Root. It is
// lexical; a path not inside the vault is errOutside.
func (v *Vault) rootRel(safeAbs string) (string, error) {
	rel, err := filepath.Rel(v.root, safeAbs)
	if err != nil || !relInside(rel) {
		return "", errOutside
	}
	return rel, nil
}

// beforeWrite runs preWriteHook for rel, when a test set it.
func beforeWrite(rel string) {
	if preWriteHook != nil {
		preWriteHook(rel)
	}
}
