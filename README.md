# Obsidian Vault MCP Server

A high-performance Model Context Protocol (MCP) server for Obsidian vaults, written in Go. Provides filesystem-based access to your Obsidian notes with full support for frontmatter, wikilinks, tags, and canvas files.

## Features

- **Zero Dependencies**: Single binary, no runtime requirements
- **Fast & Secure**: Built in Go with path traversal protection
- **Filesystem-Based**: Direct vault access, no Obsidian app required
- **Full Obsidian Support**: Frontmatter (YAML), wikilinks, tags, canvas files
- **20 MCP Tools**: Complete CRUD operations + graph navigation

## Installation

### Binary Releases

Download prebuilt binaries from the [releases page](https://github.com/nemesis312/obsidian-mcp/releases).

Available for:
- macOS (Universal Binary — arm64 + amd64)
- Linux (amd64, arm64)
- Windows (amd64, arm64)

### From Source

Requires Go 1.26.1+ (see `go.mod`):

```bash
go install github.com/nemesis312/obsidian-mcp/cmd/obsidian-mcp@latest
```

Or build locally:

```bash
git clone https://github.com/nemesis312/obsidian-mcp.git
cd obsidian-mcp
make build
```

## Usage

### Environment Variable

```bash
export OBSIDIAN_VAULT_PATH="/path/to/your/vault"
obsidian-mcp
```

### Command Line Flag

```bash
obsidian-mcp --vault="/path/to/your/vault"
```

If neither is set, the server falls back to Obsidian's own `obsidian.json`. With multiple vaults it prefers the open vault, then the most recently used.

### Version

```bash
obsidian-mcp --version
```

### MCP Configuration

Add to your MCP client configuration (e.g., Claude Desktop):

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "/path/to/obsidian-mcp",
      "env": {
        "OBSIDIAN_VAULT_PATH": "/path/to/your/vault"
      }
    }
  }
}
```

## Available Tools

### Addressing, versions and errors

Every tool that takes a note accepts `path` as a vault-relative path (with or without `.md`) or as a bare note name (`"Deep Note"`), resolved the way a `[[Deep Note]]` link written at the vault root would be. A leading `/` means the vault root. Absolute filesystem paths are refused (`absolute paths are not accepted`). A name that matches several notes is an error that lists them (`ambiguous note name "Dup": matches a/Dup.md, b/Dup.md; pass one of these paths`); an unknown name lists close matches (`note not found: Deep Not; did you mean: sub/Deep Note.md?`). Results report the canonical path.

Every note has a `version`: `sha256:` plus 16 hex characters of the hash of its bytes. `get_note` and `get_note_metadata` return it, and every write returns the new one.

| Tool | `expected_version` | Result |
| --- | --- | --- |
| `patch_note`, `delete_note` | Required. Without it the call is refused (`expected_version is required ...`). If the note changed, the call is refused with `version conflict: ... current version: sha256:...`. | `patch_note`: `{"success":true,"path":"n.md","version":"sha256:..."}`. `delete_note`: `{"success":true,"path":"n.md"}`. |
| `append_to_note`, `update_frontmatter` | Optional. When given, it is checked the same way. | `{"success":true,"path":"n.md","version":"sha256:..."}` |
| `create_note` | Not used. `path` is a path, not a name; a leading `/` means the vault root. | `{"success":true,"path":"n.md","version":"sha256:..."}` |

`get_note` returns `{"path":"n.md","version":"sha256:...","content":"..."}`. `get_note_metadata` returns its usual object plus `version`, with `path` canonical. `get_frontmatter` still returns the bare frontmatter map.

Error messages that changed: `append_to_note`, `patch_note`, `update_frontmatter` and `delete_note` on a path that is not a `.md` note now say `note not found`. `create_note` still refuses another extension with `only .md notes are supported (got ".png" extension)`. `get_linked_mentions` with an unknown name now says `note not found` too. `get_canvas` reads `.canvas` files only and refuses anything else with `not a canvas file`.

### Read Tools (5)

1. **list_vault_files** - List all markdown files
2. **get_note** - Read note content with its path and version
3. **search_vault** - Search for notes containing query
4. **get_frontmatter** - Extract YAML frontmatter
5. **get_note_metadata** - Get frontmatter, links, and backlinks

### Write Tools (5)

6. **create_note** - Create new note with optional frontmatter
7. **append_to_note** - Append content to existing note
8. **patch_note** - Replace note content
9. **update_frontmatter** - Update frontmatter fields (preserves body)
10. **delete_note** - Delete a note

### Graph/Link Tools (5)

11. **get_backlinks** - Find notes linking to target
12. **get_outgoing_links** - Get all links from note: wikilinks, embeds and markdown links
13. **get_orphaned_notes** - Find notes with no incoming or outgoing links
14. **get_linked_mentions** - Find notes mentioning target name
15. **get_link_graph** - Build full vault link graph

Links are resolved the way Obsidian does it; see [How links are resolved](#how-links-are-resolved). Paths in results are vault-relative and use `/` on every OS.

| Tool | Result |
| --- | --- |
| `get_backlinks` | `{"path": "b.md", "backlinks": [{"from": "a.md", "kind": "wikilink", "ambiguous": false}], "count": 1}`. Sorted by `from`. Self-links are left out. |
| `get_outgoing_links` | `{"path": "a.md", "links": [{"to": "b.md", "to_kind": "note", "kind": "wikilink", "ambiguous": false}], "count": 1}`. Source order, one entry per target. |
| `get_orphaned_notes` | `{"orphans": ["lonely.md"], "count": 1}`. Sorted. |
| `get_link_graph` | `{"nodes": [{"id": "a.md", "kind": "note"}], "edges": [{"from": "a.md", "to": "b.md", "to_kind": "note", "kind": "wikilink", "ambiguous": false}], "node_count": 1, "edge_count": 1}`. |

`kind` is `wikilink`, `embed` or `markdown`. `to_kind` is `note`, `attachment` or `ghost`. A ghost is a link whose target does not exist; its `to` is the normalized target text, lowercase and NFC-normalized (see rule 8). `ambiguous` is `true` in two cases only: several files share the name or path suffix the target was matched on, and the one reported was picked by the tie-break rules; or the matched path has duplicates that differ only in case. An exact path is not ambiguous: `[[Peer]]` with both `Peer.md` and `md/Peer.md` in the vault goes to `Peer.md` with `ambiguous` set to `false`. The graph has a node for every note, every linked attachment and every ghost, sorted by kind and then id; edges are sorted by `from`, `to_kind` and `to`.

### Tag/Canvas Tools (5)

16. **list_all_tags** - List all unique tags
17. **get_notes_by_tag** - Find notes with specific tag
18. **rename_tag** - Rename tag across all notes
19. **list_canvases** - List all canvas files
20. **get_canvas** - Read canvas file content

## How links are resolved

The link index follows Obsidian's rules. It reads wikilinks (`[[Note]]`), embeds (`![[Note]]`) and markdown links (`[text](Note.md)`), in the body and in the frontmatter. Links inside fenced code and inline code are ignored, and so are external URLs (`https://...`). The rules below apply the same way to all three kinds.

