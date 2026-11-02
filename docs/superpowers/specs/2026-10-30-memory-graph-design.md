# obsidian-mcp v2: Memory Graph for Agents (Design)

Status: draft for maintainer approval · Date: 2026-10-30 · Target release: v2.0.0

## 1. Intent

The community has been asking for a way to use a folder of Obsidian-style markdown as **persistent memory for AI agents** (Claude Code, Codex, others). Agents write and organize most of the notes. Humans read them and sometimes edit them.

The agent loop mirrors a memory service like `mem_search`/`mem_save`, but every memory is a plain `.md` file:

1. **Before a task**, the agent searches memory and gets a short, relevant context block.
2. **During the task**, it saves important decisions as they happen.
3. **After the task**, it saves or updates a summary of what it did. It updates an existing note instead of creating a duplicate.

The notes form a graph through `[[wikilinks]]` and tags. Humans can browse that graph in Obsidian, and later in our own viewer.

Graphify (github.com/Graphify-Labs/graphify) is the reference the maintainer pointed to. We take its retrieval model and its memory loop, and keep markdown as the source of truth (details in §6).

### Why now
Users report that "graphs don't look the same / lose links". A recon on 2026-10-28, run against synthetic vaults, confirmed it:

- The graph resolves links only as paths from the vault root. With normal basename links (`[[Note]]`), a 5,000-note vault had **0 edges**.
- Backlinks use a different rule (basename), so backlinks and the graph disagree with each other.
- Markdown links are ignored. Links inside code are counted. Unresolved links are dropped.
- There is no move/rename tool, so moving a note breaks every link to it.
- `get_orphaned_notes` is O(N²): an estimated ~8 minutes at 5,000 link-less notes.
- Every call re-reads the whole vault, because the cache is never read.
- `get_link_graph` returns 5.5 MB of JSON, which is unusable in an agent's context.

## 2. Users and modes

The design must support all three modes:

| Mode | Description | Requirement |
|---|---|---|
| A. With Obsidian | The vault is also opened in the Obsidian app | Anything the MCP writes must render and link correctly in Obsidian, and its graph must match Obsidian's graph view |
| B. Without Obsidian | A plain folder of Obsidian-format `.md` files | The MCP does the housekeeping the app would do (link updates on move/rename, link resolution) |
| C. Viewer (later) | Our own graph viewer | Out of scope now. The index must be able to feed it later without a rewrite |

**One central vault** holds memory for many projects. Each memory note is marked with its project, so there are no per-project vaults.

## 3. Goals

1. **The graph matches Obsidian.** Link resolution follows Obsidian's rules. The graph, backlinks, outgoing links and orphans all use one resolver and agree with each other.
2. **It scales to large vaults.** 10,000 notes stay interactive. The targets in §8 are budgets, not guarantees.
3. **Tools are built for agents.** Every response is bounded: list tools paginate, and graph and search tools take a token or size budget. Errors are actionable ("did you mean `projects/x/Note.md`?").
4. **Organizing doesn't break anything.** Move and rename update inbound links. Edits can target one section without rewriting the whole note. Notes can be referenced by name, not only by full path.
5. **Memory tools come first.** Search, save and session-start context work with a standard memory frontmatter, so agents don't need to know the vault layout.
6. **Graph retrieval in the Graphify style.** Search seeds a traversal over links, and the result is trimmed to a budget. There are also neighbors, shortest path and an overview of hub notes.
7. **Safety is preserved.** All the security properties from PR #2 stay intact: containment before any FS call, symlink-target `.md` checks, atomic writes, `.git`/`.obsidian` blocked.

## 4. Non-goals (this release)

- A graph viewer UI (mode C). It is planned for later, so leave a seam for it (see §7.6).
- Indexing source code (functions, imports, calls). Notes may *mention* code as text, but the repo is never parsed.
- Semantic or embedding search. Keyword ranking plus graph traversal is the retrieval model, which is also what Graphify does. Leave a seam for an external ranker.
- LLM-based extraction of entities or relations. The agent writes links and relations itself.
- HTTP/SSE transport.
- Reading or writing Obsidian's `.obsidian/` config. The one possible exception: reading the attachment folder and the "new link format" setting, and only if the builder finds it necessary.

## 5. Anti-goals (failure even if it "works")

