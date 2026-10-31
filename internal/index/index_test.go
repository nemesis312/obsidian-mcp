package index_test

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nemesis312/obsidian-mcp/internal/index"
)

// fakeSource is an in-memory vault. It is safe for concurrent use.
type fakeSource struct {
	mu    sync.Mutex
	notes map[string]fakeFile
	atts  map[string]index.FileStat
	reads atomic.Int64
	clock time.Time
	// failRead makes ReadNote fail for a path with the given error.
	failRead map[string]error
	// onStatAtts, when set, runs once inside StatAttachments: after the note
	// walk, before the index diffs it.
	onStatAtts func()
}

type fakeFile struct {
	content string
	mod     time.Time
}

func newFake(notes map[string]string, atts ...string) *fakeSource {
	f := &fakeSource{
		notes: map[string]fakeFile{},
		atts:  map[string]index.FileStat{},
		clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for p, c := range notes {
		f.notes[p] = fakeFile{content: c, mod: f.clock}
	}
	for _, a := range atts {
		f.atts[a] = index.FileStat{Path: a, Size: 1, ModTime: f.clock}
	}
	return f
}

// set writes a note and advances its ModTime.
func (f *fakeSource) set(p, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock = f.clock.Add(time.Second)
	f.notes[p] = fakeFile{content: content, mod: f.clock}
}

func (f *fakeSource) hook(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onStatAtts = fn
}

func (f *fakeSource) setFail(p string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRead == nil {
		f.failRead = map[string]error{}
	}
	if err == nil {
		delete(f.failRead, p)
		return
	}
	f.failRead[p] = err
}

func (f *fakeSource) del(p string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.notes, p)
}

func (f *fakeSource) StatNotes() ([]index.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]index.FileStat, 0, len(f.notes))
	for p, n := range f.notes {
		out = append(out, index.FileStat{Path: p, Size: int64(len(n.content)), ModTime: n.mod})
	}
	return out, nil
}

func (f *fakeSource) StatAttachments() ([]index.FileStat, error) {
	f.mu.Lock()
	hook := f.onStatAtts
	f.onStatAtts = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]index.FileStat, 0, len(f.atts))
	for _, s := range f.atts {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSource) ReadNote(p string) (string, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failRead[p]; err != nil {
		return "", err
	}
	n, ok := f.notes[p]
	if !ok {
		return "", fmt.Errorf("read %s: %w", p, fs.ErrNotExist)
	}
	return n.content, nil
}

func build(t *testing.T, src index.Source) *index.Index {
	t.Helper()
	ix, err := index.Build(src)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func froms(edges []index.Edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.From)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestBuildResolvesEdges(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "see [[B]]", "sub/B.md": "body"}))

	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatalf("Outgoing: %v", err)
	}
	if len(out) != 1 || out[0].To != "sub/B.md" || out[0].ToKind != index.NodeNote || out[0].From != "A.md" || out[0].Raw != "[[B]]" {
		t.Fatalf("Outgoing = %+v", out)
	}
	back, err := ix.Backlinks("sub/B.md")
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if !contains(froms(back), "A.md") {
		t.Fatalf("Backlinks = %+v, want A.md", back)
	}
}

func TestOutgoingUnknownNote(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": ""}))
	if _, err := ix.Outgoing("Nope.md"); err == nil {
		t.Fatal("Outgoing of unknown note: want error")
	}
	if _, err := ix.Backlinks("Nope.md"); err == nil {
		t.Fatal("Backlinks of unknown note: want error")
	}
}

func TestGhostEdge(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[Missing]]"}))
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ToKind != index.NodeGhost || out[0].To != "missing" {
		t.Fatalf("Outgoing = %+v", out)
	}
	if contains(ix.Orphans(), "A.md") {
		t.Fatal("A.md has an Out edge; must not be an orphan")
	}
}

// Ghosts dedupe by the normalized target, as Resolve sees it.
func TestGhostDedupeByNormalizedTarget(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[Ghost]] [[Ghost.md]] [[./Ghost]] [g](Ghost.md) [[sub/../Ghost]]"}))
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ToKind != index.NodeGhost || out[0].To != "ghost" || out[0].Raw != "[[Ghost]]" {
		t.Fatalf("Outgoing = %+v, want one ghost edge to \"ghost\"", out)
	}
	nodes, _ := ix.Graph()
	ghosts := 0
	for _, n := range nodes {
		if n.Kind == index.NodeGhost {
			ghosts++
		}
	}
	if ghosts != 1 {
		t.Fatalf("ghost nodes = %d, want 1: %+v", ghosts, nodes)
	}
}

