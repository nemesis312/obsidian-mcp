# v2 Phase 1: Link Resolver + Live Index (Implementation Plan)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** All graph features (backlinks, outgoing links, orphans, link graph) use a single resolver that follows Obsidian's rules. They are answered from an in-memory index that a cheap stat walk keeps current.

**Architecture:** There are two new packages.
- `internal/links` is pure, with no I/O. It extracts link references from note text and resolves each one against the vault's set of paths.
- `internal/index` builds and refreshes the parsed vault through a small `Source` interface. `vault.Vault` implements that interface, owns one `*index.Index`, and serves the graph methods from it.

The markdown files remain the source of truth. The index lives only in memory.

**Tech Stack:** Go 1.26.1, `mark3labs/mcp-go` v0.48.0, `gopkg.in/yaml.v3`, and one new pure-Go dependency, `golang.org/x/text/unicode/norm` (NFC).

**Spec:** `docs/superpowers/specs/2026-10-30-memory-graph-design.md` (§7.1, §7.2). Roadmap: `docs/superpowers/plans/2026-10-30-v2-roadmap.md`.

## Global Constraints

- Single static binary, `CGO_ENABLED=0`. No runtime dependencies or external services. Pure-Go modules are allowed (`golang.org/x/text`).
- The markdown files are the source of truth. Deleting the index loses nothing.
- Every security property from v1.x stays as it is: containment is checked before any FS call, symlink targets must be `.md` for writes, writes are atomic, and `.git`/`.obsidian` are blocked. All existing tests stay green.
- Paths in the index and in tool output are vault-relative and use forward slashes (`/`) on every OS. They never contain absolute paths.
- Matching keys are case-insensitive and Unicode NFC-normalized.
- Text inside fenced code blocks and inline code is never parsed for links.
- CI must pass on ubuntu, macos and windows: tidy check, vet, gofmt, race tests.
- Tool names stay the same in this phase. Only the JSON output shapes change, and v2 allows that. Final renames happen in phase 6.

## Review Focus

1. **Windows separators.** `ListFiles` returns `sub\Note.md` on Windows. A reasonable person expects `sub/Note.md` in every result, and backlinks must work no matter which separator the caller passes. (Test in Task 5.)
2. **A note edited outside the server between two calls** (by a human in Obsidian, or another agent). The next graph call must reflect the edit without restarting the server. (Test in Task 4.)
3. **Caller path casing differs from disk** (`notes/a.md` vs `Notes/A.md`) on a case-sensitive FS. `get_backlinks` must still find the note instead of returning an empty list. (Test in Task 5.)
4. **Garbage or huge input.** A 10 MB note, a note with no trailing newline, unterminated `[[`, `` ``` `` without a closing fence, or invalid UTF-8. Extraction must not panic and must not hang. (Test in Task 2.)
5. **Link spelling variants.** `[[ Note ]]`, `[[Note.md]]`, `[[Note.MD]]` and `[[./Note]]` should all resolve to `Note.md` the way Obsidian does. (Test in Task 3.)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/markdown/codemask.go` (new) | `MaskCode`: blank out the code regions, keeping byte offsets. The fence and inline-code scanning moves here out of `rename.go`. |
| `internal/markdown/rename.go` (modify) | Uses the shared scanner from `codemask.go`. Behavior is unchanged. |
| `internal/links/extract.go` (new) | `Ref`, `Kind`, `Extract`: all link occurrences in a note. |
| `internal/links/resolve.go` (new) | `Key`, `Resolver`, `Resolution`: Obsidian resolution rules. |
| `internal/index/index.go` (new) | `Source`, `Index`, `Note`, `Edge`, `NodeKind`, `Build`, `Refresh`, and the query methods. |
| `internal/index/testdata/linkvault/**` (new) | The fixture vault for §7.1. |
| `internal/vault/vault.go`, `io.go` (modify) | The vault implements `index.Source`, owns the index, and writes update the index. `ListAttachments` is added. |
| `internal/vault/graph.go` (rewrite) | The graph methods delegate to the index. |
| `internal/vault/cache.go` (delete) | Dead code: nothing reads it. |
| `internal/tools/graph_tools.go` (modify) | The new output shapes. |

---

### Task 1: Shared code masking

**Files:**
- Create: `internal/markdown/codemask.go`
- Create: `internal/markdown/codemask_test.go`
- Modify: `internal/markdown/rename.go` (move `openingFence`, `isClosingFence` and the inline-code span scan into `codemask.go`, then use them)

