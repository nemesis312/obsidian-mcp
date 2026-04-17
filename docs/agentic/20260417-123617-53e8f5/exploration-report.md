---
id: 20260417-123617-53e8f5-explore
title: "Exploration Report - Build a production-ready MCP server for Obsidian vaults in Go - zero-dependency single binary with 20 tools for knowledge graph operations (read, write, graph/links, tags, canvas)"
project: "obsidian-mcp"
run_id: 20260417-123617-53e8f5
step: explore
status: draft
tags:
  - agentic
  - feature
  - explore
source: bc-agentic
created_at: 2026-04-17T12:36:17Z
updated_at: 2026-04-17T12:36:17Z
---

# Exploration Report

Run: 20260417-123617-53e8f5
Goal: Build a production-ready MCP server for Obsidian vaults in Go - zero-dependency single binary with 20 tools for knowledge graph operations (read, write, graph/links, tags, canvas)
Mode: safe

---

## 1. Current State

### Project Context
This is a **greenfield project** to build a production-ready MCP server for Obsidian vaults in Go. The target is a zero-dependency single binary that treats Obsidian vaults as first-class knowledge graphs with 20 tools across read, write, graph/links, tags, and canvas operations.

### Existing Obsidian MCP Implementations (Competition Analysis)

Five major implementations exist, split into two architectural camps:

**REST API-Based Servers (Require Obsidian Running)**
1. **mcp-obsidian** (MarkusPfundstein) - Uses Obsidian Local REST API plugin. Tools: list_files, get_contents, search, patch, append, delete
2. **obsidian-mcp-server** (cyanheads) - Comprehensive suite via REST API for notes, tags, frontmatter management

**Filesystem-Based Servers (Direct Markdown Access)**
3. **mcpvault** (bitbonsai) - Lightweight with 14 methods, zero dependencies, works with any vault structure
4. **obsidian-mcp** (StevenStavrakis) - Direct file operations for reading, creating, editing notes and tags

**Plugin-Based**
5. **Semantic MCP** - Runs inside Obsidian as native plugin with access to internal APIs, knowledge graph, Dataview queries

**Key Finding**: All existing implementations are in **Node.js/TypeScript**. **No production-ready Go implementation exists** as of April 2026.

### Go MCP SDK Landscape

**Official SDK** (Recommended)
- **github.com/modelcontextprotocol/go-sdk** - Official Go SDK maintained in collaboration with Google
- Status: Implements full MCP spec but currently **unstable and subject to breaking changes**
- Supports stdio, SSE, and streamable-HTTP transports
- Provides mcp.Server, jsonrpc, and OAuth primitives

**Community Alternative**
- **github.com/mark3labs/mcp-go** - Community implementation by Mark3Labs
- Implements MCP spec version 2025-11-25 with backward compatibility
- Supports stdio, SSE, and streamable-HTTP transports
- Status: Under active development, core features working
- Has mature documentation at mcp-go.dev

**Recommendation**: Use **github.com/mark3labs/mcp-go** as originally specified. While the official SDK exists, mark3labs/mcp-go has more mature documentation and is the most widely used community Go SDK as of 2026.

## 2. Existing Components

### Available Libraries and Tools

**Markdown/Frontmatter Parsing**
- **gopkg.in/yaml.v3** - Standard YAML parser for frontmatter (stable, production-ready)
- Obsidian frontmatter format: YAML between `---` delimiters
- Special properties: tags, aliases, cssclasses
- **Caveat**: Wikilinks in frontmatter must be quoted as of Obsidian 1.4: `parent: "[[My Note]]"`

**Wikilink Format**
- Pattern: `\[\[([^\[\]|#]+)(?:#[^\[\]|]*)?(|[^\[\]]*)?\]\]`
- Supports aliases: `[[Note Name|Display Text]]`
- Supports heading refs: `[[Note#Heading]]`
- Inline tags: `#tagname` (lowercase normalized)
- Frontmatter tags: `tags: [tag1, tag2]` or `tags:\n  - tag1`

**Canvas Files**
- `.canvas` files are JSON format
- Structure: nodes (text, file, web, group) + edges (connections)
- Standard Go `encoding/json` sufficient for parsing

