package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/links"
	"github.com/nemesis312/obsidian-mcp/internal/markdown"
)

// LinkEdit is one link a move rewrote: its text before and after.
type LinkEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// LinkChange lists the links a move rewrote in one note. Path is the note's
// path after the move.
type LinkChange struct {
	Path  string     `json:"path"`
	Links []LinkEdit `json:"links"`
}

// MoveResult reports a move. Updated lists every note whose links were
// rewritten (planned, for a dry run), the moved note included under its new
// path; Failed every note that could not be updated; Warnings every ghost
// link that starts resolving because of the move, left as written.
type MoveResult struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Version  string          `json:"version"`
	DryRun   bool            `json:"dry_run"`
	Updated  []LinkChange    `json:"updated"`
	Failed   []RenameFailure `json:"failed"`
	Warnings []string        `json:"warnings"`
}

// ErrDestinationExists is returned when a move's destination already exists.
var ErrDestinationExists = errors.New("destination already exists")

var errMoveToSelf = errors.New("destination is the note being moved")

// The reasons a planned rewrite fails its check.
const (
	errLinkMeaning = "rewrite would change link meaning"
	errFrontmatter = "rewrite would break the note's frontmatter"
)

// maxListedFailures bounds the notes a refused move names.
const maxListedFailures = 10

// movePlan is everything a move writes, computed before any write.
type movePlan struct {
	src, dst       string // vault-relative "/" paths
	srcRel, dstRel string // os.Root names
	caseOnly       bool   // dst is src's own directory entry in another case
	mode           fs.FileMode
	original       string // the moved note's content as read
	content        string // the moved note's content to write at dst
	ownEdits       []LinkEdit
	linkers        []plannedNote // sorted by path; the moved note is not one
	failed         []planFailure // sorted by path
	warnings       []string
	notes, atts    []string // the path sets links were resolved against
}

type plannedNote struct {
	path    string
	version string // of the content the rewrite was made from
	content string
	edits   []LinkEdit
}

// planFailure is a linker the plan cannot update: it could not be read, or
// (verify) its rewrite failed its check.
type planFailure struct {
	path, version, reason string
	verify                bool
}

// MoveNote moves the note arg addresses to dest (a vault-relative path,
// ".md" optional) and rewrites every link whose resolution the move would
// change, so the vault's graph stays the same apart from the new path:
// links to the note, the moved note's own relative links, and links whose
// tie-break the new path would flip. A ghost link that the new path makes
// resolve is reported in Warnings and left as written. Each rewritten note
// is re-extracted and re-resolved, and its frontmatter re-parsed, before
// anything is written.
//
// The destination must not exist, unless it is the note itself spelled in
// another case (a case-only rename on a case-insensitive file system). Its
// existing folders are taken with their on-disk spelling. expected is
// optional; when set, the note must still have that version. It is required
// when the destination is inside a hidden folder.
//
// The plan is made without the write guard. A real move then takes the
// guard, re-reads the note and every planned linker and re-plans what
// changed meanwhile (a new linker included), and writes. A real move is
// refused, with nothing written, when a linker's rewrite fails its check;
// a dry run lists such linkers in Failed. ctx is honoured until the note is
// written: from then on the move finishes, so no linker is left pointing at
// the old path. A linker that fails to write is reported in Failed, without
// rollback. A dry run takes no guard and writes nothing, so its plan can be
// stale by the time a real move runs.
//
// A symlinked source is refused, as is a destination whose folders include
// a symlink, whose name holds a character Obsidian links cannot use
// (# | [ ] ^), a control character or a leading or trailing space, and a
// note whose own links could not be rewritten safely: nothing is written
// then.
func (v *Vault) MoveNote(ctx context.Context, arg, dest, expected string, dryRun bool) (MoveResult, error) {
	if err := ctx.Err(); err != nil {
		return MoveResult{}, err
	}
	if expected != "" {
		if err := checkVersion(expected); err != nil {
			return MoveResult{}, err
		}
	}
	src, err := v.addressForWrite(arg)
	if err != nil {
		return MoveResult{}, err
	}
	dst, err := v.moveDestination(dest)
	if err != nil {
		return MoveResult{}, err
	}
	if dst == src {
		return MoveResult{}, errMoveToSelf
	}
	if dir := hiddenFolder(dst); dir != "" && expected == "" {
		return MoveResult{}, &hiddenDestinationError{dir: dir}
	}

	r, err := v.openRoot()
	if err != nil {
		return MoveResult{}, v.fsError("move note", src, err)
	}
	defer r.Close()
	p, err := v.planMove(ctx, r, arg, src, dst, expected, nil)
	if err != nil {
		return MoveResult{}, err
	}
	if dryRun {
		return p.result(true), nil
	}
	if err := p.unsafe(); err != nil {
		return MoveResult{}, err
	}

	var written []string
	res, err := func() (MoveResult, error) {
		if writeLockHook != nil {
			writeLockHook()
		}
		unlock, err := v.guard.lock()
		if err != nil {
			return MoveResult{}, err
		}
		defer unlock()
		if p, err = v.planMove(ctx, r, arg, src, dst, expected, p); err != nil {
			return MoveResult{}, err
		}
		if err := p.unsafe(); err != nil {
			return MoveResult{}, err
		}
		// The last point a cancellation stops the move: once the note is
		// written, every linker must follow it.
		if err := ctx.Err(); err != nil {
			return MoveResult{}, err
		}
		var res MoveResult
		res, written, err = v.applyMove(r, p)
		return res, err
	}()
	if err != nil {
		return MoveResult{}, err
	}
	v.indexRemove(src)
	v.indexUpdate(dst)
	for _, p := range written {
		v.indexUpdate(p)
	}
	return res, nil
}

