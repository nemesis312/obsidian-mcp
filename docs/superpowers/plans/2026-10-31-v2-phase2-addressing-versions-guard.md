# v2 Phase 2: Addressing, Version Tokens, Cross-Process Write Guard (Implementation Plan)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every tool that takes an existing note accepts a path or a bare note name. No tool output contains an absolute path. Reads return a content-hash version token. Whole-note replace and delete require that token. All writes are serialized across server processes by an OS file lock.

**Architecture:**
- `internal/vault/guard*.go` adds a vault-wide write guard. It is an in-process mutex plus an OS advisory lock: `flock` on unix, `LockFileEx` on Windows. The lock is taken on `.obsidian-mcp/write.lock` inside the vault. The OS releases it when the holding process dies, so a stale lock cannot exist and no recovery code is needed.
- `internal/vault/address.go` turns a tool argument into a vault-relative note path. An exact existing path is the fast path. Otherwise the argument is resolved as a `[[wikilink]]` from the vault root, through the phase 1 resolver.
- `internal/vault/version.go` defines the token. Every modifying `Vault` method re-reads the note from disk while holding the guard, checks the token and writes atomically.
- The tools gain `expected_version` and report `version`. Tool names do not change.

**Tech Stack:** Go 1.26.1, `mark3labs/mcp-go` v0.48.0, `golang.org/x/text`, `gopkg.in/yaml.v3`. One new pure-Go dependency: `golang.org/x/sys`, used only for `windows.LockFileEx`. Unix uses stdlib `syscall.Flock`.

**Spec:** `docs/superpowers/specs/2026-10-30-memory-graph-design.md` §6 (concurrency, write scope) and §7.3 (addressing, optimistic concurrency). Roadmap: `docs/superpowers/plans/2026-10-30-v2-roadmap.md`, phase 2. Style reference: `docs/superpowers/plans/2026-10-30-v2-phase1-link-resolver-index.md`.

Markers: `**[Decided Qn]**` is a choice the spec left open; the maintainer approved the default written inline on 2026-10-31. `*(planner's call)*` is a minor choice settled here.

## Global Constraints

- `CGO_ENABLED=0`, a single static binary, no runtime services. Pure-Go modules are allowed (§6: "A pure-Go dependency such as `golang.org/x/sys` is acceptable").
- The cross-process mechanism "must work without CGO on all three OSes" (§6). CI runs on ubuntu, macos and windows: tidy check, vet, gofmt and `go test -race` (no `-race` on Windows).
- "Lock files are never listed as notes and never count as vault content" (§6).
- "Errors never include absolute filesystem paths" (§7.3). This applies to every tool result, error or not.
- The token is "a content hash" (§7.3). It is "**required** for whole-note replace, section replace and delete". "Additive operations (create, append, section append/prepend) may omit it. They re-read the file under the write guard and apply only their addition."
- Conflict errors include the current version token (§7.3).
- "Each process refreshes its view of a file before it writes to it" (§6). Under the guard, writers read from disk, never from the index.
- Every v1.x security property stays: containment is checked before any FS call, `.md`-only symlink targets for writes, atomic writes, and `.git`/`.obsidian` are blocked. An address resolved by name still goes through `resolveNote` (`ValidatePath` + `requireMarkdownTarget`) before any write. Every existing test stays green unless a task explicitly updates it.
- Paths in output use `/` and are vault-relative.
- Tool names stay the same in this phase. Renames and the migration table belong to phase 6.
- Tool descriptions are in English.

## Review Focus