func TestDedupeByResolvedTarget(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[sub/B]] [[sub/B.md]] [[B]]", "sub/B.md": ""}))
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Raw != "[[sub/B]]" {
		t.Fatalf("Outgoing = %+v, want one edge keeping first Raw", out)
	}
}

func TestAmbiguousEdge(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[Dup]]", "x/Dup.md": "", "y/Dup.md": ""}))
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !out[0].Ambiguous || out[0].To != "x/Dup.md" {
		t.Fatalf("Outgoing = %+v", out)
	}
}

func TestSelfLinkNotBacklink(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[A]]"}))
	back, err := ix.Backlinks("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 0 {
		t.Fatalf("Backlinks = %+v, want none", back)
	}
	if contains(ix.Orphans(), "A.md") {
		t.Fatal("A.md has an Out edge; must not be an orphan")
	}
}

func TestOrphansIncomingAndAttachments(t *testing.T) {
	ix := build(t, newFake(map[string]string{
		"A.md": "[[B]]", "B.md": "", "C.md": "", "D.md": "![[img.png]]",
	}, "img.png"))
	got := ix.Orphans()
	if len(got) != 1 || got[0] != "C.md" {
		t.Fatalf("Orphans = %v, want [C.md]", got)
	}
}

func TestOrphansNoReads(t *testing.T) {
	notes := map[string]string{}
	for i := 0; i < 2000; i++ {
		notes[fmt.Sprintf("n%04d.md", i)] = "plain text"
	}
	src := newFake(notes)
	ix := build(t, src)
	src.reads.Store(0)
	got := ix.Orphans()
	if len(got) != 2000 {
		t.Fatalf("Orphans = %d, want 2000", len(got))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatal("Orphans not sorted")
	}
	if n := src.reads.Load(); n != 0 {
		t.Fatalf("reads = %d, want 0", n)
	}
}

func TestRefreshPicksUpExternalEdit(t *testing.T) {
	src := newFake(map[string]string{"A.md": "nothing", "B.md": ""})
	ix := build(t, src)
	src.set("A.md", "now [[B]]")
	back, err := ix.Backlinks("B.md")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(froms(back), "A.md") {
		t.Fatalf("Backlinks = %+v, want A.md", back)
	}
}

func TestRefreshNoChangeNoReads(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]", "B.md": ""})
	ix := build(t, src)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	src.reads.Store(0)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n := src.reads.Load(); n != 0 {
		t.Fatalf("reads = %d, want 0", n)
	}
}

func TestRefreshNewNoteResolvesGhost(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[New]]"})
	ix := build(t, src)
	out, _ := ix.Outgoing("A.md")
	if len(out) != 1 || out[0].ToKind != index.NodeGhost {
		t.Fatalf("before: %+v", out)
	}
	src.set("New.md", "")
	src.reads.Store(0)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n := src.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 (only the new note)", n)
	}
	out, _ = ix.Outgoing("A.md")
	if len(out) != 1 || out[0].ToKind != index.NodeNote || out[0].To != "New.md" {
		t.Fatalf("after: %+v", out)
	}
}

func TestDeleteRemovesEdges(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]", "sub/B.md": ""})
	ix := build(t, src)
	src.del("sub/B.md")
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ToKind != index.NodeGhost || out[0].To != "b" {
		t.Fatalf("Outgoing = %+v", out)
	}
	if _, err := ix.Backlinks("sub/B.md"); err == nil {
		t.Fatal("Backlinks of deleted note: want error")
	}
}

func TestUpdate(t *testing.T) {
	src := newFake(map[string]string{"A.md": "", "B.md": "[[C]]"})
	ix := build(t, src)

	// New note: path set changes, B's ghost resolves.
	src.set("C.md", "[[A]]")
	if err := ix.Update("C.md"); err != nil {
		t.Fatal(err)
	}
	out, _ := ix.Outgoing("B.md")
	if len(out) != 1 || out[0].ToKind != index.NodeNote || out[0].To != "C.md" {
		t.Fatalf("B out = %+v", out)
	}

	// Read failure removes the note and returns the error.
	src.del("C.md")
	if err := ix.Update("C.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Update err = %v, want ErrNotExist", err)
	}
	if _, ok := ix.Canonical("C.md"); ok {
		t.Fatal("C.md still indexed after failed Update")
	}
}