// hiddenDestinationError is ErrVersionRequired for a move into a hidden
// folder.
type hiddenDestinationError struct{ dir string }

func (e *hiddenDestinationError) Error() string {
	return `expected_version is required to move a note into a hidden folder (` + e.dir + `/); read it with get_note and pass its "version"`
}
func (e *hiddenDestinationError) Unwrap() error { return ErrVersionRequired }

// hiddenFolder returns the folder of dst when one of its segments starts
// with '.', or "".
func hiddenFolder(dst string) string {
	dir := path.Dir(dst)
	if dir == "." {
		return ""
	}
	for _, seg := range strings.Split(dir, "/") {
		if strings.HasPrefix(seg, ".") {
			return dir
		}
	}
	return ""
}

// moveDestination validates dest as a note path to create: vault-relative,
// ".md" (added when there is no extension), not protected, and a name links
// can point to. Its existing folders take their on-disk spelling, so a
// folder typed in another case on a case-insensitive file system is the
// folder the note lands in. It returns the vault-relative "/" path.
func (v *Vault) moveDestination(dest string) (string, error) {
	rel, err := v.relArg(dest)
	if err != nil {
		return "", err
	}
	norm, err := NormalizeNotePath(rel)
	if err != nil {
		return "", err
	}
	// The name checks are lexical and come first, so every OS gives the
	// same error (Windows would otherwise refuse some of these names in
	// ValidatePath with its own).
	if out, ok := v.noteRel(norm); ok {
		if strings.ContainsAny(out, linkUnsafeChars) {
			return "", fmt.Errorf("invalid destination name: %s contains a character Obsidian links cannot use (# | [ ] ^)", out)
		}
		if unmatchableName(out) {
			return "", fmt.Errorf("invalid destination name: %q has a name links cannot match (a leading or trailing space, or a control character)", out)
		}
	}
	if _, err := v.resolveNote(norm); err != nil {
		return "", err
	}
	out, ok := v.noteRel(norm)
	if !ok {
		return "", errInvalid
	}
	if dir := path.Dir(out); dir != "." {
		out = v.existingSpelling(dir) + "/" + path.Base(out)
	}
	return out, nil
}

// existingSpelling returns dir with its longest existing prefix spelled as
// the directory entries spell it (onDiskName). Segments that do not exist
// yet are kept as given.
func (v *Vault) existingSpelling(dir string) string {
	segs := strings.Split(dir, "/")
	n := 0
	for n < len(segs) {
		if _, err := os.Lstat(filepath.Join(v.root, filepath.FromSlash(strings.Join(segs[:n+1], "/")))); err != nil {
			break
		}
		n++
	}
	if n == 0 {
		return dir
	}
	return strings.Join(append([]string{v.onDiskName(strings.Join(segs[:n], "/"))}, segs[n:]...), "/")
}

// linkUnsafeChars cannot appear in a note name that links point to: a link
// would read them as a heading, alias, bracket or block reference.
const linkUnsafeChars = "#|[]^"

