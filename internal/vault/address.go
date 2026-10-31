package vault

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

// ErrAbsolutePath rejects an absolute filesystem path given as a note argument.
var ErrAbsolutePath = errors.New("absolute paths are not accepted: pass a vault-relative path or a note name")

// AmbiguousNoteError reports a note name that matches several notes.
// Candidates are sorted; addressing never picks one of them.
type AmbiguousNoteError struct {
	Arg        string
	Candidates []string
}

// maxListedCandidates bounds the candidates an AmbiguousNoteError lists.
const maxListedCandidates = 10

func (e *AmbiguousNoteError) Error() string {
	shown := e.Candidates
	more := ""
	if len(shown) > maxListedCandidates {
		more = fmt.Sprintf(", … (%d more)", len(shown)-maxListedCandidates)
		shown = shown[:maxListedCandidates]
	}
	return fmt.Sprintf("ambiguous note name %q: matches %s%s; pass one of these paths", e.Arg, strings.Join(shown, ", "), more)
}

const (
	maxSuggestions    = 5
	maxSuggestQueryRn = 128
)

// foldRootCase reports whether this OS compares paths ignoring case.
var foldRootCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// relArg applies the argument policy shared by every tool that takes a path:
// rejects an absolute path inside the vault root (either spelling, case-folded
// on darwin/windows), any Windows volume or UNC path, and NUL; strips leading
// "/" or "\" (= vault root); converts "\" to "/".
func (v *Vault) relArg(arg string) (string, error) {
	if strings.IndexByte(arg, 0) >= 0 {
		return "", errInvalid
	}
	if filepath.VolumeName(arg) != "" {
		return "", ErrAbsolutePath // "C:\x", "C:x", `\\srv\share` (Windows only)
	}
	s := strings.ReplaceAll(arg, `\`, "/")
	if strings.HasPrefix(s, "/") {
		clean := path.Clean(s)
		for _, root := range []string{v.root, v.rawRoot} {
			if root == "" {
				continue
			}
			r := filepath.ToSlash(root)
			if r == "/" {
				continue
			}
			if foldRootCase {
				if strings.EqualFold(clean, r) || len(clean) > len(r) && clean[len(r)] == '/' && strings.EqualFold(clean[:len(r)], r) {
					return "", ErrAbsolutePath
				}
			} else if clean == r || strings.HasPrefix(clean, r+"/") {
				return "", ErrAbsolutePath
			}
		}
	}
	t := strings.TrimLeft(s, "/")
	// "/C:/x.md" strips to "C:/x.md": check again (Windows only; a no-op elsewhere).
	if filepath.VolumeName(t) != "" || filepath.IsAbs(t) {
		return "", ErrAbsolutePath
	}
	return t, nil
}

// AddressNote returns the vault-relative "/" path of the existing note arg
// names. An exact path to an existing file wins and needs no index, so notes
// in hidden folders stay addressable by path. Otherwise arg is resolved as a
// [[arg]] link written at the vault root, against a refreshed link index:
// several matches give an *AmbiguousNoteError, none a "note not found"
// error with suggestions. A protected path gives its path_violation error.
func (v *Vault) AddressNote(arg string) (string, error) {
	rel, err := v.relArg(arg)
	if err != nil {
		return "", err
	}
	rooted := strings.HasPrefix(strings.ReplaceAll(arg, `\`, "/"), "/")

	if norm, err := NormalizeNotePath(rel); err == nil {
		_, err := v.security.ValidatePath(norm)
		switch {
		case err == nil:
			if r, ok := v.noteRel(norm); ok {
				info, err := os.Lstat(filepath.Join(v.root, filepath.FromSlash(r)))
				if err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
					return v.onDiskName(r), nil
				}
			}
		case !errors.Is(err, errOutside) && !errors.Is(err, errInvalid):
			return "", err // protected directory
		}
		// Outside or invalid: like any unknown name, "note not found".
	}

	ix, err := v.linkIndex()
	if err != nil {
		return "", err
	}
	if err := ix.Refresh(); err != nil {
		return "", err
	}
	target := rel
	if rooted {
		target = "/" + rel
	}
	res := ix.Address(target)
	switch res.Status {
	case links.Resolved:
		return res.Path, nil
	case links.Ambiguous:
		return "", &AmbiguousNoteError{Arg: arg, Candidates: res.Candidates}
	}
	nf := &notFoundError{arg: arg}
	if c := path.Clean(rel); c != ".." && !strings.HasPrefix(c, "../") {
		nf.suggestions = suggest(rel, ix.NotePaths(), maxSuggestions)
	}
	return "", nf
}

// onDiskName returns rel spelled as the directory entries spell it, so an
// argument that matched through a case- or Unicode-insensitive file system
// ("SUB/deep note.md", NFD "café") yields the canonical path. A segment is
// replaced only when exactly one entry of its folder has the same links.Key;
// when a folder cannot be read the rest is kept as given.
func (v *Vault) onDiskName(rel string) string {
	segs := strings.Split(rel, "/")
	dir := v.root
	for i, seg := range segs {
		f, err := os.Open(dir)
		if err != nil {
			return strings.Join(segs, "/")
		}
		names, err := f.Readdirnames(-1)
		f.Close()
		if err != nil {
			return strings.Join(segs, "/")
		}
		exact := false
		match := ""
		n := 0
		k := links.Key(seg)
		for _, name := range names {
			if name == seg {
				exact = true
				break
			}
			if links.Key(name) == k {
				match = name
				n++
			}
		}
		if !exact && n == 1 {
			segs[i] = match
		}
		dir = filepath.Join(dir, segs[i])
	}
	return strings.Join(segs, "/")
}

// suggest returns up to max notes whose name is close to query: compared by
// links.Key of the basename without ".md", a note is close when its edit
// distance is at most min(3, len/3) or, for queries of 3 runes or more, when
// it contains the query. Results are sorted by (distance, path). The query
// is truncated to 128 runes, which bounds the cost.
func suggest(query string, notes []string, max int) []string {
	q := links.Key(baseName(query))
	if utf8.RuneCountInString(q) > maxSuggestQueryRn {
		q = string([]rune(q)[:maxSuggestQueryRn])
	}
	qr := []rune(q)
	if len(qr) == 0 || max <= 0 {
		return nil
	}
	limit := min(3, len(qr)/3)
	type hit struct {
		dist int
		path string
	}
	var hits []hit
	for _, p := range notes {
		name := links.Key(baseName(p))
		nr := []rune(name)
		if d, ok := boundedLevenshtein(qr, nr, limit); ok {
			hits = append(hits, hit{d, p})
		} else if len(qr) >= 3 && strings.Contains(name, q) {
			// Containing the query costs exactly the extra runes.
			hits = append(hits, hit{len(nr) - len(qr), p})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].dist != hits[j].dist {
			return hits[i].dist < hits[j].dist
		}
		return hits[i].path < hits[j].path
	})
	if len(hits) > max {
		hits = hits[:max]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.path
	}
	return out
}

// baseName is the last "/" segment of p without a trailing ".md".
func baseName(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	if len(p) >= 3 && strings.EqualFold(p[len(p)-3:], ".md") {
		p = p[:len(p)-3]
	}
	return p
}

// boundedLevenshtein returns the edit distance of a and b when it is at most
// k. It only fills, and resets, the diagonal band of width 2k+1 plus one cell
// on each side, so it costs O(len(a)·k) after O(len(b)) setup, and it gives
// up at once when the lengths differ by more than k.
func boundedLevenshtein(a, b []rune, k int) (int, bool) {
	if d := len(a) - len(b); d > k || -d > k {
		return 0, false
	}
	const inf = 1 << 30
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		if j <= k {
			prev[j] = j
		} else {
			prev[j] = inf
		}
	}
	for i := 1; i <= len(a); i++ {
		lo, hi := max(1, i-k), min(len(b), i+k)
		// Only the band is written below. The cell left of it is read in
		// this row and the cell right of it in the next, so both are reset;
		// the rest of cur is never read.
		cur[lo-1] = inf
		if hi+1 <= len(b) {
			cur[hi+1] = inf
		}
		best := inf
		if i <= k {
			cur[0] = i
			best = i
		}
		for j := lo; j <= hi; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			c := prev[j-1] + cost
			if prev[j]+1 < c {
				c = prev[j] + 1
			}
			if cur[j-1]+1 < c {
				c = cur[j-1] + 1
			}
			cur[j] = c
			if c < best {
				best = c
			}
		}
		if best > k {
			return 0, false
		}
		prev, cur = cur, prev
	}
	if prev[len(b)] > k {
		return 0, false
	}
	return prev[len(b)], true
}