func TestRemove(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]", "B.md": ""})
	ix := build(t, src)
	src.del("B.md")
	ix.Remove("B.md")
	out, _ := ix.Outgoing("A.md")
	if len(out) != 1 || out[0].ToKind != index.NodeGhost {
		t.Fatalf("Outgoing = %+v", out)
	}
}

func TestCanonical(t *testing.T) {
	ix := build(t, newFake(map[string]string{"sub/Note.md": ""}, "img.png"))
	for _, in := range []string{"sub/Note.md", "SUB/note", `sub\Note.md`} {
		if got, ok := ix.Canonical(in); !ok || got != "sub/Note.md" {
			t.Errorf("Canonical(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ix.Canonical("img.png"); ok {
		t.Error("Canonical must not return attachments")
	}
}

func TestGraph(t *testing.T) {
	ix := build(t, newFake(map[string]string{
		"A.md": "[[B]] [[Ghost]] ![[img.png]]", "B.md": "",
	}, "img.png", "unused.png"))
	nodes, edges := ix.Graph()
	wantNodes := []index.GraphNode{
		{ID: "A.md", Kind: index.NodeNote},
		{ID: "B.md", Kind: index.NodeNote},
		{ID: "img.png", Kind: index.NodeAttachment},
		{ID: "ghost", Kind: index.NodeGhost},
	}
	if fmt.Sprint(nodes) != fmt.Sprint(wantNodes) {
		t.Fatalf("nodes = %v, want %v", nodes, wantNodes)
	}
	if len(edges) != 3 {
		t.Fatalf("edges = %+v", edges)
	}
	order := []index.NodeKind{index.NodeNote, index.NodeAttachment, index.NodeGhost}
	for i, e := range edges {
		if e.From != "A.md" || e.ToKind != order[i] {
			t.Fatalf("edges not sorted by (From, ToKind, To): %+v", edges)
		}
	}
}

func TestQueriesReturnCopies(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[B]]", "B.md": ""}))
	out, _ := ix.Outgoing("A.md")
	out[0].To = "mutated"
	again, _ := ix.Outgoing("A.md")
	if again[0].To != "B.md" {
		t.Fatal("Outgoing returned an internal slice")
	}
}

func TestConcurrentQueries(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]", "B.md": "[[A]]", "C.md": ""})
	ix := build(t, src)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			src.set("C.md", fmt.Sprintf("[[A]] [[N%d]]", i%5))
			if err := ix.Update("C.md"); err != nil {
				t.Errorf("Update: %v", err)
				return
			}
		}
	}()
	var readers sync.WaitGroup
	for g := 0; g < 16; g++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 50; i++ {
				if _, err := ix.Backlinks("A.md"); err != nil {
					t.Errorf("Backlinks: %v", err)
					return
				}
				_ = ix.Orphans()
				_, _ = ix.Graph()
			}
		}()
	}
	readers.Wait()
	close(stop)
	wg.Wait()
}

func TestUpdateDuringStatWalkSurvives(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]"})
	ix := build(t, src)
	src.hook(func() {
		src.set("B.md", "hello")
		if err := ix.Update("B.md"); err != nil {
			t.Errorf("Update: %v", err)
		}
	})
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Canonical("B.md"); !ok {
		t.Fatal("stale Refresh dropped B.md that Update had just added")
	}
	out, _ := ix.Outgoing("A.md")
	if len(out) != 1 || out[0].ToKind != index.NodeNote || out[0].To != "B.md" {
		t.Fatalf("Outgoing = %+v", out)
	}
}

func TestRemoveDuringStatWalkStaysRemoved(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]", "B.md": ""})
	ix := build(t, src)
	src.set("B.md", "changed") // the walk sees B changed and would re-read it
	src.hook(func() {
		src.del("B.md")
		ix.Remove("B.md")
	})
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Canonical("B.md"); ok {
		t.Fatal("stale Refresh resurrected B.md after Remove")
	}
}