// unmatchableName reports whether a segment of p, or its file name without
// ".md", has leading or trailing white space or holds a control character
// (C0 or DEL): a link target is trimmed and cannot hold those, so no link
// could point at the note.
func unmatchableName(p string) bool {
	segs := strings.Split(p, "/")
	last := segs[len(segs)-1]
	segs = append(segs, strings.TrimSuffix(last, path.Ext(last)))
	for _, s := range segs {
		if s != strings.TrimSpace(s) {
			return true
		}
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] == 0x7f {
				return true
			}
		}
	}
	return false
}

// planMove reads the note and every linker, checks the version and the
// paths, and computes every rewrite. It writes nothing. With prev (a plan
// made earlier, before the write guard was taken) a linker whose content
// still has the version prev planned from keeps its plan, as long as the
// vault's note and attachment paths did not change; every other note is
// planned again from its current content.
func (v *Vault) planMove(ctx context.Context, r *os.Root, arg, src, dst, expected string, prev *movePlan) (*movePlan, error) {
	content, err := v.ReadNote(src)
	if err != nil {
		return nil, notFoundAs(arg, err)
	}
	if expected != "" {
		if cur := VersionOf([]byte(content)); cur != expected {
			return nil, &ConflictError{Path: src, Current: cur}
		}
	}
	ix, err := v.linkIndex()
	if err != nil {
		return nil, err
	}
	if err := ix.Refresh(); err != nil {
		return nil, err
	}
	notes, atts, refs := ix.RefSets()

	p := &movePlan{src: src, dst: dst, srcRel: filepath.FromSlash(src), notes: notes, atts: atts}
	if err := v.checkMovePaths(r, arg, p); err != nil {
		return nil, err
	}

	afterNotes := make([]string, 0, len(notes)+1)
	for _, n := range notes {
		if n != src {
			afterNotes = append(afterNotes, n)
		}
	}
	afterNotes = append(afterNotes, dst)
	m := &mover{
		src: src, dst: dst,
		before: links.NewResolver(notes, atts),
		after:  links.NewResolver(afterNotes, atts),
		warned: map[string]bool{},
		memo:   map[targetKey]string{},
	}

	// The moved note, from its fresh content.
	p.content, p.original = content, content
	out, edits, fail := m.rewrite(src, dst, content)
	switch {
	case fail != nil && fail.frontmatter:
		return nil, fmt.Errorf("cannot move %s: %s", src, errFrontmatter)
	case fail != nil:
		return nil, fmt.Errorf("cannot move %s: its own link %s could not be rewritten safely", src, fail.raw)
	case len(edits) > 0:
		p.content, p.ownEdits = out, edits
	}

	var keptPlans map[string]plannedNote
	var keptFails map[string]planFailure
	if prev != nil && slices.Equal(prev.notes, notes) && slices.Equal(prev.atts, atts) {
		keptPlans = make(map[string]plannedNote, len(prev.linkers))
		for _, l := range prev.linkers {
			keptPlans[l.path] = l
		}
		keptFails = make(map[string]planFailure, len(prev.failed))
		for _, f := range prev.failed {
			if f.verify {
				keptFails[f.path] = f
			}
		}
	}

	// Linkers: a note is a candidate when one of its distinct refs resolves
	// elsewhere after the move. It is then read and rewritten in full.
	for _, n := range notes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n == src || !m.isCandidate(n, refs[n]) {
			continue
		}
		c, err := v.ReadNote(n)
		if err != nil {
			p.failed = append(p.failed, planFailure{path: n, reason: err.Error()})
			continue
		}
		ver := VersionOf([]byte(c))
		if l, ok := keptPlans[n]; ok && l.version == ver {
			p.linkers = append(p.linkers, l)
			continue
		}
		if f, ok := keptFails[n]; ok && f.version == ver {
			p.failed = append(p.failed, f)
			continue
		}
		out, edits, fail := m.rewrite(n, n, c)
		switch {
		case fail != nil && fail.frontmatter:
			p.failed = append(p.failed, planFailure{path: n, version: ver, reason: errFrontmatter, verify: true})
		case fail != nil:
			p.failed = append(p.failed, planFailure{path: n, version: ver, reason: errLinkMeaning, verify: true})
		case len(edits) > 0:
			p.linkers = append(p.linkers, plannedNote{path: n, version: ver, content: out, edits: edits})
		}
	}
	p.warnings = m.warnings
	sort.Strings(p.warnings)
	return p, nil
}

