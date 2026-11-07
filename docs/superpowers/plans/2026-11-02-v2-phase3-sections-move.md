# v2 Phase 3: Section Edits, Move/Rename, Writes Through os.Root (Implementation Plan)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:**
- An agent can replace, append to or prepend to the content under one heading, and every other byte of the note stays the same.
- An agent can move or rename a single `.md` note. Every link whose target the move would change is rewritten, so the vault's graph is the same before and after, apart from the new path. The tool has a dry run and reports failures per file.
- Every filesystem write goes through `os.Root`, which closes the validate-then-write window deferred from phase 2 (SR-3).

**Architecture:**
- `internal/vault/atomic*.go` moves from absolute `os.*` calls to an `*os.Root` opened on the vault and vault-relative names. Containment is then enforced by the kernel-backed root at the moment of each write, not only at validation time.
- `internal/markdown/sections.go` is a pure heading and section parser plus three edit functions. The `edit_section` tool wraps it with the phase 2 `modifyNote` (guard, re-read, version check).
- `internal/links/rewrite.go` is a pure engine. It decides the new link text for a target and splices it into the original `Raw`, keeping alias, heading, block, embed marker, escaped pipe, `<…>` form, `%` encoding and `.md` presence. It also applies byte-exact edits.
- `internal/vault/move.go` plans the move by comparing every note's references under the old and new path sets. It verifies each rewritten file by re-resolving it. Then it applies the move under one guard hold.