1. **Windows, a reader holds the note open while another process writes it.** Go opens files without `FILE_SHARE_DELETE`, so `os.Rename` over a note that another server's index is reading fails with access denied. A reasonable person expects the write to succeed once the reader closes it, not an error. (Test in Task 3.)
2. **A server is killed (SIGKILL, closed terminal) while holding the lock.** The next process must get the lock at once, with no manual cleanup. (Test in Task 1.)
3. **`.obsidian-mcp` in the vault is a symlink pointing outside, or is a regular file.** Writes must fail with a vault-relative error, and nothing may be created outside the vault. (Test in Task 1.)
4. **A human edits the note in Obsidian between the agent's `get_note` and `patch_note`.** This includes edits that only change line endings or add a BOM. The replace must be refused with the current token, and the human's bytes must stay as they are. (Test in Task 3.)
5. **Name spellings.** `v1.2 notes` (a dot but no `.md`), an NFD or different-case name, a name that matches only an attachment (`img.png`), and a note in a hidden folder addressed by its exact path (`.trash/x.md`, which v1 can read today). Each one either resolves as Obsidian would or gets a clear error. None may crash or silently pick a note. (Test in Task 2.)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/vault/guard.go` (new) | `writeGuard`: in-process mutex + OS lock on `.obsidian-mcp/write.lock`, opened through `os.Root`; polling with timeout; `ErrBusy`. |
| `internal/vault/guard_unix.go`, `guard_windows.go`, `guard_other.go` (new) | `tryLockFile` / `unlockFile` per OS. |
| `internal/vault/address.go` (new) | `relArg`, `AddressNote`, `AmbiguousNoteError`, `suggest`. |
| `internal/vault/version.go` (new) | `VersionOf`, `ConflictError`, `ErrVersionRequired`. |
| `internal/vault/errors.go` (modify) | `notFoundError` gains suggestions. |
| `internal/vault/security.go` (modify) | `.obsidian-mcp` joins `protectedSegments`. |
| `internal/vault/io.go`, `tags.go`, `graph.go` (modify) | Guarded, versioned writes. Addressing in reads and graph lookups. |
| `internal/vault/atomic.go` + `atomic_windows.go`/`atomic_other.go` (new) | Bounded retry of rename and remove on Windows sharing violations. |
| `internal/index/index.go` (modify) | `Address`, `NotePaths`. |
| `internal/tools/*.go` (modify) | `expected_version`, `version`, addressing, descriptions. |
| `internal/tools/race_test.go` (new) | The two-process exit test. |
| `internal/tools/sweep_test.go` (new) | Every tool, every hostile argument, no absolute path. |
| `README.md` (modify) | Addressing, versions, concurrency, lock file. |

---

### Task 1: Cross-process write guard

**Files:**
- Create: `internal/vault/guard.go`, `guard_unix.go` (`//go:build unix`), `guard_windows.go` (`//go:build windows`), `guard_other.go` (`//go:build !unix && !windows`)
- Create: `internal/vault/guard_test.go`
- Modify: `internal/vault/vault.go` (adds the `guard *writeGuard` field, set in `New`; `New` does no FS call for it)
- Modify: `internal/vault/security.go` (`protectedSegments = []string{".git", ".obsidian", ".obsidian-mcp"}`)
- Modify: `go.mod`, `go.sum` (`golang.org/x/sys`)

**Interfaces:**
- Produces:
```go
const lockDirName, lockFileName = ".obsidian-mcp", "write.lock" // [Decided Q8]
const defaultLockTimeout = 5 * time.Second
var ErrBusy = errors.New("vault is busy: another obsidian-mcp process is writing; retry")
type writeGuard struct{ /* root string; timeout time.Duration; mu sync.Mutex */ }
func newWriteGuard(root string, timeout time.Duration) *writeGuard
func (g *writeGuard) lock() (unlock func(), err error) // mu, then OS lock; ErrBusy after timeout
func tryLockFile(f *os.File) (acquired bool, err error) // non-blocking exclusive
func unlockFile(f *os.File) error
```
- Mechanism *(planner's call, within §6's "builder's choice")*. Use OS advisory locks rather than an O_EXCL lockfile, because the OS releases them when the process dies. That removes the PID/age heuristics that stale-lock recovery needs.
  - unix: `syscall.Flock(fd, LOCK_EX|LOCK_NB)`. `EWOULDBLOCK` means busy.
  - Windows: `windows.LockFileEx(h, LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)`. `ERROR_LOCK_VIOLATION` means busy.
  - Other platforms: return an error, so writes fail closed.
- `lock()` takes `mu` first, then opens the vault with `os.OpenRoot(root)`. It runs `Mkdir(lockDirName, 0o755)` (EEXIST is fine) and `OpenFile(lockDirName+"/"+lockFileName, O_RDWR|O_CREATE, 0o644)`. It polls `tryLockFile` with backoff from 5 ms to 50 ms until `timeout`. `unlock` releases the lock and closes the file.
- The lock file is never deleted: deleting it would let two processes lock different inodes. Readers never lock. A setup error is reported as `write lock .obsidian-mcp/write.lock: <cause>`, with no absolute path.
- Granularity *(planner's call)*: one lock per vault. Writes are milliseconds, and phases 3 and 5 need multi-file atomicity anyway.
- Known limit, to be documented: advisory locks over NFS/SMB are not reliable.

- [ ] **Step 1: Write the failing tests** in `guard_test.go`:
  - `TestGuardHelperProcess`: the helper body. It skips unless `OBSIDIAN_MCP_GUARD_HELPER=hold`. It opens the guard on `OBSIDIAN_MCP_GUARD_VAULT`, takes the lock, prints `locked\n`, then blocks reading stdin.
  - `TestGuardExcludesOtherProcess`: start the helper (`exec.Command(exe, "-test.run=^TestGuardHelperProcess$")`) and wait for `locked`. Then `newWriteGuard(root, 200*time.Millisecond).lock()` must return an error with `errors.Is(err, ErrBusy)` after ≥200 ms. Close the helper's stdin; the next `lock()` must succeed within 1 s.
  - `TestGuardReleasedWhenHolderKilled` (Review Focus 2): the same setup, then `cmd.Process.Kill()` and `cmd.Wait()`. `lock()` with a 1 s timeout succeeds.
  - `TestGuardInProcessSerializes`: 16 goroutines each lock and increment a shared non-atomic counter 100 times. Under `-race` the result is 1600.
  - `TestGuardLockDirSymlinkRefused` (Review Focus 3, skip if symlinks are unavailable): `.obsidian-mcp` is a symlink to `t.TempDir()` outside the vault. `lock()` errors, the error does not contain either root, and the outside dir stays empty. A second case makes `.obsidian-mcp` a regular file: `lock()` errors.
  - `TestLockFileNeverListed`: after `lock()`/`unlock()`, `ListFiles`, `StatNotes` and `StatAttachments` contain no path starting with `.obsidian-mcp`. `ValidatePath(".obsidian-mcp/x.md")` returns a `path_violation` error.
- [ ] **Step 2: Run** `go test ./internal/vault -run 'Guard|LockFile'`. Expected: FAIL, `undefined: newWriteGuard`.
- [ ] **Step 3: Implement** the files above. Run `go get golang.org/x/sys@latest && go mod tidy`.
- [ ] **Step 4: Run** `go test -race ./internal/vault && GOOS=windows go vet ./... && GOOS=linux go vet ./...`. Expected: PASS and vet clean.
- [ ] **Step 5: Commit** `feat(vault): cross-process write guard with OS file locks`

---

### Task 2: Note addressing by name

**Files:**
- Create: `internal/vault/address.go`, `internal/vault/address_test.go`
- Modify: `internal/vault/errors.go` (`notFoundError` gains `suggestions []string`)
- Modify: `internal/index/index.go` and `index_test.go` (`Address`, `NotePaths`)
- Modify: `internal/vault/graph.go` (`GetBacklinks`, `GetOutgoingLinks` and `GetLinkedMentions` address their argument through `AddressNote`; `notePathErr`/`lookupKey` are deleted; `NotePath` is kept for labels)
- Modify: `internal/vault/io.go` (`GetFrontmatter` addresses its argument. `ReadNote` keeps exact-path semantics, because `*Vault` is the `index.Source` (`internal/index/fixture_test.go:60`, `bench_test.go`) and the index must call it with canonical paths, never back into itself.)

**Interfaces:**
- Consumes: `links.Resolution`, `links.Ref{Kind: links.KindWikilink, Target: …}`, `links.Resolved/Ambiguous/Unresolved`, `links.Key`, `(*Vault).linkIndex()`, `(*index.Index).Refresh()`.
- Produces (index):
```go
// Address resolves target as a [[target]] written in a note at the vault root,
// without refreshing. An attachment-only match is returned as Unresolved.
func (ix *Index) Address(target string) links.Resolution
func (ix *Index) NotePaths() []string // sorted
```
- Produces (vault):
```go
var ErrAbsolutePath = errors.New("absolute paths are not accepted: pass a vault-relative path or a note name") // [Decided Q4]
// relArg applies the argument policy shared by every tool that takes a path:
// rejects an absolute path inside the vault root (either spelling, case-folded
// on darwin/windows), any Windows volume or UNC path, and NUL; strips leading
// "/" or "\" (= vault root); converts "\" to "/".
func (v *Vault) relArg(arg string) (string, error)
// AddressNote returns the vault-relative "/" path of the existing note arg names.
func (v *Vault) AddressNote(arg string) (string, error)
type AmbiguousNoteError struct{ Arg string; Candidates []string }
func (e *AmbiguousNoteError) Error() string
func suggest(query string, notes []string, max int) []string
```
- `AddressNote` order. **[Decided Q5]** Recommended: the exact path wins.
  1. `relArg`.
  2. **Exact fast path, no index:** run `NormalizeNotePath` and then `ValidatePath`. If `os.Lstat` shows a regular file or a symlink, return `noteRel`. This keeps v1 behavior for full paths and for hidden-folder notes, and costs no stat walk. A `path_violation: protected directory` error is returned as it is. Any other `ValidatePath` failure (outside, invalid) falls through, so it ends as `note not found` (phase 1: "Paths that escape the vault are Unresolved").
  3. **Name path:** `linkIndex()`, then `Refresh()`, then `Address(arg)`. `Resolved` returns the path. `Ambiguous` returns `*AmbiguousNoteError`; this includes case-only duplicates, and addressing never applies the tie-break. `Unresolved` returns `&notFoundError{arg, suggest(...)}`.
- Error text. **[Decided Q7]** Recommended: one plain-text line each.
  - `ambiguous note name "Dup": matches a/Dup.md, b/Dup.md; pass one of these paths`. At most 10 candidates, then `, … (N more)`.
  - `note not found: Projcts; did you mean: Projects.md, archive/Projects.md?`. The `; did you mean …?` part appears only when there are suggestions, so phase 1's exact `note not found: nope` and `note not found: ../x` stay true.
- `suggest` *(planner's call)*:
  - Compare `links.Key` of the basename without `.md`.
  - A note is a suggestion when its Levenshtein distance is ≤ `min(3, len/3)`, or, for queries of 3 or more runes, when it contains the query as a substring.
  - Sort by (distance, path) and return at most 5.
  - The query is truncated to 128 runes before the comparison, which bounds the cost.
  - An argument that escapes the vault (`..`) gets no suggestions.

- [ ] **Step 1: Write the failing tests.**
  - `index_test.go`, `TestAddress`: the fixture source from Task 4 of phase 1, plus `a/Dup.md`, `b/Dup.md`, `Notes/A.md`, `notes/a.md` and `assets/img.png`.
    - `Address("Dup")` is Ambiguous with Candidates `[a/Dup.md b/Dup.md]`.
    - `Address("notes/a")` is Ambiguous, because case-only duplicates are ambiguous.
    - `Address("img.png")` is Unresolved.
    - `Address("../x")` is Unresolved.
    - `Address("/a/Dup")` is Resolved to `a/Dup.md`.
  - `TestNotePaths` checks that the result is sorted.
  - `address_test.go`, `TestAddressNote` on a real temp vault with `sub/Deep Note.md`, `v1.2 notes.md`, `café.md` written NFC, `a/Dup.md`, `b/Dup.md`, `.trash/Old.md` and `img.png`. Each row is input → want path or exact error text:
    - `"sub/Deep Note.md"`, `"sub/Deep Note"`, `"Deep Note"`, `"deep note"`, `"/sub/Deep Note"` and `"sub\\Deep Note"` all → `sub/Deep Note.md`
    - `"v1.2 notes"` → `v1.2 notes.md`; `"cafe\u0301"` (NFD) → `café.md` (Review Focus 5)
    - `".trash/Old.md"` → `.trash/Old.md` (exact fast path)
    - `"Dup"` → `ambiguous note name "Dup": matches a/Dup.md, b/Dup.md; pass one of these paths`
    - `"Deep Not"` → `note not found: Deep Not; did you mean: sub/Deep Note.md?`
    - `"img.png"` → `note not found: img.png` (the `.png` extension is not a note)
    - `"../x"` → `note not found: ../x`
    - `".obsidian/x.md"` → an error containing `path_violation`
    - `filepath.Join(v.Root(), "v1.2 notes.md")` → `errors.Is(err, ErrAbsolutePath)`
    - on Windows, `C:\x.md` and `\\srv\share\x.md` → `ErrAbsolutePath`
    - no error text contains `v.Root()`
  - `TestSuggestBounds`: a 10 000-rune query against 5 000 paths returns in under 1 s; `suggest("x", ["a.md"], 5)` is empty.
  - `graph_test.go`, `TestGraphByName`: `GetBacklinks("Deep Note")` equals `GetBacklinks("sub/Deep Note.md")`.
- [ ] **Step 2: Run** `go test ./internal/index ./internal/vault -run 'Address|NotePaths|Suggest|GraphByName'`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** the interfaces above. Tool-facing reads address through `AddressNote` and then call `ReadNote(rel)`. `ReadNote` itself stays unchanged: exact path, `os.OpenRoot`, regular-file check.
- [ ] **Step 4: Run** `go test -race ./...`. Expected: PASS, including phase 1's `TestGraphToolsNotFound` and `TestVaultGraphErrorsNoAbsoluteRoot` unchanged.
- [ ] **Step 5: Commit** `feat(vault): address notes by name with ambiguity and did-you-mean errors`

---

### Task 3: Version tokens and guarded writes

**Files:**
- Create: `internal/vault/version.go`, `internal/vault/version_test.go`
- Create: `internal/vault/atomic_windows.go`, `internal/vault/atomic_other.go` (`renameReplace`, `removeFile`)
- Modify: `internal/vault/io.go`, `internal/vault/tags.go`, `internal/vault/atomic.go`
- Modify: `internal/vault/io_test.go`, `tags_test.go`, `graph_test.go` (callers of the changed signatures)

**Interfaces:**
- Consumes: `(*writeGuard).lock`, `ErrBusy` (Task 1); `AddressNote`, `relArg` (Task 2).
- Produces:
```go
// VersionOf: "sha256:" + first 16 lowercase hex chars of SHA-256 over the raw file bytes. [Decided Q1]
func VersionOf(content []byte) string
var ErrVersionRequired = errors.New(`expected_version is required to replace or delete a whole note; read it with get_note and pass its "version"`)
type ConflictError struct{ Path, Current string } // Path vault-relative "/"
// Error(): "version conflict: <Path> changed since expected_version; current version: <Current>; re-read the note and retry"
func (e *ConflictError) Error() string

