package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// protectedSegments are directory/file names that are never reachable, at any depth.
var protectedSegments = []string{".git", ".obsidian"}

type SecurityLayer struct {
	root    string // absolute vault root with symlinks resolved
	rawRoot string // cleaned root as given (may be a symlink alias of root)
}

// NewSecurityLayer builds a layer for root. The root is resolved with
// filepath.EvalSymlinks when possible so containment checks compare like with like.
func NewSecurityLayer(root string) *SecurityLayer {
	raw := filepath.Clean(root)
	resolved := raw
	if r, err := filepath.EvalSymlinks(raw); err == nil {
		resolved = r
	}
	return &SecurityLayer{root: resolved, rawRoot: raw}
}

// ValidatePath ensures target path is within the vault (after resolving symlinks)
// and does not touch a protected segment (.git, .obsidian).
// Returns the resolved absolute path if valid, error otherwise.
func (s *SecurityLayer) ValidatePath(targetPath string) (string, error) {
	// 0. A volume name without an absolute path ("Z:x.md", Windows drive-relative)
	// is neither inside the vault nor safely joinable: reject before any join or FS call.
	if filepath.VolumeName(targetPath) != "" && !filepath.IsAbs(targetPath) {
		return "", errOutside
	}

	// 1. Convert to absolute, cleaned path
	var absPath string
	if filepath.IsAbs(targetPath) {
		absPath = filepath.Clean(targetPath)
	} else {
		absPath = filepath.Clean(filepath.Join(s.root, targetPath))
	}

	// 2. Lexical containment BEFORE any filesystem call, so paths outside the
	// vault (UNC shares, other volumes, "..") are never stat'ed or resolved.
	if !s.lexicallyInside(absPath) {
		return "", errOutside
	}

	// 3. Resolve symlinks (deepest existing ancestor for paths that don't exist
	// yet) and re-check containment against the resolved root.
	resolved, err := resolveExisting(absPath)
	if err != nil {
		return "", errInvalid
	}
	relPath, err := filepath.Rel(s.root, resolved)
	if err != nil || !relInside(relPath) {
		return "", errOutside
	}

	// 4. Reject protected segments, on both the lexical and the resolved path
	// (so a symlink pointing into .obsidian is caught too).
	if lex, err := filepath.Rel(s.root, absPath); err == nil && hasProtectedSegment(lex) {
		return "", fmt.Errorf("path_violation: protected directory: %s", lex)
	}
	if hasProtectedSegment(relPath) {
		return "", fmt.Errorf("path_violation: protected directory: %s", relPath)
	}

	return resolved, nil
}

var (
	errOutside = errors.New("path_violation: path is outside the vault")
	errInvalid = errors.New("path_violation: invalid path")
)

func relInside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// lexicallyInside reports whether abs lies under the root (resolved or as
// given) using string operations only. UNC paths and other volumes are rejected.
func (s *SecurityLayer) lexicallyInside(abs string) bool {
	if strings.HasPrefix(abs, `\\`) || strings.HasPrefix(abs, "//") {
		return false // UNC / device paths; never touch them
	}
	for _, root := range []string{s.root, s.rawRoot} {
		if !strings.EqualFold(filepath.VolumeName(abs), filepath.VolumeName(root)) {
			continue
		}
		if rel, err := filepath.Rel(root, abs); err == nil && relInside(rel) {
			return true
		}
	}
	return false
}

func hasProtectedSegment(rel string) bool {
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		for _, p := range protectedSegments {
			if strings.EqualFold(seg, p) { // case-insensitive filesystems
				return true
			}
		}
	}
	return false
}

// resolveExisting resolves symlinks in p. If p (or a suffix of it) does not
// exist, the deepest existing ancestor is resolved and the missing tail is
// appended unchanged. A dangling symlink is an error: writing "through" it
// would create its target, wherever that is.
func resolveExisting(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, lerr := os.Lstat(p); lerr == nil {
		return "", fmt.Errorf("dangling symlink: %s", p)
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", err
	}
	resolvedParent, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(p)), nil
}