- Any data that exists only outside the markdown files (a DB, or JSON as the source of truth). Deleting the index must lose nothing, because it is rebuilt from the `.md` files.
- Writing notes that Obsidian renders or links differently from what the MCP reported.
- A silent change to human-written content: rewriting a whole note when one section changed, or reformatting frontmatter keys the tool didn't touch.
- Losing a human's concurrent edit because an agent overwrote it.
- Returning unbounded output to an agent.
- Adding runtime dependencies: Python, CGO, external services, API keys.

## 6. Constraints and decided "how"

Decided with the maintainer:
- **Breaking changes are allowed.** Tools may be renamed, merged or changed. Ship as **v2.0.0**, with a "Breaking changes" section in the release notes and a migration table (old tool → new tool) in the README.
- **Scope is notes only** (see §4).
- **One central vault**, and every memory note carries a `project` field.
- **Several agents at once is a supported case.** Several server processes may serve the same vault at the same time, for example Claude Code in two repos plus Codex. Requirements:
  - Each process refreshes its view of a file before it writes to it.
  - Memory dedupe on save is guarded across processes, so two agents never create the same memory twice.
  - Move/rename is guarded against concurrent writers.
  - The mechanism is the builder's choice: a lockfile with stale-lock recovery, OS file locks, create-exclusive. It must work without CGO on all three OSes. A pure-Go dependency such as `golang.org/x/sys` is acceptable.
  - Lock files are never listed as notes and never count as vault content.
- **Agents may write anywhere in the vault, with protection.** Human-written notes are writable too, but only in these ways:
  - Additive operations: create, append, section append/prepend.
  - Section-scoped replace.
  - Whole-note replace, only when the call carries the version token of the content being replaced (§7.3). A whole-note replace without a token is refused.
  - Move/rename rewrites links anywhere.
  - Delete also requires the version token.

Decided by the designer (the maintainer can override any of these):
- **Stack:** Go with `mark3labs/mcp-go`, one static binary, CGO disabled. This is what the project already uses.
- **Source of truth:** the markdown files. The graph and search index live in memory and are rebuilt at startup.
- **Index updates:** the index refreshes incrementally when files change. Changes come from the server's own writes, and from external edits detected by stat checks or a watcher (§7.2).
- **Module path:** Go requires the `/v2` suffix for v2+ (`github.com/nemesis312/obsidian-mcp/v2`). Without it, `go install ...@latest` keeps installing 1.x.
- **Documentation language:** design docs, README and tool descriptions are in English, since the repo is public.

## 7. Functional design

### 7.1 Link resolution (one resolver for everything)

A single resolver module turns every link occurrence into a *target* and a *resolution status*. All graph features use it.

Links it must recognize:
- `[[Target]]`, `[[Target|Alias]]`, `[[Target#Heading]]`, `[[Target#^block]]`, and combinations of these.
- Embeds `![[...]]`.
- An escaped pipe inside tables: `[[Target\|Alias]]`.
- Markdown links `[text](Target.md)`, relative paths, URL-encoded names (`%20`) and the `<...>` form. External URLs are ignored.
- Links in frontmatter values, quoted or in lists.
- Text inside fenced code blocks and inline code is ignored, as Obsidian does.

Resolution rules, matching Obsidian as closely as possible:
1. A path-qualified link (`folder/Note`) resolves relative to the vault root. Relative markdown links resolve relative to the linking note.
2. A bare name resolves to the note with that basename anywhere in the vault.
3. If several notes share the basename, use Obsidian's tie-break. The builder must verify Obsidian's actual behavior; it is believed to prefer the shortest path, then a deterministic order. The ambiguity is recorded and exposed to tools; the resolver never silently picks one.
4. Matching ignores case. Unicode is normalized (NFC) on both sides.
5. Non-`.md` targets such as images and PDFs resolve to attachment nodes when the file exists.
6. Links that don't resolve become **unresolved ("ghost") nodes**. They appear in the graph and are flagged as such. A note whose only links are unresolved is still not an orphan, because Obsidian counts unresolved links the same way. The builder should verify this.
7. Deduplicate per note by resolved target, not by the raw link string.

Acceptance: a fixture vault covering every case above produces the same edges as Obsidian's graph view. Building that fixture is the builder's job; checking it in Obsidian by hand once is acceptable.