type NoteSnapshot struct{ Path, Content, Version string }
func (v *Vault) ReadNoteVersioned(arg string) (NoteSnapshot, error)
func (v *Vault) CreateNote(path, content string, frontmatter map[string]any) (NoteSnapshot, error)          // no token; exclusivity is its guard
func (v *Vault) AppendToNote(arg, content, expected string) (NoteSnapshot, error)                         // expected optional (§7.3)
func (v *Vault) ReplaceNote(arg, content, expected string) (NoteSnapshot, error)                          // expected required; note must exist
func (v *Vault) UpdateFrontmatter(arg string, updates map[string]any, expected string) (NoteSnapshot, error) // expected optional [Decided Q6]
func (v *Vault) DeleteNote(arg, expected string) (string, error)                                           // expected required; returns path
```
  The returned `NoteSnapshot.Content` is the full new content. Tools do not return it for writes.
- `WriteNote` becomes the unexported `writeNoteLocked(rel, content string) error`. It assumes the guard is held and keeps `resolveNote`, `MkdirAll`, the atomic write and `indexUpdate`. `RenameTag` takes the guard **per file**, re-reads, rewrites and writes. A busy guard on one file becomes a `RenameFailure` for that file *(planner's call: no token, it is a bulk tag edit)*.
- Every modifying method follows this order:
  1. `AddressNote`, or `relArg` + `NormalizeNotePath` for create.
  2. Check that a required token is present and well-formed. A malformed token gives `invalid expected_version: want sha256:<16 hex>`.
  3. `guard.lock()`.
  4. Read from disk via `ReadNote(rel)`, never from the index.
  5. Compare `VersionOf` with `expected`.
  6. Write.
  7. Unlock, then `indexUpdate`/`indexRemove`.
- A note that vanishes between addressing and the locked read gives `note not found: <arg>`.
- `ReplaceNote` on a missing note is `note not found` and does not create the note *(planner's call; see Decided Q9)*.
- Windows *(planner's call, Review Focus 1)*: `renameReplace` and `removeFile` retry on `ERROR_ACCESS_DENIED` / `ERROR_SHARING_VIOLATION` with 10→100 ms backoff for at most 2 s. On other OSes they are `os.Rename` / `os.Remove`.

- [ ] **Step 1: Write the failing tests** in `version_test.go`:
  - `TestVersionOf`: matches `^sha256:[0-9a-f]{16}$`, is deterministic, and differs for `"a\n"` vs `"a\r\n"` and for a leading BOM.
  - `TestReadNoteVersioned`: `Version == VersionOf(os.ReadFile bytes)`, and `Path` is canonical for a by-name argument.
  - `TestReplaceNote`:
    - `""` → `errors.Is(err, ErrVersionRequired)`, file unchanged
    - `"bogus"` → `invalid expected_version`
    - a stale token → `*ConflictError` with `Current == VersionOf(current bytes)`, file unchanged, and `err.Error()` contains the relative path and does not contain `v.Root()`
    - the correct token → content replaced and the returned `Version == VersionOf(new)`
    - a missing note → `note not found: <arg>`
  - `TestReplaceAfterExternalEdit` (Review Focus 4): read, then `os.WriteFile` CRLF-only changes behind the vault's back → `ReplaceNote(old token)` gives a `*ConflictError` and the human's bytes are intact.
  - `TestDeleteNote`: needs a token; a stale token → conflict and the file still exists; the right token → gone and removed from the index.
  - `TestAppendVersionOptional`: no token → ok; a stale token → conflict; the right token → ok. The result `Version` equals `VersionOf` of the file.
  - `TestUpdateFrontmatterVersionOptional`: the same three cases.
  - `TestCreateNoteReturnsVersion`.
  - `TestWritesWaitForGuard`: hold `v.guard.lock()` in the test, with `v.guard` built with a 100 ms timeout. Each of `CreateNote`, `AppendToNote`, `ReplaceNote`, `UpdateFrontmatter` and `DeleteNote` returns `errors.Is(err, ErrBusy)` and the files are unchanged. `RenameTag` reports `vault is busy` for each file in `Failed`.
  - `TestConcurrentAppendsInProcess`: 2 goroutines × 100 appends → all 200 lines present exactly once (under `-race`).
  - `TestReplaceWhileReaderOpenWindows` (Windows only, Review Focus 1): hold `os.Open(note)`, call `ReplaceNote` in a goroutine, close the reader after 100 ms → `ReplaceNote` succeeds.
- [ ] **Step 2: Run** `go test ./internal/vault -run 'Version|Replace|Delete|Append|UpdateFrontmatter|CreateNoteReturns|Guard|Concurrent'`. Expected: FAIL (undefined, or signature mismatches).
- [ ] **Step 3: Implement** the interfaces above and update the internal callers. The tools package will not compile until Task 4. Keep the tools building in this commit by passing `""` for `expected` and taking `.Path` from the snapshot. Task 4 wires the real arguments.
- [ ] **Step 4: Run** `go test -race ./... && GOOS=windows go vet ./...`. Expected: PASS. The existing tools tests `TestPatchNote` and `TestDeleteNote` now fail on "expected_version is required". Task 4 updates them, so mark them `t.Skip("updated in Task 4")` in this commit, and only these two.
- [ ] **Step 5: Commit** `feat(vault): content-hash version tokens and guarded, re-read-before-write edits`

---

### Task 4: Tools: addressing, versions, `expected_version`

**Files:**
- Modify: `internal/tools/read_tools.go`, `write_tools.go`, `graph_tools.go`
- Modify: `internal/tools/write_tools_test.go` (remove the Task 3 skips; existing calls pass versions), `graph_tools_test.go`
- Create: `internal/tools/addressing_test.go`
- Modify: `README.md` ("Available Tools": addressing, `version`/`expected_version`, the new `get_note` shape, the error formats)

**Interfaces:**
- Consumes: everything Tasks 2–3 produce.
- Arguments. **[Decided Q3]** Recommended: existing arguments are only extended.
  - `path` keeps its name. Its description on every note tool becomes: `Note path (with or without .md) or bare note name, e.g. "projects/x/Note.md" or "Note". A leading "/" means the vault root.`
  - `expected_version` (string) is added to `append_to_note`, `patch_note`, `update_frontmatter` and `delete_note`. It is listed in `Required` for `patch_note` and `delete_note`.
  - `create_note`'s `path` stays a path. It goes through `relArg`, so a leading `/` means the vault root and absolute paths are refused.
- Outputs:
  - `get_note` → `{"path":<canonical>,"version":<token>,"content":<text>}`, compact JSON. **[Decided Q2]**
  - `get_note_metadata` → the existing object plus `"version"`, and `"path"` becomes canonical.
  - `create_note`, `append_to_note`, `patch_note` and `update_frontmatter` → `{"success":true,"path":<canonical>,"version":<new token>}`.
  - `delete_note` → `{"success":true,"path":<canonical>}`.
  - `get_backlinks`, `get_outgoing_links` and `get_linked_mentions` label `path` with the address result.
  - `get_frontmatter` is unchanged *(planner's call: its result is the raw frontmatter map, and adding a key would collide with user keys)*.
- Errors are passed through as `err.Error()`. Graph tools already do this. Read and write tools keep their `failed to …: ` prefix *(planner's call: these prefixes are existing behavior, and phase 6 settles final wording)*.
- Descriptions:
  - `patch_note`: `Replace a note's whole content. Requires expected_version (the "version" from get_note); refused with the current version if the note changed.`
  - `delete_note`: the same requirement.
  - `append_to_note` and `update_frontmatter`: `expected_version is optional`.