**Build/Release Tools**
- **GoReleaser** - Mature cross-platform binary release tool
- Supports: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- macOS Universal Binaries (fat binaries with arm64 + amd64 in single file)
- Single `.goreleaser.yml` config for 40+ platform combinations

**Vault Detection**
- Obsidian config locations:
  - macOS: `~/Library/Application Support/obsidian/obsidian.json`
  - Linux: `~/.config/obsidian/obsidian.json`
  - Windows: `%APPDATA%/obsidian/obsidian.json`
- Config contains vault paths and most recently opened vault

### Repository Structure (Proposed)
Following Go best practices:
- `cmd/` - Entry point and CLI
- `internal/vault/` - Vault operations, caching, security
- `internal/markdown/` - Frontmatter, wikilinks, tags, canvas parsing
- `internal/tools/` - MCP tool handlers (read, write, graph, tags)
- `server/` - MCP server setup and transport

## 3. Gaps and Risks

### Technical Risks

**1. MCP SDK Stability**
- Official Go SDK is **unstable and subject to breaking changes**
- mark3labs/mcp-go is under active development
- **Mitigation**: Pin to specific version, prepare for updates, use mark3labs SDK for better docs

**2. Obsidian Format Evolution**
- Wikilink format changed in Obsidian 1.4 (quoted in frontmatter)
- Future format changes could break parsing
- **Mitigation**: Implement defensive parsing, version detection, comprehensive tests

**3. Path Traversal Security**
- Critical: Must prevent `../` escapes from vault root
- **Mitigation**: Mandatory security layer in `internal/vault/security.go` with absolute path resolution

**4. Concurrent File Access**
- Users may edit vault while MCP server is running
- Cache invalidation becomes critical
- **Mitigation**: TTL-based cache with invalidation on writes, file watching optional

**5. Large Vault Performance**
- Full-text search across thousands of notes
- Link graph traversal
- **Mitigation**: In-memory cache, streaming results, configurable limits

### Feature Gaps vs Existing Implementations

**Missing from Current Spec** (Consider for Future)
- File watching / live reload
- Dataview query support (requires complex parsing)
- Native Obsidian plugin integration (out of scope for standalone binary)
- Incremental search (all results at once in current spec)

**Out of Scope** (Confirmed)
- GUI/web interface
- Vault synchronization
- Plugin management
- Theme support

## 4. Constraints Found

### Technical Constraints

**1. Go Version Requirements**
- Go 1.22+ required for modern generics and standard library features
- No CGo dependencies to maintain single binary portability

**2. Transport Layer**
- **stdio transport is mandatory** (Claude Desktop requirement)
- HTTP/SSE transport is optional (nice-to-have for testing)

**3. Dependencies Must Remain Minimal**
- Zero runtime dependencies (no Node.js, no Python)
- Only Go standard library + essential packages:
  - github.com/mark3labs/mcp-go (MCP protocol)
  - gopkg.in/yaml.v3 (frontmatter)
  - github.com/spf13/cobra (CLI flags, optional - could use stdlib flag)
  - github.com/gin-gonic/gin (HTTP transport, optional)

**4. Security Requirements**
- All paths must be absolute after resolution
- Reject paths outside vault root
- No writes to `.obsidian/`, `.git/` system directories
- All file operations must go through security layer

**5. Error Handling**
- Never panic - return structured MCP errors
- Standardized error types: file_not_found, path_violation, permission_denied, parse_error, vault_not_found

**6. Markdown Preservation**
- Frontmatter updates must preserve note body exactly
- No formatting changes to user content
- Round-trip safety critical

### Operational Constraints

**1. Cache Behavior**
- Default TTL: 60 seconds
- Must invalidate on any write to that path
- Background refresh goroutine for dirty entries
- Toggle via `--no-cache` flag

**2. Vault Auto-Detection Priority**
1. `--vault` CLI flag
2. `OBSIDIAN_VAULT_PATH` environment variable
3. Obsidian config file (platform-specific)
4. Fail with helpful error if none found

**3. Build/Release**
- Single binary output per platform
- Must pass `go vet ./...` with zero warnings
- Must pass `golangci-lint run` (staticcheck, errcheck, gocritic)
- Race detector clean: `go test -race ./...`