func TestConcurrentRefreshStaleWalkKeepsNote(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[B]]"})
	ix := build(t, src)
	// While refresh 1 is between its stat walk (no B) and its diff, B appears
	// and refresh 2 runs. Refresh 1 must not drop B afterwards.
	src.hook(func() {
		src.set("B.md", "")
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := ix.Refresh(); err != nil {
				t.Errorf("Refresh 2: %v", err)
			}
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond): // refreshes are serialized
		}
	})
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	// Let refresh 2 finish if it was waiting, then look at the state without
	// triggering another refresh.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ix.Canonical("B.md"); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("B.md not indexed after concurrent refreshes")
}

func TestUnreadableNote(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[X]]", "B.md": "[[A]]", "X.md": "[[B]]"})
	src.setFail("X.md", errors.New("permission denied"))
	ix, err := index.Build(src)
	if err != nil || ix == nil {
		t.Fatalf("Build = %v, %v; want index and nil error", ix, err)
	}
	back, err := ix.Backlinks("A.md")
	if err != nil || !contains(froms(back), "B.md") {
		t.Fatalf("Backlinks(A) = %+v, %v", back, err)
	}
	// X stays a valid link target with no edges of its own.
	back, err = ix.Backlinks("X.md")
	if err != nil || !contains(froms(back), "A.md") {
		t.Fatalf("Backlinks(X) = %+v, %v", back, err)
	}
	out, err := ix.Outgoing("X.md")
	if err != nil || len(out) != 0 {
		t.Fatalf("Outgoing(X) = %+v, %v", out, err)
	}

	// A failed read records a zero stat, so every Refresh retries it.
	src.reads.Store(0)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n := src.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 (unreadable note retried)", n)
	}

	// A transient failure heals on the next Refresh with no stat change.
	src.setFail("X.md", nil)
	src.reads.Store(0)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n := src.reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1 after the failure cleared", n)
	}
	back, _ = ix.Backlinks("B.md")
	if !contains(froms(back), "X.md") {
		t.Fatalf("Backlinks(B) = %+v, want X.md after the heal", back)
	}
	// Once read, it is not read again until its stat changes.
	src.reads.Store(0)
	if err := ix.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n := src.reads.Load(); n != 0 {
		t.Fatalf("reads = %d, want 0 once healed", n)
	}
}

func TestUpdateUnreadableKeepsNote(t *testing.T) {
	src := newFake(map[string]string{"A.md": "[[X]]", "X.md": "[[A]]"})
	ix := build(t, src)
	src.setFail("X.md", errors.New("permission denied"))
	if err := ix.Update("X.md"); err == nil {
		t.Fatal("Update: want the read error")
	}
	if _, ok := ix.Canonical("X.md"); !ok {
		t.Fatal("unreadable note removed by Update")
	}
	out, _ := ix.Outgoing("X.md")
	if len(out) != 0 {
		t.Fatalf("Outgoing(X) = %+v, want none", out)
	}
	back, _ := ix.Backlinks("X.md")
	if !contains(froms(back), "A.md") {
		t.Fatalf("Backlinks(X) = %+v, want A.md", back)
	}
}

func TestEmptyResultsAreNonNil(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[B]]", "B.md": "plain"}))
	if _, edges := ix.Graph(); edges == nil {
		t.Error("Graph edges nil")
	}
	empty := build(t, newFake(map[string]string{"A.md": ""}))
	if _, edges := empty.Graph(); edges == nil {
		t.Error("Graph edges nil on vault without links")
	}
	if out, _ := ix.Outgoing("B.md"); out == nil {
		t.Error("Outgoing nil")
	}
	if back, _ := ix.Backlinks("A.md"); back == nil {
		t.Error("Backlinks nil")
	}
	if o := ix.Orphans(); o == nil {
		t.Error("Orphans nil")
	}
}

