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

Requires Go 1.22+:

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
12. **get_outgoing_links** - Get all wikilinks from note
13. **get_orphaned_notes** - Find notes with no links
14. **get_linked_mentions** - Find notes mentioning target name
15. **get_link_graph** - Build full vault link graph

### Tag/Canvas Tools (5)

16. **list_all_tags** - List all unique tags
17. **get_notes_by_tag** - Find notes with specific tag
18. **rename_tag** - Rename tag across all notes
19. **list_canvases** - List all canvas files
20. **get_canvas** - Read canvas file content

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

## Architecture

```
obsidian-mcp/
├── cmd/obsidian-mcp/    # Main entry point
├── internal/
│   ├── vault/           # Vault operations
│   │   ├── detection.go # Vault auto-detection
│   │   ├── security.go  # Path validation
│   │   ├── io.go        # File I/O
│   │   ├── cache.go     # In-memory cache (60s TTL)
│   │   ├── graph.go     # Link graph operations
│   │   └── tags.go      # Tag management
│   ├── markdown/        # Markdown parsing
│   │   ├── frontmatter.go # YAML frontmatter
│   │   ├── wikilinks.go   # [[wikilink]] parsing
│   │   ├── tags.go        # #tag parsing
│   │   └── canvas.go      # Canvas JSON
│   └── tools/           # MCP tool handlers
│       ├── read_tools.go
│       ├── write_tools.go
│       ├── graph_tools.go
│       └── tag_tools.go
└── testdata/            # Test fixtures
```

## Security

- **Path Traversal Protection**: All paths validated against vault root
- **Hidden File Protection**: Skips `.obsidian`, `.git`, and dotfiles
- **No Arbitrary Execution**: Pure file system operations only

## Performance

- **In-Memory Cache**: 60-second TTL for frequently accessed files
- **Efficient Scanning**: Uses `filepath.Walk` with early termination
- **Zero Allocations**: Optimized for large vaults (1000+ notes)

## Compatibility

- **Obsidian Version**: Compatible with Obsidian 1.4+
- **Frontmatter**: YAML between `---` delimiters
- **Wikilinks**: Supports `[[Page]]`, `[[Page|Alias]]`, `[[Page#Section]]`
- **Tags**: Inline (`#tag`) and frontmatter (`tags: [tag1, tag2]`)
- **Canvas**: JSON format (`.canvas` files)

## Development

### Requirements

- Go 1.22+
- Make (optional)

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

We welcome contributions from the community! Please follow this workflow:

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
- Discussions: [GitHub Discussions](https://github.com/nemesis312/obsidian-mcp/discussions)

---

**Note**: This is a filesystem-based MCP server. It does not require the Obsidian app to be running and does not access Obsidian's internal APIs.