- [ ] **Step 1: Write the failing tests** in `addressing_test.go` (using the `env` helper from `write_tools_test.go`):
  - `TestGetNoteVersioned`: unmarshal; `path`, `version` and `content` match the file; `version` matches `^sha256:[0-9a-f]{16}$`.
  - `TestPatchNoteWithoutVersionRefused` (roadmap exit criterion): `patch_note {path, content}` → `isErr`, the text contains `expected_version is required`, and the file bytes are unchanged. With a stale version → the text contains `version conflict:` and `current version: sha256:`. With the right version → ok, and the returned version equals the version `get_note` now reports.
  - `TestDeleteNoteWithoutVersionRefused`: the same pattern.
  - `TestAppendVersionOptionalTool`.
  - `TestNoteToolsByName`: for `get_note`, `get_note_metadata`, `get_frontmatter`, `append_to_note`, `get_backlinks`, `get_outgoing_links` and `get_linked_mentions`, `path: "Deep Note"` succeeds and reports `sub/Deep Note.md` where a path is reported.
  - `TestNoteToolsAmbiguousAndDidYouMean`: the exact texts from Task 2, wrapped in each tool's prefix.
  - `TestAbsoluteArgsRefused`: `get_note` and `create_note` with `filepath.Join(e.root, "A.md")` → `isErr`, the text contains `absolute paths are not accepted`, and not `e.root`.
  - `TestDescriptionsMentionVersion`: `patch_note` and `delete_note` list `expected_version` in `InputSchema.Required` and in their descriptions.
  - Update `TestPatchNote`/`TestDeleteNote` to read the version first.
