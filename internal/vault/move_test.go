package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nemesis312/obsidian-mcp/internal/index"
	"github.com/nemesis312/obsidian-mcp/internal/links"
)

// copyLinkvault copies the index fixture vault into a temp dir, plus the
// NFD note the fixture cannot commit (see copyFixture in package index).
func copyLinkvault(t testing.TB) string {
	t.Helper()
	src := filepath.Join("..", "index", "testdata", "linkvault")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	nfd := filepath.Join(dst, "nfd", "Café.md")
	if err := os.MkdirAll(filepath.Dir(nfd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfd, []byte("Name stored in NFD. No links.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// edgeID is an edge without its Ambiguous flag and first Raw.
type edgeID struct {
	From, To string
	ToKind   index.NodeKind
	Kind     links.Kind
}

func graphEdges(t *testing.T, v *Vault) []index.Edge {
	t.Helper()
	ix, err := index.Build(v)
	if err != nil {
		t.Fatal(err)
	}
	_, edges := ix.Graph()
	return edges
}

func edgeIDs(edges []index.Edge, mapPath func(string) string) map[edgeID]bool {
	out := map[edgeID]bool{}
	for _, e := range edges {
		id := edgeID{From: mapPath(e.From), To: e.To, ToKind: e.ToKind, Kind: e.Kind}
		if e.ToKind != index.NodeGhost {
			id.To = mapPath(e.To)
		}
		out[id] = true
	}
	return out
}

// applyWarnings turns each "<n>: <raw> now resolves to <p>" warning into the
// graph change it reports: n's ghost edge for raw becomes an edge to p.
func applyWarnings(t *testing.T, ids map[edgeID]bool, warnings []string) {
	t.Helper()
	for _, w := range warnings {
		head, p, ok := strings.Cut(w, " now resolves to ")
		n, raw, ok2 := strings.Cut(head, ": ")
		refs := links.Extract(raw)
		if !ok || !ok2 || len(refs) != 1 {
			t.Fatalf("unparsable warning %q", w)
		}
		ghost := edgeID{From: n, To: links.GhostKey(refs[0].Target), ToKind: index.NodeGhost, Kind: refs[0].Kind}
		if !ids[ghost] {
			t.Errorf("warning %q names no ghost edge %+v", w, ghost)
		}
		delete(ids, ghost)
		ids[edgeID{From: n, To: p, ToKind: index.NodeNote, Kind: refs[0].Kind}] = true
	}
}

// assertSameGraph checks that the graph after a move from src to dst is the
// graph before it with src renamed, apart from the reported warnings.
func assertSameGraph(t *testing.T, before, after []index.Edge, src, dst string, warnings []string) {
	t.Helper()
	want := edgeIDs(before, func(p string) string {
		if p == src {
			return dst
		}
		return p
	})
	applyWarnings(t, want, warnings)
	got := edgeIDs(after, func(p string) string { return p })
	for id := range want {
		if !got[id] {
			t.Errorf("edge lost: %+v", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("edge gained: %+v", id)
		}
	}
}

// readAll returns every file of dir by vault-relative "/" path, hidden
// folders excluded.
func readAll(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// skeleton returns content with every link replaced by a NUL byte, and the
// links' Raw texts in order.
func skeleton(content string) (string, []string) {
	var b strings.Builder
	var raws []string
	pos := 0
	for _, r := range links.Extract(content) {
		b.WriteString(content[pos:r.Offset])
		b.WriteByte(0)
		pos = r.Offset + len(r.Raw)
		raws = append(raws, r.Raw)
	}
	b.WriteString(content[pos:])
	return b.String(), raws
}

// assertOnlyLinksChanged checks that newC differs from oldC only inside
// links, and that the changed links are exactly the reported edits.
func assertOnlyLinksChanged(t *testing.T, p, oldC, newC string, edits []LinkEdit) {
	t.Helper()
	oldS, oldRaws := skeleton(oldC)
	newS, newRaws := skeleton(newC)
	if oldS != newS {
		t.Errorf("%s: bytes outside links changed:\nold %q\nnew %q", p, oldC, newC)
		return
	}
	var diff []LinkEdit
	for i := range oldRaws {
		if oldRaws[i] != newRaws[i] {
			diff = append(diff, LinkEdit{Old: oldRaws[i], New: newRaws[i]})
		}
	}
	key := func(es []LinkEdit) []string {
		out := []string{}
		for _, e := range es {
			out = append(out, e.Old+"\x00"+e.New)
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(key(diff), key(edits)) {
		t.Errorf("%s: changed links %v, reported %v", p, diff, edits)
	}
}

// movedName is the destination the round-trip test moves note to.
func movedName(note string) string {
	base := path.Base(note)
	if ext := path.Ext(base); strings.EqualFold(ext, ".md") {
		base = base[:len(base)-len(ext)]
	}
	return "zz/moved/" + base + "-m.md"
}

func TestMoveFixtureRoundTrip(t *testing.T) {
	probe, err := New(copyLinkvault(t))
	if err != nil {
		t.Fatal(err)
	}
	notes, err := probe.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) < 30 {
		t.Fatalf("fixture has %d notes, want 30+", len(notes))
	}
	for _, note := range notes {
		t.Run(note, func(t *testing.T) {
			dir := copyLinkvault(t)
			v, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			dst := movedName(note)
			before := graphEdges(t, v)
			oldFiles := readAll(t, dir)

			res, err := v.MoveNote(context.Background(), note, dst, "", false)
			if err != nil {
				t.Fatalf("MoveNote: %v", err)
			}
			if res.From != note || res.To != dst || res.DryRun {
				t.Errorf("result from %q to %q dry %v", res.From, res.To, res.DryRun)
			}
			if len(res.Failed) != 0 {
				t.Errorf("failed: %v", res.Failed)
			}

			after := graphEdges(t, v)
			assertSameGraph(t, before, after, note, dst, res.Warnings)

			newFiles := readAll(t, dir)
			if _, ok := newFiles[note]; ok {
				t.Errorf("source %s still exists", note)
			}
			if res.Version != VersionOf([]byte(newFiles[dst])) {
				t.Errorf("version %s, want the destination's", res.Version)
			}
			reported := map[string][]LinkEdit{}
			for _, c := range res.Updated {
				reported[c.Path] = c.Links
			}
			for p, newC := range newFiles {
				oldP := p
				if p == dst {
					oldP = note
				}
				oldC, ok := oldFiles[oldP]
				if !ok {
					t.Errorf("new file %s", p)
					continue
				}
				edits, isReported := reported[p]
				if oldC == newC {
					if isReported {
						t.Errorf("%s reported as updated but unchanged", p)
					}
					continue
				}
				if !isReported {
					t.Errorf("%s changed but not reported", p)
				}
				assertOnlyLinksChanged(t, p, oldC, newC, edits)
			}
			for p := range reported {
				if _, ok := newFiles[p]; !ok {
					t.Errorf("reported %s does not exist", p)
				}
			}
		})
	}
}

func TestMoveRewritesAllForms(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "sub/Old.md", "# H\nold note\n")
	linker := "---\nrelated: \"[[Old]]\"\n---\n" +
		"[[Old]] [[Old#H|a]] ![[Old]] [x](Old.md) [x](<sub/Old.md>) [y](sub/Old.md#h) [[sub/Old]] [[/sub/Old]] [[Old#^b]]\n" +
		"| c | [[Old\\|a]] |\n" +
		"`[[Old]]`\n```\n[[Old]]\n```\n"
	writeRaw(t, v, "L.md", linker)
	writeRaw(t, v, "sub/R.md", "[r](./Old.md) [[./Old#H]]\n")

	res, err := v.MoveNote(context.Background(), "sub/Old", "arch/New Name", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.To != "arch/New Name.md" {
		t.Errorf("To = %q", res.To)
	}
	wantL := "---\nrelated: \"[[New Name]]\"\n---\n" +
		"[[New Name]] [[New Name#H|a]] ![[New Name]] [x](New%20Name.md) [x](<arch/New Name.md>) [y](arch/New%20Name.md#h) [[arch/New Name]] [[/arch/New Name]] [[New Name#^b]]\n" +
		"| c | [[New Name\\|a]] |\n" +
		"`[[Old]]`\n```\n[[Old]]\n```\n"
	if got, _ := v.ReadNote("L.md"); got != wantL {
		t.Errorf("L.md =\n%s\nwant\n%s", got, wantL)
	}
	wantR := "[r](../arch/New%20Name.md) [[../arch/New Name#H]]\n"
	if got, _ := v.ReadNote("sub/R.md"); got != wantR {
		t.Errorf("sub/R.md = %q, want %q", got, wantR)
	}
	if got, _ := v.ReadNote("arch/New Name.md"); got != "# H\nold note\n" {
		t.Errorf("moved note = %q", got)
	}
	var paths []string
	for _, c := range res.Updated {
		paths = append(paths, c.Path)
		if c.Path == "L.md" && len(c.Links) != 11 {
			t.Errorf("L.md has %d edits, want 11: %v", len(c.Links), c.Links)
		}
	}
	if strings.Join(paths, ",") != "L.md,sub/R.md" {
		t.Errorf("updated = %v", paths)
	}
	// The index followed the move.
	edges, err := v.GetBacklinks("arch/New Name.md")
	if err != nil || len(edges) != 2 {
		t.Errorf("backlinks = %v, %v", edges, err)
	}
}

func TestMoveKeepsOtherLinksStable(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a/Dup.md", "a\n")
	writeRaw(t, v, "x/Dup.md", "moving. [s](./Sib.md) [[Sib]]\n")
	writeRaw(t, v, "x/Sib.md", "sibling\n")
	writeRaw(t, v, "b/L.md", "[[Dup]]\n")
	writeRaw(t, v, "G.md", "[[New Name]] and [[x/Dup]]\n")
	before := graphEdges(t, v)

	// x/Dup moves next to b/L, whose [[Dup]] (a/Dup) would flip to it.
	res, err := v.MoveNote(context.Background(), "x/Dup.md", "b/Dup.md", "", false)
	if err != nil {
		t.Fatal(err)
	}
	assertSameGraph(t, before, graphEdges(t, v), "x/Dup.md", "b/Dup.md", res.Warnings)
	if got, _ := v.ReadNote("b/L.md"); got != "[[a/Dup]]\n" {
		t.Errorf("b/L.md = %q, want the tie-break kept as [[a/Dup]]", got)
	}
	if got, _ := v.ReadNote("b/Dup.md"); got != "moving. [s](../x/Sib.md) [[Sib]]\n" {
		t.Errorf("moved note = %q, want its ./ link to follow", got)
	}
	if got, _ := v.ReadNote("G.md"); got != "[[New Name]] and [[b/Dup]]\n" {
		t.Errorf("G.md = %q", got)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}

	// A ghost that starts resolving is reported, not rewritten.
	writeRaw(t, v, "Old.md", "old\n")
	before = graphEdges(t, v)
	res, err = v.MoveNote(context.Background(), "Old.md", "New Name.md", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"G.md: [[New Name]] now resolves to New Name.md"}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
	if len(res.Updated) != 0 {
		t.Errorf("updated = %v, want none", res.Updated)
	}
	if got, _ := v.ReadNote("G.md"); got != "[[New Name]] and [[b/Dup]]\n" {
		t.Errorf("G.md = %q, want it unchanged", got)
	}
	assertSameGraph(t, before, graphEdges(t, v), "Old.md", "New Name.md", res.Warnings)
}

func TestMoveRefusesExistingDestination(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "[[b]]\n")
	writeRaw(t, v, "b.md", "b\n")
	writeRaw(t, v, "L.md", "[[a]]\n")
	old := readAll(t, v.Root())
	_, err := v.MoveNote(context.Background(), "a", "b.md", "", false)
	if !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("err = %v, want ErrDestinationExists", err)
	}
	if got := readAll(t, v.Root()); !reflect.DeepEqual(got, old) {
		t.Errorf("files changed: %v", got)
	}
	if _, err := v.MoveNote(context.Background(), "a", "b.md", "", true); !errors.Is(err, ErrDestinationExists) {
		t.Errorf("dry run err = %v, want ErrDestinationExists", err)
	}
}

func TestMoveRefusesBadDestination(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	for _, dst := range []string{"a.md", "a", ".obsidian/a.md", "x.txt", "../out.md", v.Root() + "/b.md"} {
		_, err := v.MoveNote(context.Background(), "a.md", dst, "", false)
		if err == nil {
			t.Errorf("MoveNote to %q succeeded", dst)
			continue
		}
		if strings.Contains(err.Error(), v.Root()) {
			t.Errorf("error %q leaks the root", err)
		}
	}
	if got, err := v.ReadNote("a.md"); err != nil || got != "a\n" {
		t.Errorf("a.md = %q, %v", got, err)
	}
}

func caseInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "case-probe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}

func TestMoveCaseOnly(t *testing.T) {
	v := newTestVault(t)
	if !caseInsensitiveFS(t, v.Root()) {
		t.Skip("file system is case-sensitive")
	}
	writeRaw(t, v, "note.md", "self [[note]]\n")
	writeRaw(t, v, "L.md", "[[note]]\n")
	res, err := v.MoveNote(context.Background(), "note.md", "Note.md", "", false)
	if err != nil {
		t.Fatalf("case-only rename: %v", err)
	}
	if res.To != "Note.md" || len(res.Failed) != 0 {
		t.Errorf("result %+v", res)
	}
	names := dirEntries(t, v.Root())
	if !contains(names, "Note.md") || contains(names, "note.md") {
		t.Errorf("entries %v, want Note.md only", names)
	}
	if got, err := v.ReadNote("Note.md"); err != nil || got != "self [[note]]\n" {
		t.Errorf("Note.md = %q, %v; want the content kept", got, err)
	}
	if got, _ := v.ReadNote("L.md"); got != "[[note]]\n" {
		t.Errorf("L.md = %q, want it unchanged", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type fileState struct {
	data string
	mod  time.Time
}

func snapshotTree(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		st := fileState{mod: info.ModTime()}
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			st.data = string(data)
		}
		out[rel] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMoveDryRunWritesNothing(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "Old.md", "[[L]]\n")
	writeRaw(t, v, "L.md", "[[Old]]\n")
	writeRaw(t, v, "G.md", "[[New]]\n")
	old := snapshotTree(t, v.Root())

	res, err := v.MoveNote(context.Background(), "Old", "dir/New.md", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.To != "dir/New.md" || res.Version != VersionOf([]byte("[[L]]\n")) {
		t.Errorf("result %+v", res)
	}
	want := []LinkChange{{Path: "L.md", Links: []LinkEdit{{Old: "[[Old]]", New: "[[New]]"}}}}
	if !reflect.DeepEqual(res.Updated, want) {
		t.Errorf("updated = %+v, want %+v", res.Updated, want)
	}
	if !reflect.DeepEqual(res.Warnings, []string{"G.md: [[New]] now resolves to dir/New.md"}) {
		t.Errorf("warnings = %q", res.Warnings)
	}
	if got := snapshotTree(t, v.Root()); !reflect.DeepEqual(got, old) {
		t.Errorf("dry run changed the vault:\nbefore %v\nafter  %v", old, got)
	}
}

func TestMoveStaleVersion(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	writeRaw(t, v, "L.md", "[[a]]\n")
	_, err := v.MoveNote(context.Background(), "a.md", "b.md", VersionOf([]byte("other")), false)
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Path != "a.md" || ce.Current != VersionOf([]byte("a\n")) {
		t.Fatalf("err = %v, want a ConflictError for a.md", err)
	}
	if _, err := v.MoveNote(context.Background(), "a.md", "b.md", "bogus", false); !errors.Is(err, errBadVersion) {
		t.Errorf("bad token err = %v", err)
	}
	if !v.NoteExists("a.md") || v.NoteExists("b.md") {
		t.Error("the stale move changed the vault")
	}
	if got, _ := v.ReadNote("L.md"); got != "[[a]]\n" {
		t.Errorf("L.md = %q", got)
	}
	res, err := v.MoveNote(context.Background(), "a.md", "b.md", VersionOf([]byte("a\n")), false)
	if err != nil || res.To != "b.md" {
		t.Errorf("move with the current version: %+v, %v", res, err)
	}
}

func TestMoveLinkerFailureReported(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	writeRaw(t, v, "L1.md", "[[a]]\n")
	writeRaw(t, v, "L2.md", "[[a]]\n")
	failed := errors.New("disk on fire")
	replaceFile = func(r *os.Root, oldRel, newRel string) error {
		if filepath.ToSlash(newRel) == "L1.md" {
			return failed
		}
		return renameReplace(r, oldRel, newRel)
	}
	t.Cleanup(func() { replaceFile = renameReplace })

	res, err := v.MoveNote(context.Background(), "a.md", "b.md", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Path != "L1.md" || !strings.Contains(res.Failed[0].Error, "disk on fire") {
		t.Errorf("failed = %+v", res.Failed)
	}
	if strings.Contains(res.Failed[0].Error, v.Root()) {
		t.Errorf("failure leaks the root: %s", res.Failed[0].Error)
	}
	if len(res.Updated) != 1 || res.Updated[0].Path != "L2.md" {
		t.Errorf("updated = %+v", res.Updated)
	}
	if v.NoteExists("a.md") || !v.NoteExists("b.md") {
		t.Error("the note did not move")
	}
	if got, _ := v.ReadNote("L1.md"); got != "[[a]]\n" {
		t.Errorf("L1.md = %q", got)
	}
	if got, _ := v.ReadNote("L2.md"); got != "[[b]]\n" {
		t.Errorf("L2.md = %q", got)
	}
}

func TestMoveCanceledLinkersFailed(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	writeRaw(t, v, "L1.md", "[[a]]\n")
	writeRaw(t, v, "L2.md", "[[a]]\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	preWriteHook = func(rel string) {
		if filepath.ToSlash(rel) == "b.md" {
			cancel()
		}
	}
	t.Cleanup(func() { preWriteHook = nil })

	res, err := v.MoveNote(ctx, "a.md", "b.md", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenameFailure{{Path: "L1.md", Error: "canceled"}, {Path: "L2.md", Error: "canceled"}}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Errorf("failed = %+v, want %+v", res.Failed, want)
	}
	if v.NoteExists("a.md") || !v.NoteExists("b.md") {
		t.Error("the note did not move")
	}
}

func TestMoveVerifyFailure(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "Old.md", "old\n")
	// The new name's backtick would pair with the one before the link and
	// turn it into inline code.
	writeRaw(t, v, "L.md", "a ` b [[Old]]\n")
	writeRaw(t, v, "M.md", "[[Old]]\n")
	res, err := v.MoveNote(context.Background(), "Old.md", "x`y.md", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenameFailure{{Path: "L.md", Error: "rewrite would change link meaning"}}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Errorf("failed = %+v, want %+v", res.Failed, want)
	}
	if got, _ := v.ReadNote("L.md"); got != "a ` b [[Old]]\n" {
		t.Errorf("L.md = %q, want it unchanged", got)
	}
	if got, _ := v.ReadNote("M.md"); got != "[[x`y]]\n" {
		t.Errorf("M.md = %q", got)
	}
}

func TestMoveSwappedDestDirRefused(t *testing.T) {
	requireDirSymlinks(t)
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	writeRaw(t, v, "sub/n.md", "n\n")
	writeRaw(t, v, "L.md", "[[a]]\n")
	outside := t.TempDir()
	swapSubAfterValidation(t, v, outside)

	_, err := v.MoveNote(context.Background(), "a.md", "sub/a.md", "", false)
	if err == nil {
		t.Fatal("MoveNote through a swapped symlink succeeded")
	}
	if names := dirEntries(t, outside); len(names) != 0 {
		t.Errorf("outside holds %v, want empty", names)
	}
	assertNoPathLeak(t, v, outside, err)
	if got, err := v.ReadNote("a.md"); err != nil || got != "a\n" {
		t.Errorf("a.md = %q, %v; want it kept", got, err)
	}
	if got, _ := v.ReadNote("L.md"); got != "[[a]]\n" {
		t.Errorf("L.md = %q, want it unchanged", got)
	}
}

func BenchmarkMovePlan5k(b *testing.B) {
	dir := b.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	write("target.md", "the note that moves\n")
	for i := 0; i < 5000; i++ {
		body := fmt.Sprintf("# Note %d\n\n[[n%d]] [[n%d]] [x](n%d.md)\n", i, (i+1)%5000, (i+7)%5000, (i+13)%5000)
		if i%100 == 0 { // 50 linkers
			body += "[[target]] ![[target#H|a]]\n"
		}
		write(fmt.Sprintf("f%d/n%d.md", i%50, i), body)
	}
	v, err := New(dir)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := v.linkIndex(); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for b.Loop() {
		res, err := v.MoveNote(ctx, "target.md", "archive/moved.md", "", true)
		if err != nil || len(res.Updated) != 50 {
			b.Fatalf("plan: %d updated, %v", len(res.Updated), err)
		}
	}
}

func TestMoveAbortsWhenOwnLinksUnsafe(t *testing.T) {
	v := newTestVault(t)
	// The moved note's self link would become [[x`y]] after the backtick
	// already on its line: inline code, no link.
	writeRaw(t, v, "Old.md", "a ` b [[Old]]\n")
	writeRaw(t, v, "L.md", "[[Old]]\n")
	// The guard's lock file is not a note write: create it beforehand.
	unlock, err := v.guard.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	old := snapshotTree(t, v.Root())
	for _, dry := range []bool{true, false} {
		_, err := v.MoveNote(context.Background(), "Old.md", "x`y.md", "", dry)
		want := "cannot move Old.md: its own links could not be rewritten safely"
		if err == nil || err.Error() != want {
			t.Errorf("dry %v: err = %v, want %q", dry, err, want)
		}
	}
	if got := snapshotTree(t, v.Root()); !reflect.DeepEqual(got, old) {
		t.Errorf("the refused move changed the vault")
	}
}

func TestMoveRefusesSymlinkSource(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "real.md", "real\n")
	if err := os.Symlink("real.md", filepath.Join(v.Root(), "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, dry := range []bool{true, false} {
		_, err := v.MoveNote(context.Background(), "link.md", "moved.md", "", dry)
		want := "cannot move link.md: it is a symlink; move its target instead"
		if err == nil || err.Error() != want {
			t.Errorf("dry %v: err = %v, want %q", dry, err, want)
		}
	}
	if info, err := os.Lstat(filepath.Join(v.Root(), "link.md")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link.md is no longer a symlink: %v", err)
	}
	if v.NoteExists("moved.md") {
		t.Error("moved.md was created")
	}
}

func TestMoveRefusesLinkUnsafeName(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "a.md", "a\n")
	cases := map[string]string{
		"a#b":      "a#b.md",
		"a|b.md":   "a|b.md",
		"x/[y]":    "x/[y].md",
		"p]q":      "p]q.md",
		"a^b":      "a^b.md",
		"f#g/n.md": "f#g/n.md",
	}
	for dst, norm := range cases {
		_, err := v.MoveNote(context.Background(), "a.md", dst, "", false)
		want := "invalid destination name: " + norm + " contains a character Obsidian links cannot use (# | [ ] ^)"
		if err == nil || err.Error() != want {
			t.Errorf("MoveNote to %q: err = %v, want %q", dst, err, want)
		}
	}
	if got := dirEntries(t, v.Root()); !reflect.DeepEqual(got, []string{"a.md"}) {
		t.Errorf("vault holds %v, want only a.md", got)
	}
}