## 5. Recommendations

### Architecture Decisions

**1. Use Filesystem-Based Approach**
- **Rationale**: Zero dependencies, no plugin required, works offline
- **Trade-off**: No access to Obsidian's internal knowledge graph APIs
- **Impact**: Must implement link graph and backlinks ourselves

**2. Use mark3labs/mcp-go SDK**
- **Rationale**: More mature documentation, active community, implements full spec
- **Alternative**: Official SDK exists but is explicitly unstable
- **Impact**: Follow mark3labs versioning, contribute back if issues found

**3. Implement In-Memory Cache with TTL**
- **Rationale**: Large vaults need performance optimization
- **Trade-off**: Memory usage vs speed
- **Impact**: Configurable TTL, optional disable flag, invalidation on writes

**4. Security-First File Operations**
- **Rationale**: MCP servers have broad file access; must be sandboxed
- **Implementation**: All file ops go through `internal/vault/security.go`
- **Impact**: Every path is resolved to absolute and validated against vault root

### Implementation Priorities

**Phase 1: Foundation** (Critical Path)
1. Project scaffold with Go modules
2. Vault detection and security layer
3. Basic MCP server with stdio transport
4. First read tool (get_note) as proof of concept

**Phase 2: Read Tools** (5 tools)
- list_vault_files, get_note, search_vault, get_frontmatter, get_note_metadata
- In-memory cache implementation
- Comprehensive error handling

**Phase 3: Write Tools** (5 tools)
- create_note, append_to_note, patch_note, update_frontmatter, delete_note
- Cache invalidation on writes
- Atomic file operations

**Phase 4: Graph Tools** (5 tools)
- get_backlinks, get_outgoing_links, get_orphaned_notes, get_linked_mentions, get_link_graph
- Link graph builder and cache
- Adjacency list representation

**Phase 5: Tag and Canvas Tools** (5 tools)
- list_all_tags, get_notes_by_tag, rename_tag
- list_canvases, get_canvas
- Tag normalization

**Phase 6: Testing and Release**
- Unit tests (path traversal, frontmatter round-trip, wikilink parsing)
- Integration tests (temp vault fixtures)
- GoReleaser configuration
- README with Claude Desktop config

### Next Steps for bc-cpo (PRD Stage)

**Key Questions to Answer in PRD**:
1. Which 20 tools are mandatory for MVP? (Currently all 20 specified)
2. Should we support HTTP transport in MVP or defer to v2?
3. Cache enabled by default or opt-in?
4. Should `--vault` flag be required or rely on auto-detection?
5. Error message verbosity level (debug logs vs production-friendly)?

**Must Decide**:
- Minimum Go version (1.22 or 1.23?)
- CLI framework (spf13/cobra vs stdlib flag)
- Gin vs net/http for optional HTTP transport
- License (MIT confirmed in spec)

**Carry Forward**:
- All security constraints (path sandboxing critical)
- Zero runtime dependencies requirement
- Filesystem-based approach (no REST API)
- mark3labs/mcp-go SDK choice

---

## Sources

- [Official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk)
- [mark3labs/mcp-go GitHub](https://github.com/mark3labs/mcp-go)
- [mcp-go Documentation](https://mcp-go.dev/getting-started/)
- [mcpvault - Lightweight Obsidian MCP Server](https://github.com/bitbonsai/mcpvault)
- [mcp-obsidian - REST API-Based Implementation](https://github.com/MarkusPfundstein/mcp-obsidian)
- [cyanheads/obsidian-mcp-server](https://github.com/cyanheads/obsidian-mcp-server)
- [Obsidian Forum: MCP Servers Discussion](https://forum.obsidian.md/t/obsidian-mcp-servers-experiences-and-recommendations/99936)
- [Obsidian Wikilinks in Frontmatter](https://forum.obsidian.md/t/wikilinks-in-yaml-front-matter/10052)
- [GoReleaser Build Documentation](https://goreleaser.com/customization/builds/)
- [GoReleaser macOS Universal Binaries](https://goreleaser.com/customization/builds/universalbinaries/)