- [ ] **Step 2: Run** `go test ./internal/tools`. Expected: FAIL.
- [ ] **Step 3: Implement** the handler changes and the README sections.
- [ ] **Step 4: Run** `gofmt -l . && go vet ./... && go test -race ./...`. Expected: clean and PASS.
- [ ] **Step 5: Commit** `feat(tools): address notes by name, return versions, require them for replace and delete`

---

### Task 5: No absolute path in any tool result (sweep)

**Files:**
- Create: `internal/tools/sweep_test.go`
- Modify: `internal/vault/io.go` (`ReadCanvas` applies `relArg`; any other leak the sweep finds is fixed at its source, not filtered in the tools)

**Interfaces:**
- Consumes: `relArg`, `ErrAbsolutePath` (Task 2); `server.MCPServer.ListTools() map[string]*server.ServerTool` (mcp-go v0.48.0).

- [ ] **Step 1: Write the failing test** `TestNoToolOutputContainsAbsolutePath`:
  - Assert that `ListTools()` keys equal the 20 known tool names, so a new tool forces an update of this test.
  - Vault setup: `A.md`; `dir.md/` as a directory; `a/Dup.md` and `b/Dup.md`; a symlink `out.md` to a file outside (skipped if symlinks are unavailable); `x.canvas`.
  - For every tool, and for every string property in its `InputSchema.Properties`, call the tool with that property set to each hostile value. Other required strings are `"x"`, other required objects are `{"k":"v"}`, and versions are `"sha256:0000000000000000"`.
  - The hostile values: `filepath.Join(root,"A.md")`, `filepath.Join(root,"missing.md")`, the unresolved `t.TempDir()` spelling of the same, `"/etc/passwd"`, `"C:\\Windows\\win.ini"`, `"\\\\srv\\share\\x.md"`, `"../outside.md"`, `"missing.md"`, `"dir.md"`, `".obsidian/x.md"`, `"out.md"`, `"Dup"` and `"a\x00.md"`.
  - Assert, for every result (error or not), that the text contains none of: `root`, `filepath.ToSlash(root)`, the pre-`EvalSymlinks` temp dir (macOS `/var` vs `/private/var`), or `filepath.Dir(root)`.