### 7.2 Index

At startup the server builds an in-memory index: notes, frontmatter, tags, headings and sections, resolved edges, ghost nodes, and a search index. Every read tool answers from the index.

- The index refreshes incrementally: on the server's own writes immediately, and on external edits. The detection mechanism is the builder's choice (polling mtimes, fsnotify, or a cheap stat check before queries). It must work on macOS, Linux and Windows without CGO.
- Orphans, backlinks and tag lookups must not re-read the vault (this fixes the O(N²) path).
- The existing unused `Cache` is removed or replaced.

### 7.3 Note addressing and editing

- **Addressing.** Any tool that takes a note accepts a full path with or without `.md`, or a bare note name resolved by the §7.1 rules.
  - An ambiguous name returns an error listing the candidates.
  - A name that doesn't exist returns "did you mean …" suggestions.
  - Errors never include absolute filesystem paths.
- **Section edits.** Replace, append or prepend content under a specific heading. The note's other bytes are left exactly as they were.
- **Optimistic concurrency.** Read tools return a version token (a content hash).
  - Every modifying tool accepts an expected version and refuses with a clear conflict error if the file changed since then. The error includes the current version token, so the agent can re-read and retry.
  - The token is **required** for whole-note replace, section replace and delete (§6).
  - Additive operations (create, append, section append/prepend) may omit it. They re-read the file under the write guard and apply only their addition, so nothing someone else wrote is lost.
- **Move/rename.** Move or rename a note and rewrite every inbound link: wikilinks, markdown links and frontmatter links. Rewriting preserves aliases, headings and block refs, and the link style used (`[[name]]` stays name-only when it is still unambiguous; otherwise it becomes path-qualified).
  - The tool reports every file it changed and every file it failed to update.
  - It refuses if the destination already exists.
  - A dry-run option lists what would change without writing.
  - *Designer's call:* this release moves and renames **single `.md` notes**. Folder moves and attachment renames are out of scope; they would widen the write surface beyond `.md`.

### 7.4 Agent graph tools (Graphify-inspired)

| Capability | Behavior |
|---|---|
| Graph query | Keyword query → ranked seed notes (BM25/IDF over title, aliases, tags, headings and body; title and tags weighted higher) → bounded BFS over links and shared tags → result trimmed to a token budget. Returns notes with snippets, the connecting edges, and why each note was included. Optional explicit `seeds` let an external ranker drive it later. |
| Neighbors | Linked notes in, out or both, filtered by relation type, depth 1–N, paginated. |
| Shortest path | The path between two notes over links. |
| Overview | Counts, hub ("god") notes, top tags, ghost-node count and orphan count. Small and cheap; meant for session start. |
| Search | Ranked results with snippets and match highlights, filtered by project, type, tags, folder and date. Paginated. |
| Orphans / unresolved | Paginated lists of each. |

The full-graph dump (`get_link_graph`) is replaced or bounded: paginated, filterable, or written as an export file rather than returned inline. The builder chooses which.

**Typed relations.** Frontmatter may declare typed links, for example:
```yaml
relations:
  supersedes: ["[[Old decision]]"]
  caused_by: ["[[Bug 123]]"]
```
These become typed edges. Plain body links are untyped `links_to` edges. The set of relation names is open: any key is allowed, and the docs recommend a small standard set.

### 7.5 Memory tools

Memories are ordinary notes with a standard frontmatter, so they work in Obsidian and in a plain folder.

```yaml
---
id: my-api-jwt-refresh  # stable identity; generated from project + title if not supplied
title: JWT refresh strategy
type: decision          # decision | session | bug | note | lesson (open set)
project: my-api         # required for memory notes
status: active          # active | open | resolved | superseded (open set; default by type)
tags: [auth, jwt]
created: 2026-10-30T14:05:00Z
updated: 2026-10-30T14:05:00Z
outcome: useful         # optional: useful | dead_end | corrected
correction: "..."       # optional, used with outcome: corrected
relations: {}           # optional typed links (§7.4)
---
```

All of the following are designer's calls, made to answer the builder check:

- **Identity.** A memory is identified by its frontmatter `id`.
  - If the caller supplies no `id`, it is derived from `project` plus a slug of `title`. The filename is not the identity, so renaming the file never breaks dedupe.
  - Notes without an `id` that have `project`, `type` and `title` match on that triple.
