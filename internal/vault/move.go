package vault

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nemesis312/obsidian-mcp/internal/links"
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

// errLinkMeaning is the failure of a note whose rewritten links would not
// resolve to their intended targets once re-extracted.
const errLinkMeaning = "rewrite would change link meaning"

// movePlan is everything a move writes, computed before any write.
type movePlan struct {
	src, dst       string // vault-relative "/" paths
	srcRel, dstRel string // os.Root names
	caseOnly       bool   // dst is src's own directory entry in another case
	content        string // the moved note's content to write at dst
	original       string // the moved note's content as read
	version        string
	linkers        []plannedNote // sorted by path; the moved note is not one
	res            MoveResult
}

type plannedNote struct {
	path    string
	content string
	edits   []LinkEdit
}

// MoveNote moves the note arg addresses to dest (a vault-relative path,
// ".md" optional) and rewrites every link whose resolution the move would
// change, so the vault's graph stays the same apart from the new path:
// links to the note, the moved note's own relative links, and links whose
// tie-break the new path would flip. A ghost link that the new path makes
// resolve is reported in Warnings and left as written. Each rewritten note
// is re-extracted and re-resolved before it is written; one whose links
// would change meaning is reported in Failed and not written.
//
// The destination must not exist, unless it is the note itself spelled in
// another case (a case-only rename on a case-insensitive file system).
// expected is optional; when set, the note must still have that version.
//
// A real move holds the write guard for the whole plan and write. The note
// moves first; a linker that fails to write afterwards is reported in
// Failed, without rollback, as is every linker left once ctx is done. A dry
// run takes no guard and writes nothing, so its plan can be stale by the
// time a real move runs.
//
// The source is moved by its own name: a symlinked note is replaced by a
// regular file at dest and the symlink is removed, its target kept.
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

	if dryRun {
		r, err := v.openRoot()
		if err != nil {
			return MoveResult{}, v.fsError("move note", src, err)
		}
		defer r.Close()
		p, err := v.planMove(r, arg, src, dst, expected)
		if err != nil {
			return MoveResult{}, err
		}
		p.res.DryRun = true
		for _, l := range p.linkers {
			p.res.Updated = append(p.res.Updated, LinkChange{Path: l.path, Links: l.edits})
		}
		return p.res, nil
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
		r, err := v.openRoot()
		if err != nil {
			return MoveResult{}, v.fsError("move note", src, err)
		}
		defer r.Close()
		p, err := v.planMove(r, arg, src, dst, expected)
		if err != nil {
			return MoveResult{}, err
		}
		written, err = v.applyMove(ctx, r, p)
		return p.res, err
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

// moveDestination validates dest as a note path to create: vault-relative,
// ".md" (added when there is no extension), not protected. It returns the
// vault-relative "/" path.
func (v *Vault) moveDestination(dest string) (string, error) {
	rel, err := v.relArg(dest)
	if err != nil {
		return "", err
	}
	norm, err := NormalizeNotePath(rel)
	if err != nil {
		return "", err
	}
	if _, err := v.resolveNote(norm); err != nil {
		return "", err
	}
	out, ok := v.noteRel(norm)
	if !ok {
		return "", errInvalid
	}
	return out, nil
}

// planMove reads the note and every linker, checks the version and the
// destination, and computes every rewrite. It writes nothing.
func (v *Vault) planMove(r *os.Root, arg, src, dst, expected string) (*movePlan, error) {
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

	p := &movePlan{src: src, dst: dst, srcRel: filepath.FromSlash(src)}
	// Validate again: a folder may have been retargeted since the call
	// started. The destination is written by its resolved name, like
	// CreateNote does.
	if _, err := v.resolveNote(src); err != nil {
		return nil, err
	}
	safeDst, err := v.resolveNote(dst)
	if err != nil {
		return nil, err
	}
	if p.dstRel, err = v.rootRel(safeDst); err != nil {
		return nil, err
	}
	if p.caseOnly, err = v.checkDestination(r, p); err != nil {
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
	}

	// The moved note, from its fresh content.
	p.content, p.original = content, content
	if out, edits, ok := m.rewrite(src, dst, content); !ok {
		p.res.Failed = append(p.res.Failed, RenameFailure{Path: dst, Error: errLinkMeaning})
	} else if len(edits) > 0 {
		p.content = out
		p.res.Updated = append(p.res.Updated, LinkChange{Path: dst, Links: edits})
	}
	p.version = VersionOf([]byte(p.content))

	// Linkers: a note is a candidate when one of its distinct refs resolves
	// elsewhere after the move. It is then read and rewritten in full.
	for _, n := range notes {
		if n == src || !m.isCandidate(n, refs[n]) {
			continue
		}
		c, err := v.ReadNote(n)
		if err != nil {
			p.res.Failed = append(p.res.Failed, RenameFailure{Path: n, Error: err.Error()})
			continue
		}
		out, edits, ok := m.rewrite(n, n, c)
		switch {
		case !ok:
			p.res.Failed = append(p.res.Failed, RenameFailure{Path: n, Error: errLinkMeaning})
		case len(edits) > 0:
			p.linkers = append(p.linkers, plannedNote{path: n, content: out, edits: edits})
		}
	}

	p.res.From, p.res.To, p.res.Version = src, dst, p.version
	p.res.Warnings = m.warnings
	if p.res.Updated == nil {
		p.res.Updated = []LinkChange{}
	}
	if p.res.Failed == nil {
		p.res.Failed = []RenameFailure{}
	}
	if p.res.Warnings == nil {
		p.res.Warnings = []string{}
	}
	sort.Strings(p.res.Warnings)
	return p, nil
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

// applyMove writes the plan through r: the note first, then each linker.
// It returns the linkers written.
func (v *Vault) applyMove(ctx context.Context, r *os.Root, p *movePlan) ([]string, error) {
	beforeWrite(p.dstRel)
	data := []byte(p.content)
	if p.caseOnly {
		if err := renameReplace(r, p.srcRel, p.dstRel); err != nil {
			return nil, v.fsError("move note", p.src, err)
		}
		// The note keeps its bytes unless its own links were rewritten. A
		// failed rewrite leaves it moved, unrewritten, and reported.
		if len(p.res.Updated) > 0 && p.res.Updated[0].Path == p.dst {
			if err := writeFileAtomic(r, p.dstRel, data); err != nil {
				p.res.Failed = append(p.res.Failed, RenameFailure{Path: p.dst, Error: v.fsError("write note", p.dst, err).Error()})
				p.res.Updated = p.res.Updated[1:]
				p.res.Version = VersionOf([]byte(p.original))
			}
		}
	} else {
		if err := r.MkdirAll(filepath.Dir(p.dstRel), 0o755); err != nil {
			return nil, v.fsError("create directory for", p.dst, err)
		}
		if err := createExclusive(r, p.dstRel, data); err != nil {
			if errors.Is(err, errNoteExists) {
				return nil, &destExistsError{path: p.dst}
			}
			return nil, v.fsError("move note to", p.dst, err)
		}
		if err := removeFile(r, p.srcRel); err != nil {
			_ = removeFile(r, p.dstRel)
			return nil, v.fsError("remove moved note", p.src, err)
		}
	}

	var written []string
	for _, l := range p.linkers {
		if ctx.Err() != nil {
			p.res.Failed = append(p.res.Failed, RenameFailure{Path: l.path, Error: "canceled"})
			continue
		}
		if err := v.writeLinker(r, l); err != nil {
			p.res.Failed = append(p.res.Failed, RenameFailure{Path: l.path, Error: err.Error()})
			continue
		}
		written = append(written, l.path)
		p.res.Updated = append(p.res.Updated, LinkChange{Path: l.path, Links: l.edits})
	}
	return written, nil
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

// rewrite rewrites every link of content (a note at fromOld before the
// move, fromNew after it) whose resolution the move changes, then checks
// the result: re-extracted, it must hold the same links in the same order,
// each with the text written and resolving after the move where it should.
// ok is false when it does not.
func (m *mover) rewrite(fromOld, fromNew, content string) (out string, edits []LinkEdit, ok bool) {
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
		t := links.TargetFor(m.after, fromNew, intended, links.StyleOf(ref), keepExt)
		raw := links.RewriteRaw(ref, t)
		if raw == ref.Raw {
			return content, nil, false // the link cannot be pointed at intended
		}
		wantRaw[i] = raw
		es = append(es, links.Edit{Offset: ref.Offset, Old: ref.Raw, New: raw})
		edits = append(edits, LinkEdit{Old: ref.Raw, New: raw})
	}
	if len(es) == 0 {
		return content, nil, true
	}
	out, err := links.ApplyEdits(content, es)
	if err != nil {
		return content, nil, false
	}
	got := links.Extract(out)
	if len(got) != len(refs) {
		return content, nil, false
	}
	for i, ref := range got {
		if ref.Kind != refs[i].Kind || ref.Raw != wantRaw[i] {
			return content, nil, false
		}
		to := ""
		if a := m.after.Resolve(fromNew, ref); a.Status != links.Unresolved {
			to = a.Path
		}
		if to != wantTo[i] {
			return content, nil, false
		}
	}
	return out, edits, true
}