1. **Target text.** A wikilink target is split at `#` (heading), `#^` (block) and `|` (alias). `\|` also works, for tables. Markdown targets may be wrapped in `<...>` and percent-encoded (`My%20Note.md`).
2. **Case and Unicode.** Matching ignores case and Unicode normalization (NFC). For notes, `.md` is optional and matches in any case (`[[Note.MD]]`).
3. **Only a heading or block** (`[[#Section]]`, `[text](#section)`) points to the note the link is in.
4. **`./` and `../`** make the target relative to the folder of the note, and only there: there is no fallback to the rest of the vault. A target that climbs out of the vault is unresolved.
5. **A leading `/`** makes the target an exact path from the vault root.
6. **An exact path from the vault root wins**, and the edge is not `ambiguous`, even when other files end with the same name (`[[Peer]]` goes to `Peer.md` even if `md/Peer.md` exists). Otherwise, any file whose path ends with the target, by whole folders, matches: `[[c/Dup]]` finds `b/c/Dup.md`, and `[[Dup]]` finds every `Dup.md`. This holds for markdown links too: `[text](Peer.md)` written in `md/` goes to a root `Peer.md` before `md/Peer.md`. Notes are tried before attachments. A target is an attachment only when it has an extension other than `.md` (`pic.png`, `doc.pdf`).
7. **Several suffix matches.** The winner is the one in the same folder as the source note (folder names compared as written, so `K/` is not `k/`), then the one with the fewest path segments, then the first in alphabetical order of the lowercase path. The edge is marked `ambiguous`. Paths that differ only in case or Unicode form (`Note.md` and `note.md`) are never picked silently: the link is `ambiguous`, and the reported path is the alphabetically first in byte order, after the same-folder rule for a suffix match. Use a longer path (`[[b/c/Dup]]`) to pick one on purpose.
8. **No match** gives a ghost: the edge is kept, pointing to the target as it was matched, lowercase and NFC-normalized, with surrounding spaces, `./`, a leading `/` and a trailing `.md` removed and the path cleaned. `[[Ghost]]`, `[[ghost]]`, `[[Ghost.md]]`, `[[./Ghost]]` and `[g](Ghost.md)` are the same ghost, `ghost`, even when `[[./Ghost]]` is written in another folder. A target that names a folder, not a file (only `/` or `./`, or a path that cleans to `.` or `..` only, such as `a/..` or `../`), gives no edge.

