# Security Policy

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub private vulnerability reporting: go to the repository's **Security** tab and choose **Report a vulnerability**.

Include the affected version (`obsidian-mcp --version`), reproduction steps, and the impact you observed. We will acknowledge the report and keep you updated in the private advisory thread.

## Threat model summary

obsidian-mcp is a local MCP server with filesystem access limited to a single vault.

- Every path is validated against the vault root. Paths that escape the vault, including via symlinks, are rejected.
- `.git/` and `.obsidian/` are blocked.
- Write tools (`create_note`, `append_to_note`, `patch_note`, `update_frontmatter`, `delete_note`) only operate on `.md` files.
- The server communicates over stdio and opens no network listener.
- The server does not execute note contents.

Out of scope: anything the MCP client or the connected AI model does with data it legitimately reads from the vault, and access to files outside the vault granted by other tools on your machine. Note that the server can modify and delete notes in the vault by design; keep backups or use version control.