- [ ] **Step 2: Run** `go test ./internal/tools -run TestNoToolOutputContainsAbsolutePath`. Expected: FAIL at least on `get_canvas` with an absolute path. Today `ReadCanvas` accepts it and the not-found error echoes it.
- [ ] **Step 3: Fix** every leak at its source in the vault: `ReadCanvas` → `relArg`, plus anything else the run reports.
- [ ] **Step 4: Run** `go test -race ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `test(tools): sweep every tool for absolute paths in output`

---

### Task 6: Two-process race test (exit criterion) and concurrency docs

**Files:**
- Create: `internal/tools/race_test.go`
- Modify: `README.md` (a "Concurrency" section, and in "Security" the `.obsidian-mcp/` directory, the protected segment, the lock behavior and the NFS/SMB limitation)

**Interfaces:**
- Consumes: the tool handlers via `newEnv`-style registration on a given vault dir. The helper runs the real handlers in a separate OS process: the test binary re-executes itself.
- Helper protocol *(planner's call)*:
  - `TestRaceHelper` skips unless `OBSIDIAN_MCP_RACE_ROLE` is set.
  - It reads `OBSIDIAN_MCP_RACE_VAULT`, `_OP` (`create|append|delete|append1|deleteSame`), `_ID` (`A|B`) and `_N`.
  - It registers the tools on `vault.New(dir)`, then blocks until it reads `go\n` on stdin, which is the start barrier.
  - It writes one JSON line `{"i":i,"ok":bool,"text":"..."}` per operation.
  - The parent launches both helpers with `exec.Command(exe, "-test.run=^TestRaceHelper$", "-test.count=1")` and writes `go\n` to both stdins. It waits with a 60 s timeout.

- [ ] **Step 1: Write the test** `TestTwoProcessRace` with subtests:
  - `create`: N=100; both helpers call `create_note {path:"race/c<i>.md", content:"from <ID>"}`. For each i, exactly one `ok`. The file content is `from <winner>`, and the loser's text contains `already exists`.
  - `append`: one note `race/log.md`; each helper appends N=100 lines `<ID>-<i>`. Every call is `ok`, and the final file contains each of the 200 lines exactly once.
  - `delete`: the parent creates `race/d<i>.md` for N=50, reads each `version` via `get_note`, and passes the versions to helper B in a file.
    - Helper A appends `"A"` to each note, without a token.
    - Helper B deletes each note with its original token.
    - For each i, exactly one of A and B is `ok`. The file exists iff A's call was `ok`, and then its content ends with `A`.
  - `deleteSame`: both helpers delete the same 50 notes with the same tokens. For each i, exactly one `ok`; the other text contains `note not found`.
- [ ] **Step 2: Prove that the test detects the bug.** Run `git worktree add "$SCRATCH/base" develop`, copy `race_test.go` into `$SCRATCH/base/internal/tools/`, then run `go test ./internal/tools -run TestTwoProcessRace -count=1` there. `env` lives in `write_tools_test.go` on develop too; unknown arguments such as `expected_version` are ignored there. Expected:
  - `create`: PASS, because `os.Link` is already exclusive
  - `append`: FAIL, with lost lines
  - `delete`: FAIL, with both A and B `ok` for some i
  
  Record the output for the PR, then remove the worktree.
- [ ] **Step 3: Run on this branch:** `go test -race ./internal/tools -run TestTwoProcessRace -count=3`. Expected: PASS all three times.
- [ ] **Step 4: Write the README sections.** Then run `gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race ./...`. Expected: clean and PASS.
- [ ] **Step 5: Commit** `test(tools): two-process race on create, append and delete`

---

### Phase exit (before opening the PR)

- [ ] **Independent security review** (mandatory: this phase touches writes and path handling), by a fresh reviewer and not the implementer. Focus areas:
  - Lock dir creation via `os.Root` (symlink, file, or junction on Windows).
  - Every name-addressed write still passes `resolveNote` before any FS call.
  - The TOCTOU window between `AddressNote` and the locked read.
  - The `relArg` absolute and leading-`/` policy on all three OSes.
  - `.obsidian-mcp` is unreachable through tools.
  - The sweep test covers all 20 tools.
  - No token or error leaks content or absolute paths.
  - Fix every Medium-or-higher finding.
- [ ] Open the PR against `develop`. The description includes:
  - the Task 6 Step 2 red/green evidence
  - a **Breaking changes (unreleased v2)** list: the `get_note` shape; `expected_version` required on `patch_note`/`delete_note`; `patch_note` no longer creates missing notes; absolute paths refused; `.obsidian-mcp/` created in the vault
  - the decisions table (Q1–Q9)
  
  CI must be green on all three OSes.

## Decisions (all recommended defaults approved)

| Q | Task | Decision |
|---|---|---|
| Q1 token format | 3 | `sha256:` + 16 hex chars (64 bits) of SHA-256 over the raw bytes; opaque to callers |
| Q2 `get_note` shape | 4 | Compact JSON `{path, version, content}` |
| Q3 argument names | 4 | Keep `path` (now also accepts names); add `expected_version`; output field `version` |
| Q4 absolute / leading `/` | 2, 5 | Refuse absolute paths inside the vault root and Windows volume/UNC paths with a fixed message (no echo); a leading `/` means the vault root |
| Q5 exact path vs name | 2 | An existing exact path wins; only a non-exact argument goes through name resolution |
| Q6 `update_frontmatter` token | 3, 4 | Optional (a merge applied under the guard, like append) |
| Q7 error shape | 2, 4 | One plain-text line; ≤10 candidates; ≤5 suggestions |
| Q8 lock location | 1 | `<vault>/.obsidian-mcp/write.lock` (hidden, protected, never deleted) |
| Q9 breaking callers now | 3–6 | Yes: land the breaking changes on `develop` in this phase and list them in the PR; phase 6 writes the migration table |