Each note keeps one edge per target, whatever the number of links to it or the ways it is spelled (`[[Note]]`, `[[a/../note]]`; `[[./Note]]` and `[[././note]]`). The fixture vault in `internal/index/testdata/linkvault` has one note per case, with the expected edges in its README. These rules were checked against the Obsidian app's metadata cache (`app.metadataCache.resolvedLinks`) on 2026-10-31.

The index skips hidden files and folders (`.obsidian`, `.git`), subfolders it lacks permission to read or search, and `.md` entries that are not regular files (FIFOs, sockets, devices); it never reads one through a symlink either. A vault root it cannot read fails the index build.

## Examples

### Create a Note

```json
{
  "name": "create_note",
  "arguments": {
    "path": "notes/meeting.md",
    "content": "# Meeting Notes\n\nDiscussed project timeline.",
    "frontmatter": {
      "title": "Team Meeting",
      "date": "2026-04-17",
      "tags": ["meeting", "project"]
    }
  }
}
```

### Search Vault

```json
{
  "name": "search_vault",
  "arguments": {
    "query": "project timeline"
  }
}
```

### Get Backlinks

```json
{
  "name": "get_backlinks",
  "arguments": {
    "path": "notes/project.md"
  }
}
```

Result:

```json
{
  "path": "notes/project.md",
  "backlinks": [{"from": "daily/2026-10-30.md", "kind": "wikilink"}],
  "count": 1
}
```

## Architecture

```
obsidian-mcp/
├── cmd/obsidian-mcp/    # Main entry point
└── internal/
    ├── vault/           # Vault operations
    │   ├── detection.go # Vault auto-detection
    │   ├── security.go  # Path validation
    │   ├── address.go   # Notes addressed by path or name, suggestions
    │   ├── version.go   # Version tokens and guarded edits
    │   ├── guard.go     # Cross-process write lock (guard_unix.go, guard_windows.go, guard_other.go)
    │   ├── io.go        # File I/O and the vault walk
    │   ├── atomic.go    # Atomic and no-clobber writes (atomic_windows.go, atomic_other.go)
    │   ├── graph.go     # Graph queries backed by the link index
    │   └── tags.go      # Tag management
    ├── links/           # Link extraction and resolution (no I/O)
    │   ├── extract.go   # Wikilinks, embeds, markdown links
    │   └── resolve.go   # Obsidian resolution rules
    ├── index/           # In-memory link index
    │   ├── index.go     # Notes, edges, backlinks, orphans, graph
    │   └── testdata/    # Link-resolution fixture vault
    ├── markdown/        # Markdown parsing
    │   ├── frontmatter.go # YAML frontmatter
    │   ├── codemask.go    # Masks fenced and inline code
    │   ├── tags.go        # #tag parsing
    │   └── canvas.go      # Canvas JSON
    └── tools/           # MCP tool handlers
        ├── read_tools.go
        ├── write_tools.go
        ├── graph_tools.go
        └── tag_tools.go
```

## Concurrency

Several obsidian-mcp processes can serve the same vault at once, for example one per agent or per project.

- **One writer at a time per vault.** Every write takes the vault's write lock first: an in-process lock plus an OS advisory lock (`flock` on Unix, `LockFileEx` on Windows) on `.obsidian-mcp/write.lock`. `rename_tag` takes it once per note it changes; if the lock stays busy, it stops waiting after the first timeout and reports the remaining notes that have the tag as failed.
- **Fresh reads under the lock.** While holding the lock, a write reads the note from disk, applies its change and writes the result atomically (temporary file, then rename). It never writes back a copy read earlier, so two processes appending to the same note keep both lines.
- **Versions guard destructive edits.** `patch_note` and `delete_note` need the `expected_version` from `get_note`. If another process changed the note in between, the call is refused with the current version; read the note again and retry. Additive writes (`create_note`, `append_to_note`, `update_frontmatter`) need no version and apply only their own change.
- **Exclusive create.** When two processes create the same path, one succeeds and the other gets `already exists`.
- **Bounded wait.** A write waits up to 5 seconds for the lock, then fails with `vault is busy: another obsidian-mcp process is writing; retry`.
- **No stale locks.** The OS releases the lock when the process that holds it exits or crashes, so there is nothing to clean up. The lock file itself is never deleted.
- **Readers never lock.** Because writes are atomic renames, a read sees either the old or the new note, never a partial one.

