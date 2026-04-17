# Installing obsidian-mcp

This guide covers how to install and connect `obsidian-mcp` with three MCP clients:
- [Claude Code CLI](#claude-code-cli)
- [Claude Desktop](#claude-desktop)
- [VS Code with GitHub Copilot](#vs-code-with-github-copilot)

---

## Step 1 — Get the Binary

### Option A: Download a prebuilt release (recommended)

Go to the [releases page](https://github.com/mark3labs/obsidian-mcp/releases) and download the archive for your platform:

| Platform | File |
|---|---|
| macOS (Apple Silicon) | `obsidian-mcp_darwin_arm64.tar.gz` |
| macOS (Intel) | `obsidian-mcp_darwin_amd64.tar.gz` |
| Linux x86-64 | `obsidian-mcp_linux_amd64.tar.gz` |
| Linux ARM64 | `obsidian-mcp_linux_arm64.tar.gz` |
| Windows x86-64 | `obsidian-mcp_windows_amd64.zip` |

Extract and move the binary to somewhere on your `$PATH`:

```bash
# macOS / Linux
tar -xzf obsidian-mcp_darwin_arm64.tar.gz
sudo mv obsidian-mcp /usr/local/bin/
chmod +x /usr/local/bin/obsidian-mcp

# Verify
obsidian-mcp --version
```

### Option B: Build from source (requires Go 1.22+)

```bash
go install github.com/mark3labs/obsidian-mcp/cmd/obsidian-mcp@latest
```

The binary lands in `$(go env GOPATH)/bin/obsidian-mcp`. Make sure that directory is on your `$PATH`.

### Option C: Build locally

```bash
git clone https://github.com/mark3labs/obsidian-mcp.git
cd obsidian-mcp
make build
# binary is at ./obsidian-mcp
```

---

## Step 2 — Find Your Vault Path

You need the absolute path to your Obsidian vault folder (the directory that contains your `.obsidian/` config folder).

```bash
# Example paths
/Users/yourname/Documents/MyVault          # macOS
/home/yourname/notes                       # Linux
C:\Users\yourname\Documents\MyVault        # Windows
```

The server auto-detects the vault on startup if you skip this, but providing it explicitly is safer.

---

## Claude Code CLI

### Quick start (one-off)

```bash
OBSIDIAN_VAULT_PATH="/path/to/your/vault" obsidian-mcp
```

### Add as a persistent MCP server

Claude Code stores MCP servers in `.claude/settings.json` (project-level) or `~/.claude/settings.json` (global).

**Via CLI (recommended):**

```bash
claude mcp add obsidian /usr/local/bin/obsidian-mcp -- --vault "/path/to/your/vault"
```

**Or manually** — open `~/.claude/settings.json` and add:

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "/usr/local/bin/obsidian-mcp",
      "args": ["--vault", "/path/to/your/vault"]
    }
  }
}
```

**Using an environment variable instead of a flag:**

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "/usr/local/bin/obsidian-mcp",
      "env": {
        "OBSIDIAN_VAULT_PATH": "/path/to/your/vault"
      }
    }
  }
}
```

### Verify it works

```bash
claude mcp list
# Should show: obsidian  /usr/local/bin/obsidian-mcp
```

Start a session and test a tool:

```
> list the files in my vault
```

---

## Claude Desktop

### Config file location

| OS | Path |
|---|---|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
| Linux | `~/.config/Claude/claude_desktop_config.json` |

Create the file if it does not exist.

### Configuration

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "/usr/local/bin/obsidian-mcp",
      "args": ["--vault", "/path/to/your/vault"]
    }
  }
}
```

**Windows** — use double backslashes or forward slashes:

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "C:/Users/yourname/bin/obsidian-mcp.exe",
      "args": ["--vault", "C:/Users/yourname/Documents/MyVault"]
    }
  }
}
```

**Multiple vaults** — give each server a distinct key:

```json
{
  "mcpServers": {
    "obsidian-work": {
      "command": "/usr/local/bin/obsidian-mcp",
      "args": ["--vault", "/path/to/work-vault"]
    },
    "obsidian-personal": {
      "command": "/usr/local/bin/obsidian-mcp",
      "args": ["--vault", "/path/to/personal-vault"]
    }
  }
}
```

### Restart and verify

1. Quit Claude Desktop completely (`Cmd+Q` / `Alt+F4`)
2. Reopen it
3. Look for the hammer icon (🔨) in the chat input — click it to see available tools
4. You should see tools like `get_note`, `search_vault`, `list_vault_files`, etc.

---

## VS Code with GitHub Copilot

VS Code supports MCP servers through the Copilot agent panel (requires VS Code 1.99+ and a GitHub Copilot subscription).

### Configuration

Open your VS Code `settings.json` (`Cmd+Shift+P` → "Open User Settings (JSON)") and add:

```json
{
  "github.copilot.chat.mcp.servers": {
    "obsidian": {
      "command": "/usr/local/bin/obsidian-mcp",
      "args": ["--vault", "/path/to/your/vault"],
      "type": "stdio"
    }
  }
}
```

**Windows:**

```json
{
  "github.copilot.chat.mcp.servers": {
    "obsidian": {
      "command": "C:/Users/yourname/bin/obsidian-mcp.exe",
      "args": ["--vault", "C:/Users/yourname/Documents/MyVault"],
      "type": "stdio"
    }
  }
}
```

### Workspace-level config (share with your team)

Create `.vscode/mcp.json` in the repo root:

```json
{
  "servers": {
    "obsidian": {
      "command": "obsidian-mcp",
      "args": ["--vault", "${env:OBSIDIAN_VAULT_PATH}"],
      "type": "stdio"
    }
  }
}
```

This reads the vault path from the `OBSIDIAN_VAULT_PATH` environment variable so each developer sets their own path without touching the shared file.

### Verify it works

1. Open the Copilot Chat panel (`Ctrl+Shift+I` / `Cmd+Shift+I`)
2. Switch to **Agent** mode (dropdown in the chat panel)
3. Click the **Tools** icon — `obsidian` should appear in the list
4. Ask: `@obsidian list the files in my vault`

---

## Optional Flags

| Flag | Default | Description |
|---|---|---|
| `--vault` | auto-detect | Absolute path to the Obsidian vault |
| `--transport` | `stdio` | `stdio` or `http` |
| `--port` | `8080` | Port when `--transport=http` |
| `--cache-ttl` | `60` | Cache TTL in seconds |
| `--no-cache` | off | Disable in-memory cache |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |

---

## Vault Auto-Detection

If you omit `--vault`, the server looks in this order:

1. `OBSIDIAN_VAULT_PATH` environment variable
2. Obsidian's own config file (picks the most recently opened vault):
   - macOS: `~/Library/Application Support/obsidian/obsidian.json`
   - Linux: `~/.config/obsidian/obsidian.json`
   - Windows: `%APPDATA%/obsidian/obsidian.json`
3. Exits with an error if no vault is found

---

## Troubleshooting

**Binary not found**
Ensure the binary is on your `$PATH`. Run `which obsidian-mcp` (macOS/Linux) or `where obsidian-mcp` (Windows) to confirm.

**macOS Gatekeeper blocks the binary**
```bash
xattr -d com.apple.quarantine /usr/local/bin/obsidian-mcp
```

**"vault not found" error**
Pass `--vault` explicitly with the absolute path to the folder that contains `.obsidian/`.

**Tools not appearing in Claude Desktop**
Check that the config JSON is valid (no trailing commas). Use `python3 -m json.tool claude_desktop_config.json` to validate.

**Permission denied on vault files**
The server only reads and writes files inside the vault root. If it rejects a path, verify there are no symlinks pointing outside the vault.

---

## Security Notes

- All file paths are validated against the vault root — `../` traversal attempts are rejected
- Writes to `.obsidian/` and `.git/` are blocked
- The server does not execute any code from note contents
- No network access is made unless you run `--transport=http`
