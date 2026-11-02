package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// protectedSegments are directory/file names that are never reachable, at any depth.
var protectedSegments = []string{".git", ".obsidian", ".obsidian-mcp"}

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
// and does not touch a protected segment (.git, .obsidian, .obsidian-mcp).
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
	lexRel, ok := s.lexicalRel(absPath)
	if !ok {
		return "", errOutside
	}
	// 2b. Windows name forms that alias another name (trailing "." or space,
	// "::$INDEX_ALLOCATION" streams, 8.3 short names) are refused before any
	// filesystem call, so they can never reach a protected folder.
	if checkWindowsNames && hasWindowsUnsafeSegment(lexRel) {
		return "", errWindowsName
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
	// errWindowsName rejects a segment Windows would read as another name.
	errWindowsName = errors.New(`path_violation: name not allowed (a ':', a trailing '.' or space, or an 8.3 short name of a protected folder like "OBSIDI~1")`)
)

// checkWindowsNames enables hasWindowsUnsafeSegment in ValidatePath.
var checkWindowsNames = runtime.GOOS == "windows"

func relInside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// lexicalRel returns abs relative to the root (resolved or as given) when
// it lies under it, using string operations only. UNC paths and other
// volumes are rejected.
func (s *SecurityLayer) lexicalRel(abs string) (string, bool) {
	if strings.HasPrefix(abs, `\\`) || strings.HasPrefix(abs, "//") {
		return "", false // UNC / device paths; never touch them
	}
	for _, root := range []string{s.root, s.rawRoot} {
		if !strings.EqualFold(filepath.VolumeName(abs), filepath.VolumeName(root)) {
			continue
		}
		if rel, err := filepath.Rel(root, abs); err == nil && relInside(rel) {
			return rel, true
		}
	}
	return "", false
}

// hasProtectedSegment reports whether a segment of rel names a protected
// folder, ignoring case and any trailing dots and spaces (which Windows
// strips, so ".git." is ".git" there). It does so on every OS.
func hasProtectedSegment(rel string) bool {
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		trimmed := strings.TrimRight(seg, ". ")
		for _, p := range protectedSegments {
			if strings.EqualFold(seg, p) || strings.EqualFold(trimmed, p) { // case-insensitive filesystems
				return true
			}
		}
	}
	return false
}

// hasWindowsUnsafeSegment reports whether a segment of rel (other than ".")
// contains ':', ends in '.' or space, or is the 8.3 short-name alias of a
// protected folder (isProtectedShortName). Windows reads each of these as
// another name.
func hasWindowsUnsafeSegment(rel string) bool {
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if seg == "" || seg == "." {
			continue
		}
		if strings.ContainsRune(seg, ':') || strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return true
		}
		if isProtectedShortName(seg) {
			return true
		}
	}
	return false
}

// isProtectedShortName reports whether seg can be the 8.3 short-name alias
// of a protected folder: Windows drops the leading dot, so ".git" is
// "GIT~1" and ".obsidian" or ".obsidian-mcp" is "OBSIDI~1". It matches,
// ignoring case, ^\.*(GIT|OBSIDI)~[0-9]+$. Other names with "~<digit>",
// such as "draft~2.md", are not aliases of a protected folder.
func isProtectedShortName(seg string) bool {
	seg = strings.TrimLeft(seg, ".")
	stem, num, ok := strings.Cut(seg, "~")
	if !ok || num == "" || !(strings.EqualFold(stem, "GIT") || strings.EqualFold(stem, "OBSIDI")) {
		return false
	}
	for i := 0; i < len(num); i++ {
		if num[i] < '0' || num[i] > '9' {
			return false
		}
	}
	return true
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