**Interfaces:**
- Produces: `func MaskCode(content string) string`. It returns a string of exactly `len(content)` bytes. Every byte inside a fenced code block (including the fence lines) or an inline code span (including the backticks) becomes a space. `\n` and `\r` are preserved. Every other byte is unchanged.
- Produces: `func codeRanges(content string) [][2]int` (unexported). These are the byte ranges of the code regions, used by `MaskCode` and by `rename.go`.

- [ ] **Step 1: Write the failing test** `TestMaskCode` (table-driven). For every case, assert `len(got)==len(in)` and that the `\n` positions are unchanged.
  - `"a [[X]] b"` → unchanged
  - ``"`[[X]]` [[Y]]"`` → `"        [[Y]]"`
  - ``"x\n```\n[[X]]\n```\n[[Y]]"`` → the lines between and including the fences become spaces, and `[[Y]]` is kept
  - `"~~~\n[[X]]\n~~~"` → masked
  - ``"````\n```\n[[X]]\n````\n[[Y]]"`` → `[[Y]]` is kept
  - ``"` [[X]]"`` (unclosed backtick) → unchanged
  - ``"```\n[[X]]"`` (unclosed fence) → masked to the end of the content
  - `"#a\r\n```\r\n#b\r\n```\r\n"` → `\r` is preserved
- [ ] **Step 2: Run** `go test ./internal/markdown -run TestMaskCode`. Expected: FAIL, `undefined: MaskCode`.
- [ ] **Step 3: Implement `MaskCode` and `codeRanges` in `codemask.go`.** Move the fence logic and the inline-code run matching from `rename.go`. Refactor `RenameInline` to skip the ranges returned by `codeRanges`.
- [ ] **Step 4: Run** `go test -race ./internal/markdown`. Expected: PASS, including every existing `rename_test.go` case unchanged.
- [ ] **Step 5: Commit** `refactor(markdown): extract code-region masking shared by tags and links`

---

### Task 2: Link extraction

**Files:**
- Create: `internal/links/extract.go`
- Create: `internal/links/extract_test.go`

**Interfaces:**
- Consumes: `markdown.MaskCode`.
- Produces:
```go
type Kind int
const (
    KindWikilink Kind = iota // [[...]]
    KindEmbed                // ![[...]]
    KindMarkdown             // [text](target)
)
type Ref struct {
    Kind    Kind
    Raw     string // exact source text, e.g. "![[Note#H|A]]"
    Target  string // decoded target without heading/block/alias; may contain "/" and an extension
    Heading string // after '#', without '^'
    Block   string // after '#^'
    Alias   string // after '|' (wikilink) or the link text (markdown)
    Offset  int    // byte offset of Raw in the original content
}
func Extract(content string) []Ref // in source order; scans the whole content including frontmatter
```

- [ ] **Step 1: Write the failing tests.** `TestExtract` is table-driven; each case asserts the full `[]Ref` with `Offset`:
  - `[[Note]]` → `{KindWikilink, Target:"Note"}`
  - `[[Note|Al]]` → `Alias:"Al"`
  - `[[Note#H]]` → `Heading:"H"`
  - `[[Note#^b1]]` → `Block:"b1"`
  - `[[Note#H|Al]]` → both set
  - `![[Note]]` → `KindEmbed`
  - `![[img.png]]` → `Target:"img.png"`
  - `| [[T4\|Alias]] |` → `Target:"T4"`, `Alias:"Alias"`
  - `[t](Note.md)` → `{KindMarkdown, Target:"Note.md", Alias:"t"}`
  - `[t](sub/Note.md)`
  - `[t](Note%20Name.md)` → `Target:"Note Name.md"`
  - `[t](<Note Name.md>)` → `Target:"Note Name.md"`
  - `[t](Note.md#Sec)` → `Heading:"Sec"`
  - ignored, producing no Ref: `[t](https://x.y)`, `[t](mailto:a@b)`, `[t](#local)`, `![alt](https://x/i.png)`
  - frontmatter `related: "[[FM1]]"` and a YAML list `- "[[FM2]]"` → both extracted
  - `` `[[InCode]]` `` and a fenced `[[InCode]]` → none
  - `[[ Note ]]` → `Target:"Note"` (trimmed)
  - `[[]]` and `[[#H]]` → `[[#H]]` gives `Target:""`, `Heading:"H"` (a link to the same note); `[[]]` is skipped

  `TestExtractRobust` asserts no panic and that each case completes in under 1 second:
  - 10 MB of `[[` with no closing brackets
  - an unclosed fence
  - invalid UTF-8 bytes (`"\xff[[A]]"` → extracts `A`)