Limits:

- Other programs (the Obsidian app, sync clients, editors) do not take this lock. A version token still catches their changes before a `patch_note` or `delete_note`, but a write from such a program at the same instant as an obsidian-mcp write can overwrite it.
- Advisory locks are not reliable on network filesystems (NFS, SMB). Keep a vault that several processes write to on a local disk.
- On platforms without an OS file lock (aix, solaris), writes are refused rather than left unguarded.

## Security

- **Vault confinement**: paths that escape the vault, including via symlinks, are rejected.
- **Protected directories**: `.git/`, `.obsidian/` and `.obsidian-mcp/` are blocked at any depth: no tool lists, reads or writes them. Spellings Windows treats as the same folder are blocked too: a name with trailing dots or spaces (`.git.`) on every OS, and on Windows any path segment that contains `:`, ends in a dot or space, or looks like an 8.3 short name (`OBSIDI~1`).
- **Lock directory**: the server creates `.obsidian-mcp/write.lock` in the vault on the first write (see [Concurrency](#concurrency)). It is created through the vault root, so a symlink or junction at `.obsidian-mcp` cannot redirect it outside the vault; if `.obsidian-mcp` is not a real directory, or `write.lock` is not a regular file (a symlink included), writes are refused. The folder is created with mode `0700` and the lock file with `0600`, so other users on the machine cannot open the lock and block writes; a folder created by an earlier version keeps its mode. The lock file is never listed as a note. Add `.obsidian-mcp/` to `.gitignore` if the vault is a git repository.
- **Lock behavior**: the lock is advisory and serializes obsidian-mcp writers only; a write waits at most 5 seconds for it. It does not protect against other programs, and it is not reliable on NFS or SMB shares.
- **Markdown-only writes**: `create_note`, `append_to_note`, `patch_note`, `update_frontmatter` and `delete_note` only operate on `.md` files.
- **Symlinked notes**: a note that is a symlink is written through to its target, which must be a `.md` file inside the vault. `delete_note` on such a note deletes the target file and leaves the link in place, now dangling.
- **Canvas reads**: `get_canvas` reads `.canvas` files only (the symlink target too), and only regular files, so a FIFO cannot block it.
- **Safe tag renames**: `rename_tag` also renames nested tags (`#a/b`), skips code blocks, and reports per-file failures.
- **No code execution**: pure file system operations.

See [SECURITY.md](SECURITY.md) for the threat model and how to report vulnerabilities.

## Performance

- **In-memory link index**: refreshed by a stat walk before each graph query; only changed notes are re-read.

## Compatibility

- **Frontmatter**: YAML between `---` delimiters
- **Links**: `[[Page]]`, `[[Page|Alias]]`, `[[Page#Section]]`, `![[embed]]` and `[text](Page.md)`; see [How links are resolved](#how-links-are-resolved)
- **Tags**: Inline (`#tag`) and frontmatter (`tags: [tag1, tag2]`)
- **Canvas**: JSON format (`.canvas` files)

## Development

### Requirements

- Go 1.26.1+
- Make (optional)

CI runs `go vet`, `gofmt` and race tests on pull requests (`.github/workflows/ci.yml`).

### Build

```bash
make build
```

### Test

```bash
make test
```

### Coverage

```bash
make coverage
```

### Release

Releases are created automatically by pushing a version tag to `master`:

```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions will run goreleaser and publish binaries for all platforms.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). In short:

1. Fork the repository
2. Create a feature branch off `develop`:
   ```bash
   git checkout develop
   git checkout -b feature/your-feature
   ```
3. Commit your changes with clear messages
4. Push and open a Pull Request **targeting `develop`**
5. A maintainer review is required before merge
6. Releases are cut from `master` — the maintainer merges `develop` → `master` when ready

> PRs targeting `master` directly will not be accepted.

## License

MIT License — see [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built with [mcp-go](https://github.com/mark3labs/mcp-go)
- Inspired by the [Model Context Protocol](https://modelcontextprotocol.io/)
- Designed for [Obsidian](https://obsidian.md/)

## Support

- Issues: [GitHub Issues](https://github.com/nemesis312/obsidian-mcp/issues)

---

**Note**: This is a filesystem-based MCP server. It does not require the Obsidian app to be running and does not access Obsidian's internal APIs.