- **Save memory.**
  - Creates a note from the schema, or updates the existing memory with the same identity. The tool result says which one happened.
  - **By default, an update is additive.** It appends a dated entry under an `## Updates` heading and bumps `updated`. Frontmatter changes (tags, status, relations) are merged; lists are unioned and scalars are overwritten. Keys the caller didn't send are left untouched.
  - Replacing the body of an existing memory requires the version token (§7.3).
  - Related notes passed by the caller become links, and typed relations go into `relations`. The file is placed by the layout rule below.
- **Status and lifecycle.**
  - Defaults by type: `bug` → `open`, `decision` → `active`, everything else → `active`.
  - A `supersedes` relation marks the target `superseded`. This is applied by save under the write guard, as a frontmatter merge on the target.
  - Session context excludes `resolved` and `superseded` notes unless they are asked for.
- **Search memory.** The §7.4 search, scoped to memory notes and filtered by project, type, status, tags and date.
- **Session context.**
  - One call returns a bounded block (default about 2,000 tokens) for a project. In priority order: open bugs, active decisions (most recent first), the current lessons, then hub notes.
  - This is the "before a task" call. It never writes.
- **Lessons.**
  - Lessons are computed on the fly from `outcome`/`correction` signals, with recent items ranked higher, and are included in the session context.
  - A separate, explicit tool can also materialize them into `memory/<project>/lessons.md`. It writes **only** between generated-block markers (`<!-- obsidian-mcp:lessons:start -->` … `end`), so humans can write above and below the block.
  - Lessons never run as a hidden side effect of save.
  - Modeled on Graphify's `reflect`, but exposed through MCP.

**Layout (designer's call; the maintainer can override).** The default is `memory/<project>/<type>/<YYYY-MM-DD>-<slug>.md`. The root folder name is configurable with a flag or env var. Any note anywhere in the vault that has `project` and `type` frontmatter counts as a memory, so humans can file notes wherever they like.

**Links the server writes (user decision, 2026-10-31).** Links the server writes into notes (wikilinks, embeds and markdown links) must never be inside inline code or code blocks: no backticks or fences around them. Obsidian and this server's own index ignore links inside code. Tool descriptions that invite agents to write links must say so.

MCP resources are optional; the builder decides. Graphify exposes a report, stats and similar views as resources. If our client support is good, the overview and the per-project context can be exposed as resources too.

### 7.6 Viewer seam (no UI now)

The index exposes a stable, documented graph export format (nodes, edges, types, ghosts, project). A later viewer, such as a `graph.html` export with embedded JS, can consume it without touching the resolver.

### 7.7 Existing tools

All 20 current tools are re-evaluated against this design. Each one is kept, renamed, merged or removed, and the migration table records the decision. The builder decides the final tool list, with these limits:
- Fewer, clearer tools beat many overlapping ones, because LLMs pick tools from their descriptions.
- Every description says what the tool returns, its limits, and how to address notes.

## 8. Quality bar

- **Performance** (budgets; measure on a synthetic vault and report the numbers): on 10,000 notes, a cold index build in seconds, and graph query, search, neighbors and session context answered in tens of milliseconds once warm. No operation is O(N²) over the vault.
- **Response size:** every tool has a default limit and a hard cap. No response over about 100 KB by default.
- **Tests:**
  - A link-resolution fixture vault covering §7.1, kept in the repo as testdata.
  - Move/rename round trips.
  - Concurrency conflicts, including **two server processes** on the same vault racing save-dedupe, append and move. The test must use real separate processes, not only goroutines.
  - Index refresh after external edits.
  - Security regressions: all existing tests stay green.
  - CI on the three OSes.
- **An independent security review** of anything that touches path handling or writes. This step already caught real bugs in v1.x.

## 9. Left to the builder

- Package structure and the internal data structures of the index.
- The file-change detection mechanism (within the §7.2 constraints).
- The search ranking formula and tokenization, including support for non-English text.
- The final tool names and list, and the migration table.
- Whether `get_link_graph` is paginated, filtered or exported.
- Whether to expose MCP resources.
- How to break the work into PRs and phases, as long as each phase leaves `develop` releasable.