// Many notes sharing one basename and one huge note repeating a link to it
// must not cost refs x candidates: Build and an Update that adds a path both
// stay well under 2s, under -race too.
func TestManyDuplicateBasenamesAndRefs(t *testing.T) {
	const k, refs = 1000, 200_000
	notes := map[string]string{"big.md": strings.Repeat("[[index]] ", refs)}
	for i := 0; i < k; i++ {
		notes[fmt.Sprintf("d%d/index.md", i)] = ""
	}
	src := newFake(notes)

	start := time.Now()
	ix := build(t, src)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Build took %v, want under 2s", d)
	}
	out, err := ix.Outgoing("big.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !out[0].Ambiguous || out[0].To != "d0/index.md" {
		t.Fatalf("Outgoing(big.md) = %+v", out)
	}

	src.set("new/index.md", "")
	start = time.Now()
	if err := ix.Update("new/index.md"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Update adding a path took %v, want under 2s", d)
	}
}

// equivalentSpellings returns n distinct spellings of the wikilink target
// "index" that Resolve treats alike, built from "./" and "./ " prefixes. Each
// differs from the others after links.Key, so only the normalization Resolve
// applies can tell they are the same.
func equivalentSpellings(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var sb strings.Builder
		for b := 0; i>>b > 0; b++ {
			sb.WriteString("./")
			if i&(1<<b) != 0 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("index")
		out = append(out, sb.String())
	}
	return out
}

// Equivalent spellings of one target (S1 bypass): a note holding 20k
// distinct spellings of a basename shared by 1000 notes must not cost
// spellings x candidates, and every spelling resolves to the same edge.
func TestEquivalentSpellingsBounded(t *testing.T) {
	const k, n = 1000, 20_000
	variants := equivalentSpellings(n)
	seen := map[string]bool{}
	var sb strings.Builder
	for _, v := range variants {
		if seen[v] {
			t.Fatalf("spelling %q repeats", v)
		}
		seen[v] = true
		sb.WriteString("[[" + v + "]] ")
	}
	notes := map[string]string{"big.md": sb.String()}
	for i := 0; i < k; i++ {
		notes[fmt.Sprintf("d%d/index.md", i)] = ""
	}

	start := time.Now()
	ix := build(t, newFake(notes))
	if d := time.Since(start); d > time.Second {
		t.Errorf("Build took %v, want under 1s", d)
	}
	out, err := ix.Outgoing("big.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !out[0].Ambiguous || out[0].To != "d0/index.md" || out[0].Raw != "[["+variants[0]+"]]" {
		t.Fatalf("Outgoing(big.md) = %+v, want one ambiguous edge to d0/index.md", out)
	}
}

// The resolve memo is keyed on the normalized target: 20k notes in one
// folder, each with its own spelling of a basename shared by 1000 notes,
// resolve it once.
func TestEquivalentSpellingsAcrossNotesMemoized(t *testing.T) {
	const k, n = 1000, 20_000
	notes := map[string]string{}
	for i, v := range equivalentSpellings(n) {
		notes[fmt.Sprintf("n%05d.md", i)] = "[[" + v + "]]"
	}
	for i := 0; i < k; i++ {
		notes[fmt.Sprintf("d%d/index.md", i)] = ""
	}

	start := time.Now()
	ix := build(t, newFake(notes))
	if d := time.Since(start); d > time.Second {
		t.Errorf("Build took %v, want under 1s", d)
	}
	_, edges := ix.Graph()
	got := 0
	for _, e := range edges {
		if !strings.HasPrefix(e.From, "n") {
			continue
		}
		got++
		if !e.Ambiguous || e.To != "d0/index.md" {
			t.Fatalf("edge %+v, want an ambiguous edge to d0/index.md", e)
		}
	}
	if got != n {
		t.Fatalf("edges from the n notes = %d, want %d", got, n)
	}
}

// A target that is only "/" or "./" names nothing: no edge, no ghost node
// with an empty id.
func TestRootOnlyTargetHasNoEdge(t *testing.T) {
	ix := build(t, newFake(map[string]string{"A.md": "[[/]] [[./]] [r](/) [[B]]", "B.md": ""}))
	out, err := ix.Outgoing("A.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].To != "B.md" {
		t.Fatalf("Outgoing = %+v, want only the edge to B.md", out)
	}
	nodes, edges := ix.Graph()
	for _, n := range nodes {
		if n.ID == "" {
			t.Fatalf("graph node with empty id: %+v", nodes)
		}
	}
	for _, e := range edges {
		if e.To == "" {
			t.Fatalf("graph edge with empty To: %+v", edges)
		}
	}
}