// checkMovePaths validates the source and the destination against the file
// system as it is now: both inside the vault and writable, the source not a
// symlink, no symlink among the destination's existing folders, and the
// destination free (or the source itself, for a case-only rename). It sets
// p.dstRel, p.mode and p.caseOnly.
func (v *Vault) checkMovePaths(r *os.Root, arg string, p *movePlan) error {
	if _, err := v.resolveNote(p.src); err != nil {
		return err
	}
	if err := v.checkDestFolders(r, p.dst); err != nil {
		return err
	}
	// The destination is written by its resolved name, like CreateNote does.
	safeDst, err := v.resolveNote(p.dst)
	if err != nil {
		return err
	}
	info, err := r.Lstat(p.srcRel)
	if err != nil {
		return notFoundAs(arg, v.fsError("move note", p.src, err))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cannot move %s: it is a symlink; move its target instead", p.src)
	}
	p.mode = info.Mode().Perm()
	safeDst = requestedLeaf(safeDst, filepath.Join(v.root, filepath.FromSlash(p.dst)))
	if p.dstRel, err = v.rootRel(safeDst); err != nil {
		return err
	}
	p.caseOnly, err = v.checkDestination(r, p)
	return err
}

// requestedLeaf returns resolved with its last name spelled as in lexical,
// the destination as asked for, when the two differ only in case. On
// Windows filepath.EvalSymlinks returns each existing name in its on-disk
// case, so a case-only rename ("note.md" to "Note.md") would resolve to
// the source's own name. Any other difference (a symlink at the leaf) keeps
// resolved. Folder names keep their resolved case.
func requestedLeaf(resolved, lexical string) string {
	base := filepath.Base(lexical)
	if resolved == lexical || filepath.Base(resolved) == base || !strings.EqualFold(resolved, lexical) {
		return resolved
	}
	return filepath.Join(filepath.Dir(resolved), base)
}

