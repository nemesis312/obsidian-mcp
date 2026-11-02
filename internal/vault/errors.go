package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// notFoundError reports a missing note by the caller's argument, with the
// closest note paths when there are any. It wraps fs.ErrNotExist so
// errors.Is keeps working (the link index relies on it).
type notFoundError struct {
	arg         string
	suggestions []string
}

func (e *notFoundError) Error() string {
	if len(e.suggestions) == 0 {
		return "note not found: " + e.arg
	}
	return "note not found: " + e.arg + "; did you mean: " + strings.Join(e.suggestions, ", ") + "?"
}
func (e *notFoundError) Unwrap() error { return fs.ErrNotExist }

// fsError maps an error from a file system call made for arg to one that
// never contains the absolute vault root: "note not found: <arg>" when the
// file does not exist, otherwise "<op> <vault-relative path>: <cause>". The
// path inside a *fs.PathError or *os.LinkError is made vault-relative, or
// replaced by arg when it is not inside the vault.
func (v *Vault) fsError(op, arg string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return &notFoundError{arg: arg}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s %s: %w", op, v.relOr(pe.Path, arg), pe.Err)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("%s %s: %w", op, v.relOr(le.New, arg), le.Err)
	}
	return fmt.Errorf("%s %s: %w", op, arg, err)
}

// relOr returns p as a vault-relative "/" path, or arg when p is not inside
// the vault. A relative p (from an os.Root call) is already vault-relative.
func (v *Vault) relOr(p, arg string) string {
	if !filepath.IsAbs(p) {
		if p == "" || filepath.VolumeName(p) != "" {
			return arg
		}
		return filepath.ToSlash(p)
	}
	rel, err := filepath.Rel(v.root, p)
	if err != nil || !relInside(rel) {
		return arg
	}
	return filepath.ToSlash(rel)
}