**Tech Stack:** Go 1.26.1 (`os.Root` with `Rename`, `Link`, `Remove`, `MkdirAll`, `OpenFile`), `mark3labs/mcp-go` v0.48.0, `golang.org/x/text`, `gopkg.in/yaml.v3`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-30-memory-graph-design.md` §6 (write scope, concurrency) and §7.3 (section edits, optimistic concurrency, move/rename). Roadmap: `docs/superpowers/plans/2026-10-30-v2-roadmap.md`, phase 3. Previous phase: `docs/superpowers/plans/2026-10-31-v2-phase2-addressing-versions-guard.md`.

Markers:
- `**[Qn]**` is a choice the spec leaves open. The maintainer approved the default written inline on 2026-11-02 (see "Decided questions" at the end).
- `*(planner's call)*` is a minor choice settled here.

## Global Constraints

- `CGO_ENABLED=0`, one static binary, no new dependencies, and no data outside the `.md` files (§5).
- Section edits: "The note's other bytes are left exactly as they were" (§7.3). Nothing outside the edited section and the inserted text changes: not the frontmatter, not the line endings, not the BOM.
- Tokens (§7.3):
  - "The token is **required** for whole-note replace, section replace and delete."
  - "Additive operations (create, append, section append/prepend) may omit it. They re-read the file under the write guard and apply only their addition."
- Move and rename (§7.3):
  - "rewrite every inbound link: wikilinks, markdown links and frontmatter links".
  - "Rewriting preserves aliases, headings and block refs, and the link style used (`[[name]]` stays name-only when it is still unambiguous; otherwise it becomes path-qualified)".
  - "reports every file it changed and every file it failed to update", "refuses if the destination already exists", and has "A dry-run option".
  - Single `.md` notes only. Folders and attachments are out of scope.
- "Move/rename is guarded against concurrent writers" (§6). Every write takes the phase 2 `writeGuard`. The guard is not reentrant, so code that already holds it never calls a function that takes it.
- Anti-goal (§5): "Writing notes that Obsidian renders or links differently from what the MCP reported." Every rewritten link is checked by re-extracting and re-resolving it before the file is written.
- Every v1.x and phase 2 security property stays:
  - containment is checked before any FS call
  - write symlink targets must be `.md`
  - protected segments (`.git`, `.obsidian`, `.obsidian-mcp`, the Windows aliases)
  - no absolute path in any output
  - atomic writes
- Paths in output use `/` and are vault-relative. Tool descriptions are in English.
- Links the MCP writes are never wrapped in backticks (spec §7.5 rule). This holds for rewritten links too.
- Existing tool names do not change, and new tools are additive. Renames belong to phase 6.
- CI runs on ubuntu, macos and windows: tidy check, vet, gofmt and `go test -race` (no `-race` on Windows).

## Review Focus

1. **The same note is linked in many forms.** For example, a linker contains `[[Old]]`, `[[Old#H|a]]`, `![[Old]]`, `[x](Old.md)`, `[x](<sub/Old.md>)`, `[x](sub/Old%20Name.md#h)`, `[[Old\|a]]` inside a table, and `related: "[[Old]]"` in frontmatter. After the move, every one of them resolves to the new path and keeps its alias, fragment and form. A link inside a code block or inline code is left as written. (Test in Task 4.)
2. **The move changes what other links resolve to.**
   - Moving `x/Dup.md` next to a linker flips the same-folder tie-break of an unrelated `[[Dup]]`.
   - A note that moves away loses its own `./` relative links.
   - A ghost `[[New Name]]` starts resolving.

   The graph must stay identical, or the change must be reported. It must never change silently. (Test in Task 5.)
3. **The section heading is not where the agent expects.**
   - The heading has a trailing `#`s, a setext underline, or extra spaces.
   - It appears twice.
   - It sits inside a fenced code block.
   - It is in the frontmatter.
   - The section is the last one in a file that has no final newline.

   Each case either edits the right bytes or returns an error listing the headings. No case writes to the wrong place. (Test in Task 2.)
4. **CRLF notes written on Windows.** A section append or a link rewrite in a CRLF file keeps CRLF everywhere: the inserted text gets `\r\n`, and no line changes ending. (Test in Task 2 and Task 4.)
5. **A rename that changes only the case (`note.md` → `Note.md`) on a case-insensitive filesystem** (macOS, Windows). The destination "exists" because it is the same file. The rename must succeed and must not delete the note. (Test in Task 5.)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/vault/atomic.go`, `atomic_other.go`, `atomic_windows.go` (modify) | Root-relative `writeTemp`, `writeFileAtomic`, `createExclusive`, `renameReplace`, `removeFile`. |
| `internal/vault/root.go` (new) | `openRoot`, `rootRel`: open the vault root once per write and turn a validated absolute path into a root name. |
| `internal/vault/io.go`, `version.go` (modify) | Writers use the root helpers. `preWriteHook` for TOCTOU tests. |
| `internal/markdown/sections.go` (new) | `Heading`, `Headings`, `FindSection`, `ReplaceSection`, `AppendToSection`, `PrependToSection`, section errors. |
| `internal/vault/section.go` (new) | `Vault.EditSection`. |
| `internal/links/rewrite.go` (new) | `LinkStyle`, `StyleOf`, `TargetFor`, `RewriteRaw`, `Edit`, `ApplyEdits`. |
| `internal/index/index.go` (modify) | `RefSets` snapshot: all note paths, attachment paths, distinct refs per note. |
| `internal/vault/move.go` (new) | `MoveNote`, `planMove`, `MoveResult`, `LinkChange`. |
| `internal/tools/write_tools.go` (modify) | `edit_section`, `move_note`. |
| `internal/tools/sweep_test.go`, `race_test.go` (modify) | 22 tools; `move` race ops. |
| `README.md` (modify) | The two new tools, section rules, move rules, os.Root in Security. |

---

### Task 1: Every write through os.Root (SR-3)

**Files:**
- Create: `internal/vault/root.go`, `internal/vault/root_test.go`
- Modify: `internal/vault/atomic.go`, `atomic_other.go`, `atomic_windows.go`, `io.go` (`writeNoteLocked`), `version.go` (`CreateNote`, `DeleteNote`)

**Interfaces:**
- Produces:
```go
func (v *Vault) openRoot() (*os.Root, error)                 // os.OpenRoot(v.root); caller closes
func (v *Vault) rootRel(safeAbs string) (string, error)      // lexical rel under v.root, OS separators; errOutside if not inside
var preWriteHook func(rel string)                            // test hook: after validation, before the first Root call
func writeTemp(r *os.Root, targetRel string, data []byte, mode fs.FileMode) (tmpRel string, err error)
func writeFileAtomic(r *os.Root, rel string, data []byte) error
func createExclusive(r *os.Root, rel string, data []byte) error
func renameReplace(r *os.Root, oldRel, newRel string) error  // windows: retryShared(r.Rename)
func removeFile(r *os.Root, rel string) error                // windows: retryShared(r.Remove)
```
- `writeTemp` creates `<dir>/.obsidian-mcp-<16 hex from crypto/rand>.tmp` with `r.OpenFile(name, O_WRONLY|O_CREATE|O_EXCL, 0o600)`. On EEXIST it retries at most 3 times. Then it writes, fsyncs, closes and chmods through `r.Chmod`. `os.CreateTemp` has no Root form.
- `createExclusive` keeps its two steps: first `r.Link` (no-clobber), then the `r.OpenFile(O_EXCL)` fallback. `MkdirAll` becomes `r.MkdirAll(dir, 0o755)`.
- Callers keep the `ValidatePath` → `requireMarkdownTarget` order. After validation they call `rootRel(safeAbs)` and do every FS call on the root.
- *(planner's call)* One `openRoot` per write operation, the same as reads today. A move opens it once for the whole operation.

- [ ] **Step 1: Write the failing tests** in `internal/vault/root_test.go`
  - `TestWriteRefusesSymlinkSwappedAfterValidation` (skip on Windows when symlinks need privileges).
    - Seed `sub/n.md`, plus a directory `outside` in another `t.TempDir()`.
    - Set `preWriteHook` to replace `sub` with a symlink to `outside`.
    - Call `AppendToNote("sub/n.md", "x", "")`.
    - Assert an error, assert `outside` is still empty, and assert the error does not contain `outside` or the vault root.
  - `TestCreateRefusesSymlinkSwappedAfterValidation`: the same setup for `CreateNote("sub/new.md", …)`.
  - `TestDeleteRefusesSymlinkSwappedAfterValidation`: the same for `DeleteNote`. Seed `outside/n.md`. It must still exist afterwards.
  - `TestTempFileNameIsHiddenAndCleaned`: after a write that fails (stub `renameReplace` to fail via a test var), no `.obsidian-mcp-*.tmp` is left in the directory.
- [ ] **Step 2:** `go test ./internal/vault -run 'Swapped|TempFile' -v`. Expected: the three swap tests FAIL, because data lands in or is deleted from `outside`.
- [ ] **Step 3:** Implement the interfaces above, and switch `writeNoteLocked`, `CreateNote` and `DeleteNote` to them. `NoteExists`, `AddressNote` and `onDiskName` are reads and stay as they are.
- [ ] **Step 4:** `go test -race ./...` passes. `GOOS=windows go vet ./...` is clean.
- [ ] **Step 5:** Commit: `fix(vault): do every write through os.Root (SR-3)`

---

### Task 2: Heading and section parser

**Files:**
- Create: `internal/markdown/sections.go`, `internal/markdown/sections_test.go`

**Interfaces:**
- Produces:
```go
type Heading struct {
    Level     int    // 1-6
    Text      string // trimmed, closing #s removed, setext underline excluded
    Path      string // ancestor texts joined by " > ", e.g. "Plan > Risks"
    Start     int    // byte offset of the heading line
    BodyStart int    // byte offset just after the heading line(s), including its line ending
    End       int    // start of the next heading with Level <= this one, or len(content)
}
func Headings(content string) []Heading
func FindSection(content, query string) (Heading, error)
type SectionNotFoundError struct{ Query string; Headings []string } // up to 10 Paths
type AmbiguousSectionError struct{ Query string; Matches []string }  // Paths
func ReplaceSection(content string, h Heading, body string) string
func AppendToSection(content string, h Heading, text string) string
func PrependToSection(content string, h Heading, text string) string
```
- Parsing:
  - ATX headings: `#{1,6}` followed by a space or the end of the line, with at most 3 leading spaces.
  - Setext headings: a non-blank paragraph line followed by `===` or `---`. A `---` line directly after a blank line, or the frontmatter fence, is not a heading.
  - The parser scans `MaskCode(content)` from the end of the frontmatter, so headings in code and in frontmatter are skipped.
- **[Decided Q2] Matching.** `query` matches `Text` or `Path`. The comparison uses `links.Key` (case-insensitive, NFC) after trimming spaces and leading `#`s from the query. If more than one heading matches, the result is `AmbiguousSectionError`, listing their `Path`s. An agent passes `"Plan > Risks"` to pick one.
- **[Decided Q3] Extent.** A section runs from `BodyStart` to `End`, so it includes its subsections.
  - Replace swaps `[BodyStart, End)`.
  - Append inserts at `End`, after the subsections.
  - Prepend inserts at `BodyStart`.
- Newlines *(planner's call)*:
  - Inserted text gets the line ending of the heading line (`\r\n` or `\n`).
  - If the text doesn't end with a newline, one is added.
  - When the section's last line has no newline (end of file), one newline is added before the appended text.
  - Replace keeps exactly one blank line before the next heading only when the original had one.

- [ ] **Step 1: Write the failing table tests** `TestHeadings`, `TestFindSection`, `TestSectionEdits`. Cases:
  - ATX with closing `##`
  - setext `===` and `---`
  - a frontmatter `---` that is not a heading
  - `# x` inside a fenced block and inside inline code is ignored
  - `#tag` at the start of a line is not a heading (no space)
  - a duplicate `## Notes` under two parents gives `AmbiguousSectionError{Matches: ["A > Notes", "B > Notes"]}`, and `"B > Notes"` resolves
  - a missing heading gives `SectionNotFoundError` listing the Paths
  - append to the last section in a file with no final newline
  - a CRLF file keeps CRLF on every line, inserted lines included
  - a BOM-prefixed file keeps the BOM
  - for every edit: `content[:h.Start]` and the bytes after the edited range are byte-identical to the input
- [ ] **Step 2:** `go test ./internal/markdown -run 'Heading|Section' -v`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** The tests pass. `go vet ./...` is clean.
- [ ] **Step 5:** Commit: `feat(markdown): heading and section parser with byte-exact edits`

---

### Task 3: `edit_section` tool

**Files:**
- Create: `internal/vault/section.go`, `internal/vault/section_test.go`
- Modify: `internal/tools/write_tools.go`, `internal/tools/write_tools_test.go`, `internal/tools/sweep_test.go` (adds `edit_section` to `sweepTools`)

**Interfaces:**
- Consumes: `modifyNote`, `addressForWrite`, `NoteSnapshot` (phase 2); Task 2's functions.
- Produces:
```go
type SectionMode string // "replace" | "append" | "prepend"
var ErrSectionVersionRequired = errors.New(`expected_version is required to replace a section; read it with get_note and pass its "version"`)
func (v *Vault) EditSection(arg, heading string, mode SectionMode, content, expected string) (NoteSnapshot, error)
```
- **[Decided Q1] Tool surface.** There is one tool, `edit_section`.
  - Arguments: `path` (required, `notePathDesc`), `heading` (required), `mode` (required, enum `replace|append|prepend`), `content` (required) and `expected_version` (optional; required when `mode=replace`).
  - It returns `writeResult(snap)`.
  - Errors are `failed to edit section: <err>`.
  - The description says that the section includes its subsections, that `"A > B"` disambiguates, and that `replace` needs the version.
- Checks run before the guard is taken: the mode is valid, and `replace` without `expected` gives `ErrSectionVersionRequired`. The heading lookup runs inside the `edit` closure, on the content re-read under the guard.

- [ ] **Step 1: Write the failing tests.**
  - `TestEditSectionReplaceNeedsVersion`.
  - `TestEditSectionStaleVersionConflict`: the error has the current version, and the file is unchanged.
  - `TestEditSectionAppendWithoutVersion`.
  - `TestEditSectionOtherBytesUnchanged`: frontmatter, the other sections and the trailing text are compared byte for byte.
  - `TestEditSectionAmbiguousListsPaths`.
  - Tool test `TestEditSectionTool`: the three modes through `e.call("edit_section", …)`, plus an invalid mode.
  - The sweep includes `edit_section`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test -race ./...` passes, the sweep included.
- [ ] **Step 5:** Commit: `feat(tools): edit_section replaces, appends or prepends under a heading`

---

### Task 4: Link rewrite engine

**Files:**
- Create: `internal/links/rewrite.go`, `internal/links/rewrite_test.go`

**Interfaces:**
- Consumes: `Ref`, `Resolver.Resolve`, `NormalizeTarget`, `Key` (phase 1 and #8).
- Produces:
```go
type LinkStyle int // StyleName (no "/"), StylePath (has "/", not rooted), StyleRooted (leading "/"), StyleRelative ("./" or "../")
func StyleOf(ref Ref) LinkStyle
// TargetFor returns the target text (no fragment, no alias) to write in `from` so that it resolves to note `to` under r.
func TargetFor(r *Resolver, from, to string, style LinkStyle, keepExt bool) string
// RewriteRaw returns ref.Raw with only its target part replaced by newTarget.
func RewriteRaw(ref Ref, newTarget string) string
type Edit struct{ Offset int; Old, New string }
func ApplyEdits(content string, edits []Edit) (string, error) // sorted, non-overlapping, content[Offset:Offset+len(Old)] == Old, else error
```
- `TargetFor` rules (spec §7.3 link-style rule):
  - **Name style:** use the basename when it resolves (Status `Resolved` or `Ambiguous`, with `Path == to`). Otherwise use the shortest whole-segment suffix that does. Otherwise use the full vault path.
  - **Path style:** use the full vault path without a leading `/`.
  - **Rooted style:** use `/` plus the full path.
  - **Relative style:** use a path relative to `from`'s folder, starting with `./` or `../`.
  - The result ends in `.md` only when `keepExt` is set. Callers pass `keepExt` when the original target ended in `.md` (case-insensitive).
- `RewriteRaw` re-parses `Raw`. `Ref` has no sub-spans, so this is the only way to keep the bytes around the target.
  - **Wikilink and embed:** the target part runs from after `[[` (and the `!` before it) up to the first `#`, `|` or `\|`. Everything after it is kept byte for byte.
  - **Markdown:** only the path part of the destination is replaced, which is the text before `#` inside `(…)` or `<…>`.
    - Inside `<…>` the new path is written raw.
    - Otherwise these characters are percent-encoded: `%`→`%25`, space→`%20`, `(`→`%28`, `)`→`%29`, `<`→`%3C`, `>`→`%3E`, `#`→`%23`. Unicode is kept as written, which is what Obsidian does.

- [ ] **Step 1: Write the failing tests.**
  - `TestRewriteRawKeepsEverythingButTarget`: a table over `[[Old]]`, `[[Old#H|a]]`, `![[Old#^b]]`, `[[Old\|a]]`, `[[ Old ]]` (the inner spaces may go), `[x](Old.md)`, `[x](<sub/Old Name.md>)`, `[x](sub/Old%20Name.md#h)`, `![alt](Old.md)` and `"[[Old]]"` from frontmatter. Each case checks the exact output, and that `Extract(out)[0]` has the same Kind, Alias, Heading and Block.
  - `TestTargetForStyles`: on a resolver with `a/Dup.md`, `b/Dup.md`, `b/c/Dup.md`, `Note.md` and `md/Peer.md`, check each style, including a name that becomes ambiguous (it falls back to the shortest unique suffix, e.g. `c/Dup`) and a relative `../`.
  - `TestApplyEditsVerifiesOld`: a mismatched `Old` gives an error, overlapping edits give an error, and bytes outside the edits are identical.
  - A CRLF content case for `ApplyEdits`.
- [ ] **Step 2:** `go test ./internal/links -run 'Rewrite|TargetFor|ApplyEdits' -v`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** The tests pass. Fuzz `FuzzRewriteRaw` for 30 s locally: for any extracted `Ref` and any target made of `[A-Za-z0-9 /._-]`, `Extract(RewriteRaw(ref, t))` yields exactly one ref with Target `t`.
- [ ] **Step 5:** Commit: `feat(links): rewrite a link's target and keep its alias, fragment and form`

---

### Task 5: `Vault.MoveNote`

**Files:**
- Create: `internal/vault/move.go`, `internal/vault/move_test.go`
- Modify: `internal/index/index.go` (+ test)

**Interfaces:**
- Consumes: Task 1 root helpers, Task 4 engine, `addressForWrite`, `relArg`, `NormalizeNotePath`, `resolveNote`, `ConflictError`, `RenameFailure` (tags.go), `guard.lock`.
- Produces:
```go
// index
func (ix *Index) RefSets() (notes, attachments []string, refs map[string][]links.Ref) // copies; distinct refs per note as parsed
// vault
type LinkEdit struct{ Old string `json:"old"`; New string `json:"new"` }
type LinkChange struct{ Path string `json:"path"`; Links []LinkEdit `json:"links"` }
type MoveResult struct {
    From, To string; Version string; DryRun bool
    Updated  []LinkChange   `json:"updated"`
    Failed   []RenameFailure `json:"failed"`
    Warnings []string       `json:"warnings"`
}
var ErrDestinationExists = errors.New("destination already exists")
func (v *Vault) MoveNote(ctx context.Context, arg, dest, expected string, dryRun bool) (MoveResult, error)
```
- Move algorithm. Steps 1–2 run before the guard; steps 3–8 run under one guard hold **[Decided Q5]**:
  1. Resolve the source: `src := addressForWrite(arg)`.
  2. Resolve the destination: `relArg` → `NormalizeNotePath` → `resolveNote`. A protected or non-`.md` destination is refused. A destination equal to `src` returns an error.
  3. Re-read `src` from disk. If `expected` is set and doesn't match, return `ConflictError`. Then `ix.Refresh()`, and take `RefSets()`.
  4. Check the destination: `r.Lstat(dest)` must be not-exist. The one exception is **[Decided Q6]** case-only renames: when `os.SameFile` matches the source, the move continues.
  5. Build `before := NewResolver(notes, attachments)` and `after` from the same lists with `src` replaced by `dest`.
  6. **[Decided Q4] Candidates.** For every note `n` (using its after-move name) and every ref in `refs[n]`, compare the resolutions:
     - The intended target is `before.Resolve(n_old, ref)`, with `src` mapped to `dest`.
     - The actual target is `after.Resolve(n_new, ref)`.
     - If the paths differ, `n` is a candidate.
     - A ghost that would now resolve is not rewritten. It is reported in `Warnings` as `"<n>: [[raw]] now resolves to <dest>"`.
  7. For each candidate:
     - Read it from disk. The source note is read as its own content.
     - Run `links.Extract` on all occurrences, skipping refs in code; `Extract` already masks code.
     - Build edits with `TargetFor` + `RewriteRaw` for each ref whose resolution changes.
     - Apply them with `ApplyEdits`.
     - **Verify:** re-extract the result and check that every ref resolves under `after` to its intended target. If it doesn't, the candidate goes to `Failed` with the reason `rewrite would change link meaning`.
  8. If `dryRun` is set, return the plan without writing.
     Otherwise:
     - Write the destination with `createExclusive`, using the rewritten source content. For a case-only rename, use `renameReplace` and then `writeFileAtomic`.
     - Remove the source.
     - If removing the source fails, remove the destination and return an error.
     - Then write each linker with `writeFileAtomic`. A failure goes to `Failed`, and the move is not rolled back **[Decided Q5]**.
     - `ctx.Err()` is checked before each linker. Once it is set, the remaining linkers go to `Failed` with the reason `canceled`.
- After the guard is released, call `indexRemove(src)`, `indexUpdate(dest)` and `indexUpdate` for each linker.
- *(planner's call)* A dry run takes no guard and makes no writes. Its result can be stale by the time a real move runs, and the description says so.
- `expected_version` is optional for a move **[Decided Q7]**.

- [ ] **Step 1: Write the failing tests.**
  - **Exit test `TestMoveFixtureRoundTrip`.** For every note in a fresh `copyFixture` (a loop, each in its own copy):
    - Record the index edges.
    - Run `MoveNote(note, "zz/moved/<base>-m.md")`.
    - Rebuild the index.
    - Assert the edge set equals the old one with the note's path mapped, `Ambiguous` flags aside.
    - For every changed file, assert that the old and new bytes differ only inside the reported `LinkEdit` spans. The test re-derives this by applying the edits.
  - `TestMoveRewritesAllForms`: Review Focus 1 as one linker note.
  - `TestMoveKeepsOtherLinksStable`: Review Focus 2 (tie-break flip, outgoing `./` links of the moved note, ghost warning).
  - `TestMoveRefusesExistingDestination`: nothing changes.
  - `TestMoveCaseOnly`: Review Focus 5. It runs on a case-insensitive FS and is skipped otherwise.
  - `TestMoveDryRunWritesNothing`: the bytes and mtimes of all files are unchanged.
  - `TestMoveStaleVersion`.
  - `TestMoveLinkerFailureReported`: one linker is made read-only, or the write is stubbed to fail for that path. It appears in `Failed`, and the note is still moved.
  - `TestMoveSwappedDestDirRefused`: `preWriteHook` swaps the destination directory for a symlink pointing outside the vault, as in Task 1.
- [ ] **Step 2:** `go test ./internal/vault -run Move -v`. Expected: FAIL.
- [ ] **Step 3:** Implement `RefSets`, then `MoveNote`.
- [ ] **Step 4:** `go test -race ./...` passes. Benchmark `BenchmarkMovePlan5k` (5k notes, one move with 50 linkers), with the result recorded in the commit message. Budget: under 1 s.
- [ ] **Step 5:** Commit: `feat(vault): move a note and rewrite every link the move would change`

---

### Task 6: `move_note` tool, race exit test, README

**Files:**
- Modify: `internal/tools/write_tools.go`, `write_tools_test.go`, `sweep_test.go` (22 tools), `race_test.go`, `cmd/obsidian-mcp/main.go` (the "20 tools" comment), `README.md`

**Interfaces:**
- Consumes: `MoveNote`, `MoveResult`.
- Produces the tool `move_note`:
  - Arguments: `path` (required), `new_path` (required, a vault-relative path; `.md` optional; missing folders are created), `expected_version` (optional) and `dry_run` (optional bool, default false).
  - The result is `MoveResult` as JSON, plus `"success": true`. On an error the result is `failed to move: <err>`.
  - The description lists:
    - single `.md` notes only
    - every link form that is rewritten
    - that a dry run takes no lock
    - that failures are per file
    - that the move refuses an existing destination
- Race ops (new `case` branches in `TestRaceHelper`):
  - `move`: move `src.md` to `dst-<id>.md`.
  - `moveAppend`: append a line to `src.md` by name.

- [ ] **Step 1: Write the failing tests.**
  - `TestMoveNoteTool`: a dry run returns `updated`, and the files are unchanged. Then a real move returns `version`, the old path is gone, and the linker is rewritten.
  - The sweep includes `move_note`, with hostile `new_path` values (absolute, `../x`, `.obsidian/x`, `C:\x`, `/C:/x`, `OBSIDI~1/x`). None of them may leak the root.
  - New `TestTwoProcessRace` subtests:
    - `moveSame`: two processes move the same note to different destinations. Exactly one reports ok, exactly one of the destinations exists, and the source is gone.
    - `moveVsAppend`: one process moves while the other appends 100 lines by note name. Every append that reported ok is present in the destination file, and no line is lost.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement the tool and the race ops. Update the README:
  - the tool count (22)
  - a "Section edits" subsection and a "Moving and renaming notes" subsection
  - the `expected_version` table rows for `edit_section` and `move_note`
  - the Security section: every write goes through `os.Root`
  - the architecture tree
- [ ] **Step 4:** `go test -race ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `gofmt -l .` (empty) and `go mod tidy -diff` (empty).
- [ ] **Step 5:** Commit: `feat(tools): move_note with dry run, per-file failures and race test`

---

## Phase exit

- The roadmap exit criterion is met by `TestMoveFixtureRoundTrip`. It is recorded in the PR together with the `moveSame` and `moveVsAppend` race results.
- An independent security review runs before the PR (roadmap rule). Its focus:
  - the os.Root conversion
  - move destination validation
  - `RewriteRaw` with hostile link text
  - lock hold time
  - temp-file naming
  - the dry-run information leak (it lists vault-relative paths only)
- Still deferred, unchanged: SR-6, SR-14, backslash names, `#fragment` stripping in addressing, and output-format unification (phase 6).

## Decided questions (maintainer approved the defaults on 2026-11-02)

| Q | Decided | Rejected alternative |
|---|---|---|
| Q1 | One `edit_section` tool with a `mode` argument, plus one `move_note` tool (a rename is a move within the same folder). | Separate tools: `replace_section`, `append_to_section`, `prepend_to_section`, `move_note`, `rename_note`. |
| Q2 | The heading is matched by its text, ignoring case. When it is duplicated, the error lists the paths, and `"Parent > Child"` picks one. | Exact text only; or the first match wins. |
| Q3 | A section includes its subsections. Append goes at the end of the whole section, and prepend goes right after the heading. | The section ends at the next heading of any level. |
| Q4 | The whole graph is kept the same: inbound links, the moved note's own relative links, and links whose tie-break changes are all rewritten. A ghost that starts resolving is reported, not rewritten. | Only rewrite inbound links to the moved note, as the spec states at minimum. |
| Q5 | One lock for the whole move. The note moves first. A linker that fails to update is reported, with no rollback. | All-or-nothing with rollback (more complex; a failure can leave a half-restored state). |
| Q6 | A case-only rename (`note.md` → `Note.md`) is allowed. | Refused as "destination exists". |
| Q7 | `expected_version` is optional on `move_note`, because a move doesn't change the note's content. | Required. |