// checkDestFolders refuses a destination whose existing folders include a
// symlink: links written by path would name the symlink, while the note is
// listed and indexed under the symlink's target.
func (v *Vault) checkDestFolders(r *os.Root, dst string) error {
	dir := path.Dir(dst)
	if dir == "." {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(dir, "/") {
		cur = path.Join(cur, seg)
		info, err := r.Lstat(filepath.FromSlash(cur))
		if errors.Is(err, fs.ErrNotExist) {
			return nil // the rest is created
		}
		if err != nil {
			return v.fsError("move note to", dst, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid destination %s: a folder in the path is a symlink", dst)
		}
	}
	return nil
}

// checkDestination fails with ErrDestinationExists when p.dstRel exists,
// unless it is the source's own entry spelled in another case: then it
// reports a case-only rename.
func (v *Vault) checkDestination(r *os.Root, p *movePlan) (caseOnly bool, err error) {
	dInfo, err := r.Lstat(p.dstRel)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, v.fsError("move note to", p.dst, err)
	}
	exists := &destExistsError{path: p.dst}
	if links.Key(p.src) != links.Key(p.dst) {
		return false, exists
	}
	sInfo, err := r.Lstat(p.srcRel)
	if err != nil || !os.SameFile(sInfo, dInfo) {
		return false, exists
	}
	// Two distinct entries (hard links on a case-sensitive file system)
	// are not a case-only rename: the destination's exact name is listed.
	if hasEntry(r, filepath.Dir(p.dstRel), filepath.Base(p.dstRel)) {
		return false, exists
	}
	return true, nil
}

type destExistsError struct{ path string }

func (e *destExistsError) Error() string { return ErrDestinationExists.Error() + ": " + e.path }
func (e *destExistsError) Unwrap() error { return ErrDestinationExists }

// hasEntry reports whether folder dir of r lists name exactly.
func hasEntry(r *os.Root, dir, name string) bool {
	f, err := r.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// unsafe returns the error that refuses a real move when a linker's rewrite
// failed its check, naming up to maxListedFailures of them.
func (p *movePlan) unsafe() error {
	var bad []string
	for _, f := range p.failed {
		if f.verify {
			bad = append(bad, f.path+" ("+f.reason+")")
		}
	}
	if len(bad) == 0 {
		return nil
	}
	n, more := len(bad), ""
	if n > maxListedFailures {
		more = fmt.Sprintf(", … (%d more)", n-maxListedFailures)
		bad = bad[:maxListedFailures]
	}
	return fmt.Errorf("cannot move %s: %d linked note(s) could not be rewritten safely, nothing was written: %s%s; run with dry_run=true to see the plan",
		p.src, n, strings.Join(bad, ", "), more)
}

// result is the plan as a MoveResult: the moved note's own edits, the
// planned linkers for a dry run (a real move adds the ones it writes), and
// every planned failure.
func (p *movePlan) result(dryRun bool) MoveResult {
	res := MoveResult{
		From: p.src, To: p.dst, Version: VersionOf([]byte(p.content)), DryRun: dryRun,
		Updated: []LinkChange{}, Failed: []RenameFailure{}, Warnings: append([]string{}, p.warnings...),
	}
	if len(p.ownEdits) > 0 {
		res.Updated = append(res.Updated, LinkChange{Path: p.dst, Links: p.ownEdits})
	}
	if dryRun {
		for _, l := range p.linkers {
			res.Updated = append(res.Updated, LinkChange{Path: l.path, Links: l.edits})
		}
	}
	for _, f := range p.failed {
		res.Failed = append(res.Failed, RenameFailure{Path: f.path, Error: f.reason})
	}
	return res
}

// applyMove writes the plan through r: the note first, then each linker.
// It takes no context: once the note is written, the linkers must follow.
// It returns the result and the linkers written.
func (v *Vault) applyMove(r *os.Root, p *movePlan) (MoveResult, []string, error) {
	res := p.result(false)
	beforeWrite(p.dstRel)
	data := []byte(p.content)
	if p.caseOnly {
		if err := renameReplace(r, p.srcRel, p.dstRel); err != nil {
			return MoveResult{}, nil, v.fsError("move note", p.src, err)
		}
		// The note keeps its bytes unless its own links were rewritten. A
		// failed rewrite leaves it moved, unrewritten, and reported.
		if len(p.ownEdits) > 0 {
			if err := writeFileAtomic(r, p.dstRel, data); err != nil {
				res.Failed = append(res.Failed, RenameFailure{Path: p.dst, Error: v.fsError("write note", p.dst, err).Error()})
				res.Updated = res.Updated[1:]
				res.Version = VersionOf([]byte(p.original))
			}
		}
	} else {
		if err := r.MkdirAll(filepath.Dir(p.dstRel), 0o755); err != nil {
			return MoveResult{}, nil, v.fsError("create directory for", p.dst, err)
		}
		if err := createExclusive(r, p.dstRel, data, p.mode); err != nil {
			if errors.Is(err, errNoteExists) {
				return MoveResult{}, nil, &destExistsError{path: p.dst}
			}
			return MoveResult{}, nil, v.fsError("move note to", p.dst, err)
		}
		if err := removeFile(r, p.srcRel); err != nil {
			_ = removeFile(r, p.dstRel)
			return MoveResult{}, nil, v.fsError("remove moved note", p.src, err)
		}
	}

	var written []string
	for _, l := range p.linkers {
		if err := v.writeLinker(r, l); err != nil {
			res.Failed = append(res.Failed, RenameFailure{Path: l.path, Error: err.Error()})
			continue
		}
		written = append(written, l.path)
		res.Updated = append(res.Updated, LinkChange{Path: l.path, Links: l.edits})
	}
	return res, written, nil
}

// writeLinker writes one rewritten linker through r, by its resolved name
// as writeNoteLocked does.
func (v *Vault) writeLinker(r *os.Root, l plannedNote) error {
	safe, err := v.resolveNote(l.path)
	if err != nil {
		return err
	}
	rel, err := v.rootRel(safe)
	if err != nil {
		return err
	}
	beforeWrite(rel)
	if err := writeFileAtomic(r, rel, []byte(l.content)); err != nil {
		return v.fsError("write note", l.path, err)
	}
	return nil
}

// mover compares link resolutions before and after a move.
type mover struct {
	src, dst      string
	before, after *links.Resolver
	warnings      []string
	warned        map[string]bool
	memo          map[targetKey]string // TargetFor results
}

// targetKey is what links.TargetFor's result depends on: the linking note's
// folder (resolution and relative paths use only that), the target, the
// style and the extension choice.
type targetKey struct {
	dir, to string
	style   links.LinkStyle
	keepExt bool
}

// targetFor is links.TargetFor against the after-move resolver, memoized:
// a note repeating a link pays for the target text once.
func (m *mover) targetFor(from, to string, style links.LinkStyle, keepExt bool) string {
	k := targetKey{dir: path.Dir(from), to: to, style: style, keepExt: keepExt}
	if t, ok := m.memo[k]; ok {
		return t
	}
	t := links.TargetFor(m.after, from, to, style, keepExt)
	m.memo[k] = t
	return t
}

// targets returns where ref, written in a note at fromOld before the move
// and at fromNew after it, should point (intended: its target before the
// move, src renamed to dst) and points after the move (actual). "" is a
// ghost.
func (m *mover) targets(fromOld, fromNew string, ref links.Ref) (intended, actual string) {
	if b := m.before.Resolve(fromOld, ref); b.Status != links.Unresolved {
		intended = b.Path
		if intended == m.src {
			intended = m.dst
		}
	}
	if a := m.after.Resolve(fromNew, ref); a.Status != links.Unresolved {
		actual = a.Path
	}
	return intended, actual
}

// isCandidate reports whether a ref of note n resolves elsewhere after the
// move. It records a warning for a ghost that starts resolving.
func (m *mover) isCandidate(n string, refs []links.Ref) bool {
	cand := false
	for _, ref := range refs {
		intended, actual := m.targets(n, n, ref)
		switch {
		case intended == actual:
		case intended == "":
			m.warn(n, ref.Raw, actual)
		default:
			cand = true
		}
	}
	return cand
}

func (m *mover) warn(n, raw, to string) {
	w := n + ": " + raw + " now resolves to " + to
	if !m.warned[w] {
		m.warned[w] = true
		m.warnings = append(m.warnings, w)
	}
}

// rewriteFailure says why a rewrite failed its check: the first link that
// could not be rewritten safely (raw, as written before), or a frontmatter
// the rewrite would break.
type rewriteFailure struct {
	raw         string
	frontmatter bool
}

// rewrite rewrites every link of content (a note at fromOld before the
// move, fromNew after it) whose resolution the move changes, then checks
// the result: re-extracted, it must hold the same links in the same order,
// each with the text written and resolving after the move where it should,
// and a frontmatter that parsed before must still parse, with the same
// top-level keys. fail is non-nil when it does not.
func (m *mover) rewrite(fromOld, fromNew, content string) (out string, edits []LinkEdit, fail *rewriteFailure) {
	refs := links.Extract(content)
	wantRaw := make([]string, len(refs))
	wantTo := make([]string, len(refs))
	var es []links.Edit
	for i, ref := range refs {
		wantRaw[i] = ref.Raw
		intended, actual := m.targets(fromOld, fromNew, ref)
		switch {
		case intended == actual:
			wantTo[i] = actual
			continue
		case intended == "":
			m.warn(fromNew, ref.Raw, actual)
			wantTo[i] = actual
			continue
		}
		wantTo[i] = intended
		keepExt := strings.HasSuffix(strings.ToLower(strings.TrimSpace(ref.Target)), ".md")
		t := m.targetFor(fromNew, intended, links.StyleOf(ref), keepExt)
		raw := links.RewriteRaw(ref, t)
		if raw == ref.Raw {
			// The link cannot be pointed at intended.
			return content, nil, &rewriteFailure{raw: ref.Raw}
		}
		wantRaw[i] = raw
		es = append(es, links.Edit{Offset: ref.Offset, Old: ref.Raw, New: raw})
		edits = append(edits, LinkEdit{Old: ref.Raw, New: raw})
	}
	if len(es) == 0 {
		return content, nil, nil
	}
	out, err := links.ApplyEdits(content, es)
	if err != nil {
		return content, nil, &rewriteFailure{raw: es[0].Old}
	}
	got := links.Extract(out)
	for i := range refs {
		if i >= len(got) || got[i].Kind != refs[i].Kind || got[i].Raw != wantRaw[i] {
			return content, nil, &rewriteFailure{raw: refs[i].Raw}
		}
		to := ""
		if a := m.after.Resolve(fromNew, got[i]); a.Status != links.Unresolved {
			to = a.Path
		}
		if to != wantTo[i] {
			return content, nil, &rewriteFailure{raw: refs[i].Raw}
		}
	}
	if len(got) != len(refs) {
		return content, nil, &rewriteFailure{raw: es[0].Old}
	}
	if !frontmatterKept(content, out) {
		return content, nil, &rewriteFailure{frontmatter: true}
	}
	return out, edits, nil
}

// frontmatterKept reports whether after's frontmatter still parses, with the
// same top-level keys, when before's did. A note without a valid
// frontmatter before has nothing to break.
func frontmatterKept(before, after string) bool {
	bfm, _, err := markdown.ParseFrontmatter(before)
	if err != nil || bfm == nil {
		return true
	}
	afm, _, err := markdown.ParseFrontmatter(after)
	if err != nil || afm == nil || len(afm.Data) != len(bfm.Data) {
		return false
	}
	for k := range bfm.Data {
		if _, ok := afm.Data[k]; !ok {
			return false
		}
	}
	return true
}
