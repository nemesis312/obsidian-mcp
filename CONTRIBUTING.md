# Contributing to obsidian-mcp

Thanks for your interest. Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first.

## Development setup

Requirements: Go 1.26.1+ (see `go.mod`) and Make.

```bash
git clone https://github.com/nemesis312/obsidian-mcp.git
cd obsidian-mcp
make build   # builds ./obsidian-mcp
make test    # go test -v -race ./...
make lint    # go vet ./... and go fmt ./...
```

CI (`.github/workflows/ci.yml`) runs vet, gofmt and race tests on pull requests.

## Workflow

1. Fork the repository and branch from `develop`:
   ```bash
   git checkout develop
   git checkout -b feat/your-feature
   ```
2. Make your change and add or update tests. Pull requests without tests for new behavior will not be merged.
3. Run `make lint` and `make test`.
4. Open a pull request against `develop`. PRs targeting `master` are not accepted; the maintainer merges `develop` into `master` for releases.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`, `refactor:`. Release notes are generated from these prefixes.

## Important: stdout is reserved for MCP

The server speaks the MCP protocol over stdout. Never write anything else to stdout (no `fmt.Println`, no stray logging). Write diagnostics to stderr, for example `fmt.Fprintf(os.Stderr, ...)`.

## Security

Do not open public issues for vulnerabilities. See [SECURITY.md](SECURITY.md).
