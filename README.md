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

### Read Tools (5)

1. **list_vault_files** - List all markdown files
2. **get_note** - Read note content
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

`kind` is `wikilink`, `embed` or `markdown`. `to_kind` is `note`, `attachment` or `ghost`. A ghost is a link whose target does not exist; its `to` is the normalized target text, lowercase and NFC-normalized (see rule 7). `ambiguous` is `true` when several files match the target (a shared basename, or paths that differ only in case) and the one reported was picked by the tie-break rules. The graph has a node for every note, every linked attachment and every ghost, sorted by kind and then id; edges are sorted by `from`, `to_kind` and `to`.

### Tag/Canvas Tools (5)

16. **list_all_tags** - List all unique tags
17. **get_notes_by_tag** - Find notes with specific tag
18. **rename_tag** - Rename tag across all notes
19. **list_canvases** - List all canvas files
20. **get_canvas** - Read canvas file content

## How links are resolved

The link index follows Obsidian's rules. It reads wikilinks (`[[Note]]`), embeds (`![[Note]]`) and markdown links (`[text](Note.md)`), in the body and in the frontmatter. Links inside fenced code and inline code are ignored, and so are external URLs (`https://...`) and markdown same-note anchors (`[text](#heading)`).

1. **Target text.** A wikilink target is split at `#` (heading), `#^` (block) and `|` (alias). `\|` also works, for tables. Markdown targets may be wrapped in `<...>` and percent-encoded (`My%20Note.md`). A link with only a heading, such as `[[#Section]]`, points to the note it is in.
2. **Case and Unicode.** Matching ignores case and Unicode normalization (NFC). For notes, `.md` is optional and matches in any case (`[[Note.MD]]`).
3. **Markdown links** (not starting with `/`) are tried relative to the folder of the note first, then like a wikilink. `./` and `../` work. A target that climbs out of the vault is unresolved.
4. **Targets with a `/`** are paths from the vault root (a leading `/` is allowed). Suffixes of a path are not matched.
5. **Targets without a `/`** match the file name anywhere in the vault. Notes are tried before attachments. A target is an attachment only when it has an extension other than `.md` (`pic.png`, `doc.pdf`).
6. **Duplicate names.** When several notes share a name, the winner is: the one in the same folder as the source note, then the one with the fewest path segments, then the first in alphabetical order of the lowercase path. The edge is marked `ambiguous`. Paths that differ only in case or Unicode form (`Note.md` and `note.md`) are never picked silently: the link is `ambiguous`, and the reported path is the alphabetically first in byte order. Use a path (`[[b/c/Dup]]`) to pick one on purpose.
7. **No match** gives a ghost: the edge is kept, pointing to the target as it was matched, lowercase and NFC-normalized, with surrounding spaces, `./`, a leading `/` and a trailing `.md` removed and the path cleaned. `[[Ghost]]`, `[[ghost]]`, `[[Ghost.md]]`, `[[./Ghost]]` and `[g](Ghost.md)` are the same ghost, `ghost`. A target that is only `/` or `./` names nothing and gives no edge.

Each note keeps one edge per target, whatever the number of links to it or the ways it is spelled (`[[Note]]`, `[[./ ./note]]`). The fixture vault in `internal/index/testdata/linkvault` has one note per case, with the expected edges in its README. These rules are unverified against the Obsidian app itself.

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
    │   ├── io.go        # File I/O and the vault walk
    │   ├── atomic.go    # Atomic and no-clobber writes
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

## Security

- **Vault confinement**: paths that escape the vault, including via symlinks, are rejected.
- **Protected directories**: `.git/` and `.obsidian/` are blocked.
- **Markdown-only writes**: `create_note`, `append_to_note`, `patch_note`, `update_frontmatter` and `delete_note` only operate on `.md` files.
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