- [ ] **Step 2: Run** `go test ./internal/links -run TestExtract`. Expected: FAIL, the package does not exist.
- [ ] **Step 3: Implement `Extract`.** Run the regexes over `MaskCode(content)`, and slice `Raw` from the original content at the same offsets. Wikilinks use the pattern `(!?)\[\[([^\[\]\n]*?)\]\]`. Handle the `\|` escape before splitting on `|`. Markdown links use the pattern `(!?)\[([^\]\n]*)\]\((<[^>\n]+>|[^)\s]+)\)`. Skip any target that has a URL scheme (`^[a-zA-Z][a-zA-Z0-9+.-]*:`) or starts with `#`. Use `url.PathUnescape` and keep the raw target if unescaping fails. The regexes have no backtracking, so the 10 MB case stays linear.
- [ ] **Step 4: Run** `go test -race ./internal/links`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(links): extract wikilinks, embeds and markdown links outside code`

---

### Task 3: Resolver (Obsidian rules)

**Files:**
- Create: `internal/links/resolve.go`
- Create: `internal/links/resolve_test.go`
- Modify: `go.mod`, `go.sum` (add `golang.org/x/text`)

**Interfaces:**
- Consumes: `Ref` from Task 2.
- Produces:
```go
func Key(s string) string // strings.ToLower(norm.NFC.String(s))
type Status int
const ( Resolved Status = iota; Ambiguous; Unresolved )
type Resolution struct {
    Status     Status
    Path       string   // vault-relative, "/" separators; set for Resolved and Ambiguous (the tie-break winner)
    Candidates []string // all matches when Ambiguous, sorted by tie-break order
    Attachment bool     // Path is a non-.md file
}
type Resolver struct{ /* unexported */ }
func NewResolver(notes, attachments []string) *Resolver // paths vault-relative with "/"
func (r *Resolver) Resolve(fromNote string, ref Ref) Resolution
func (r *Resolver) Lookup(path string) (string, bool) // case/NFC-insensitive exact path match, ".md" optional; returns canonical path
```

Rules, in order. The executor must apply them exactly.
1. Empty `Target` (`[[#H]]`) → `Resolved` to `fromNote`.
2. Normalize the target: trim whitespace and a leading `./`. A trailing `.md` (any case) is optional for notes.
3. For `KindMarkdown`, first try the target relative to `path.Dir(fromNote)` (cleaned). If that yields a path inside the vault that exists, it wins.
4. If the target contains `/`, do an exact path match from the vault root (by `Key`, with `.md` optional).
5. Otherwise, do a basename match (by `Key`, the basename without `.md`) across all notes.
   - With one match → `Resolved`.
   - With more than one → `Ambiguous`, with `Path` set to the tie-break winner. The tie-break order is: same folder as `fromNote` first, then the fewest path segments, then lexicographic order of `Key(path)`.
6. If the target has a non-`.md` extension, match it against `attachments` with the same rules 3–5, and set `Attachment:true`.
7. If nothing matches → `Unresolved`.

- [ ] **Step 1: Write the failing tests.** `TestResolve`, with notes `Note.md`, `sub/dir/Note2.md`, `a/Dup.md`, `b/c/Dup.md`, `CaseTarget.md`, `café.md` (NFC), `v1.2 notes.md` and `x/Rel.md`, plus the attachment `assets/img.png`:
  - `[[Note2]]` → `sub/dir/Note2.md` Resolved
  - `[[Dup]]` from `a/Other.md` → Ambiguous, Path `a/Dup.md`, Candidates `[a/Dup.md b/c/Dup.md]`
  - `[[Dup]]` from `z.md` → Path `a/Dup.md` (fewer segments)
  - `[[b/c/Dup]]` → Resolved `b/c/Dup.md`
  - `[[casetarget]]` → `CaseTarget.md`
  - `[[café]]` (NFD) → `café.md`
  - `[[v1.2 notes]]` → Resolved
  - `[[ Note ]]`, `[[Note.md]]`, `[[Note.MD]]`, `[[./Note]]` → `Note.md` (Review Focus 5)
  - markdown `Rel.md` from `x/Other.md` → `x/Rel.md`, and from `y.md` → also `x/Rel.md` via basename (Obsidian behavior)
  - `![[img.png]]` → `assets/img.png`, `Attachment:true`
  - `[[Ghost]]` → Unresolved
  - `[[#H]]` from `Note.md` → `Note.md`
  - `../outside` from `Note.md` → Unresolved (never escapes the vault)

  `TestLookup`: `Lookup("notes/a")` finds `Notes/A.md`; `Lookup("missing")` → false.
- [ ] **Step 2: Run** `go test ./internal/links -run 'TestResolve|TestLookup'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep three maps: path key → canonical path, basename key → []paths (pre-sorted per tie-break, minus the same-folder rule, which is applied at resolve time), and the same pair for attachments. Run `go get golang.org/x/text@latest && go mod tidy`.
- [ ] **Step 4: Run** `go test -race ./internal/links`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(links): resolve links with Obsidian rules (basename, case, NFC, ambiguity)`

The one-time check against the real Obsidian app runs at phase exit, because the fixture vault only exists after Task 6.

---

### Task 4: Index with stat-walk refresh

**Files:**
- Create: `internal/index/index.go`
- Create: `internal/index/index_test.go`

**Interfaces:**
- Consumes: `links.Extract`, `links.NewResolver`, `links.Resolver.Resolve`, `links.Resolver.Lookup`, and `markdown.ParseFrontmatter`.
- Produces:
```go
type FileStat struct { Path string; Size int64; ModTime time.Time } // Path vault-relative, "/"
type Source interface {
    StatNotes() ([]FileStat, error)       // all .md notes
    StatAttachments() ([]FileStat, error) // all non-.md, non-.canvas files outside protected/hidden dirs
    ReadNote(path string) (string, error)
}
type NodeKind int
const ( NodeNote NodeKind = iota; NodeAttachment; NodeGhost )
type Edge struct {
    From      string     // note path
    To        string     // path for note/attachment; Key(target) for ghost
    ToKind    NodeKind
    Kind      links.Kind
    Ambiguous bool
    Raw       string     // first occurrence's Raw
}
type Note struct { Path string; Frontmatter map[string]any; Out []Edge } // Out deduped by (ToKind, To)
type Index struct{ /* sync.RWMutex, notes map, stats map, resolver, in map[string][]Edge */ }
func Build(src Source) (*Index, error)
func (ix *Index) Refresh() error              // stat walk; re-read only changed/new; drop deleted; re-resolve all edges if the path set changed
func (ix *Index) Update(path string) error    // re-read one note now (used after server writes)
func (ix *Index) Remove(path string)
func (ix *Index) Canonical(path string) (string, bool) // via Resolver.Lookup; accepts "\" or "/"
func (ix *Index) Outgoing(path string) ([]Edge, error) // error if note not found
func (ix *Index) Backlinks(path string) ([]Edge, error) // excludes self-links
func (ix *Index) Orphans() []string            // notes with no Out edges (any ToKind) and no incoming edges from other notes; sorted
func (ix *Index) Graph() (nodes []GraphNode, edges []Edge) // nodes include ghosts and linked attachments; sorted
type GraphNode struct { ID string; Kind NodeKind }
```
- All query methods call `Refresh()` first (Review Focus 2). `Refresh` must cost one stat walk and zero reads when nothing changed.

- [ ] **Step 1: Write the failing tests** using an in-memory fake `Source` (a map with stats and contents, plus read counters):
  - `TestBuildResolvesEdges`: two notes `A.md` → `[[B]]` and `sub/B.md`. Assert `Outgoing("A.md")` has one edge `To:"sub/B.md"`, `ToKind:NodeNote`, and that `Backlinks("sub/B.md")` contains `A.md`.
  - `TestGhostEdge`: `[[Missing]]` → an edge with `ToKind:NodeGhost`, `To:"missing"`. The note is not in `Orphans()`.
  - `TestDedupeByResolvedTarget`: `[[sub/B]] [[sub/B.md]] [[B]]` → exactly one edge.
  - `TestSelfLinkNotBacklink`: `A.md` containing `[[A]]` → `Backlinks("A.md")` is empty, and `Orphans()` excludes A because it has an Out edge.
  - `TestOrphansNoReads`: build over 2,000 link-less notes, reset the read counter, call `Orphans()` → 2,000 results and **0 reads**.
  - `TestRefreshPicksUpExternalEdit`: build, then change `A.md`'s content and ModTime in the fake. `Backlinks` reflects the new link without any `Update` call.
  - `TestRefreshNoChangeNoReads`: a second `Refresh()` with no changes → 0 reads.
  - `TestRefreshNewNoteResolvesGhost`: `[[New]]` is a ghost, then `New.md` is added to the fake → after `Refresh`, the edge is a `NodeNote`.
  - `TestDeleteRemovesEdges`: removing `sub/B.md` from the fake → `A.md`'s edge becomes a ghost.
  - `TestConcurrentQueries`: 16 goroutines calling `Backlinks`, `Orphans` and `Graph` while one goroutine calls `Update` in a loop; run under `-race`.
- [ ] **Step 2: Run** `go test ./internal/index`. Expected: FAIL.
- [ ] **Step 3: Implement.** Store parsed refs per note; do not re-read a note to re-resolve it. When the path set changes, rebuild the resolver and re-resolve the stored refs of every note. That is O(total refs) in memory, with no I/O. Rebuild the `in` map on every edge change.
- [ ] **Step 4: Run** `go test -race ./internal/index`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(index): in-memory vault index with stat-walk refresh and ghost nodes`

---

### Task 5: Wire the vault and tools to the index

**Files:**
- Modify: `internal/vault/vault.go` (add an `index *index.Index` field built lazily on the first graph call, guarded by `sync.Once` plus an error; drop the `cache` field)
- Modify: `internal/vault/io.go` (add `StatNotes` and `StatAttachments` with slash paths and the same skip rules as `ListFiles`; `ListFiles` returns slash paths; after every successful write or delete, call `v.index.Update` or `Remove` if the index exists; remove `invalidate`)
- Delete: `internal/vault/cache.go`
- Rewrite: `internal/vault/graph.go`
- Modify: `internal/tools/graph_tools.go`
- Test: `internal/vault/graph_test.go` (new), `internal/tools/graph_tools_test.go` (new)

**Interfaces:**
- Consumes: everything Task 4 produces.
- Produces (vault):
```go
func (v *Vault) GetBacklinks(path string) ([]index.Edge, error)
func (v *Vault) GetOutgoingLinks(path string) ([]index.Edge, error)
func (v *Vault) GetOrphanedNotes() ([]string, error)
func (v *Vault) BuildLinkGraph() ([]index.GraphNode, []index.Edge, error)
```
  `GetLinkedMentions` stays as it is (it is unlinked text search; phase 4 replaces it).
- Tool output (JSON, compact, not indented). `ToKind` is serialized as `"note" | "attachment" | "ghost"`, and `kind` as `"wikilink" | "embed" | "markdown"`.
  - `get_backlinks` → `{"path":<canonical>,"backlinks":[{"from":...,"kind":...}],"count":N}`
  - `get_outgoing_links` → `{"path":...,"links":[{"to":...,"to_kind":...,"kind":...,"ambiguous":bool}],"count":N}`
  - `get_orphaned_notes` → `{"orphans":[...],"count":N}`
  - `get_link_graph` → `{"nodes":[{"id","kind"}],"edges":[{"from","to","to_kind","kind"}],"node_count","edge_count"}`. It is still unbounded in this phase, and phase 4 bounds it. The description says so: "Full graph; can be large on big vaults."
- A path argument is resolved through `index.Canonical`. When it isn't found, the error is `note not found: <arg as given>`, with no absolute path.

- [ ] **Step 1: Write the failing tests.**
  - `TestVaultGraphAgainstDisk` (in `graph_test.go`, using a real `t.TempDir()` vault): `A.md` → `[[B]]`, `sub/B.md`. Then:
    - `GetBacklinks("sub/B.md")` contains from `A.md`.
    - `GetBacklinks(filepath.FromSlash("sub/B.md"))` gives the same result (Review Focus 1).
    - `GetBacklinks("SUB/b")` gives the same result (Review Focus 3).
    - `GetOutgoingLinks("A.md")[0].To == "sub/B.md"` on every OS.
  - `TestVaultExternalEditVisible`: write `C.md` with `os.WriteFile`, bypassing the vault, containing `[[B]]` → it appears in the backlinks of `sub/B.md`.
  - `TestVaultWriteUpdatesIndex`: `v.AppendToNote("A.md", "[[New]]")`, then `v.CreateNote("New.md", ...)` → the `A.md` edge to `New.md` is a note edge.
  - `TestGraphToolsJSON` (in `graph_tools_test.go`, following the handler-test pattern in `write_tools_test.go`): call each of the four tools and unmarshal the result. Assert the field names above, that `"\\"` never appears in any path, that `to_kind:"ghost"` appears for a missing target, and that the error text for `get_backlinks` with path `nope` is exactly `note not found: nope`.
- [ ] **Step 2: Run** `go test ./internal/vault ./internal/tools`. Expected: FAIL (types and shapes).
- [ ] **Step 3: Implement** the changes listed above. `ListFiles` callers elsewhere (`read_tools`, `tags.go`, `SearchVault`) keep working with slash paths, because `ValidatePath` accepts either separator. Confirm this by running their existing tests.
- [ ] **Step 4: Run** `gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race ./...`. Expected: no gofmt output, vet clean, all PASS.
- [ ] **Step 5: Commit** `feat(graph): serve backlinks, outgoing, orphans and graph from the index`

---

### Task 6: Fixture vault, end-to-end check, benchmark, docs

**Files:**
- Create: `internal/index/testdata/linkvault/**`. One small note per resolution case in Tasks 2–3, plus `README.md` inside the fixture, listing each case and the edge Obsidian should show.
- Create: `internal/index/fixture_test.go`
- Create: `internal/index/bench_test.go`
- Modify: `README.md` (a "How links are resolved" section with rules 1–7 from Task 3, and the new output shapes of the four graph tools)

**Interfaces:**
- Consumes: `index.Build` over a disk-backed `Source`. Use `vault.New(fixturePath)` and the vault as the Source, so the real stat and read path is exercised.

- [ ] **Step 1: Write the failing test** `TestFixtureEdges`. Build the index over `testdata/linkvault` and compare `Graph()` edges (sorted) against an expected table in the test. The table covers every Task 2–3 case: basename in a subfolder, duplicates, case, NFC (the file is written as NFD by the test setup into a temp copy, because git may normalize names), all 4 markdown-link forms, the table pipe, an embed of a note, an embed of an attachment, frontmatter links, a code-block link absent, a ghost present, and dots in the name.
- [ ] **Step 2: Run** `go test ./internal/index -run TestFixtureEdges`. Expected: FAIL until the fixture files exist, then PASS. If it FAILS after the fixture exists, the bug is in Tasks 2–4: fix it there, not in the expectations.
- [ ] **Step 3: Write the benchmarks** `BenchmarkBuild5k`, `BenchmarkRefreshNoChange5k` and `BenchmarkOrphans5k`. In `b.TempDir`, generate 5,000 notes across 50 folders: 0.5–3 KB each, 3–10 basename wikilinks, a fixed `rand` seed. Also add `TestOrphans5kLinkless` (no links at all), which asserts it completes in under 2 seconds. This catches a regression to O(N²); the old code extrapolates to about 8 minutes.
- [ ] **Step 4: Run** `go test -race ./... && go test -bench . -benchtime 3x -run ^$ ./internal/index`. Expected: PASS. Paste the benchmark output into the PR description.
- [ ] **Step 5: Commit** `test(index): Obsidian link-resolution fixture and 5k-note benchmarks`

---

### Phase exit (before opening the PR)

- [ ] **Manual Obsidian check (one time).** Open `internal/index/testdata/linkvault` in Obsidian. Confirm that the graph view and the backlinks pane match `TestFixtureEdges`, especially the duplicate-basename winner (rule 5) and markdown-relative links (rule 3). If Obsidian differs, change the rule, its test and the fixture expectation, and write the observed behavior in a comment at the top of `resolve.go`. If no Obsidian is available, write "unverified against Obsidian" there and in the PR description.
- [ ] An independent security review of the diff by a fresh reviewer, focused on: `StatAttachments` must not list or expose files outside the vault, inside protected dirs, or through symlinks escaping the vault; and no absolute paths in any tool output or error. Fix every Medium or higher finding.
- [ ] Open the PR against `develop`. The description includes the benchmark numbers, the Obsidian verification result (or "unverified") and the changed tool output shapes. CI must be green on all three OSes.
