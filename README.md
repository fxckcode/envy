# Envy

Secure environment management for humans, CI, and AI agents — TUI + CLI over a shared core.
Browse environments, edit variables, compare diffs, validate health, and grant temporary agent
access without exposing secrets by default.

## Requirements

- Go 1.23+

## Release usage

Install a released version with Go, then run `envy` from a project directory
that contains `envy.yaml`:

```bash
go install github.com/fxckcode/envy/cmd/envy@latest
envy
```

For the MCP server used by AI agents:

```bash
go install github.com/fxckcode/envy/cmd/envy-mcp@latest
ENVY_ROOT=. ENVY_AGENT=default envy-mcp
```

## Quick start from source

```bash
# Interactive TUI (from a project directory containing envy.yaml)
go run ./cmd/envy

# CLI
go run ./cmd/envy list
go run ./cmd/envy check --env development
go run ./cmd/envy check --env production --ci
go run ./cmd/envy doctor
go run ./cmd/envy diff staging production
go run ./cmd/envy get REDIS_URL
go run ./cmd/envy set REDIS_URL redis://localhost:6379 --env development
go run ./cmd/envy set REDIS_URL redis://localhost:6379 --env development --as-agent claude-code
go run ./cmd/envy delete REDIS_URL --env development
go run ./cmd/envy import .env --env development
go run ./cmd/envy export production
go run ./cmd/envy export production --reveal
go run ./cmd/envy run development -- npm run dev
go run ./cmd/envy agent grant claude-code --env development --write --ttl 30m
go run ./cmd/envy agent revoke claude-code
go run ./cmd/envy agent session start claude-code --env development --ttl 30m
go run ./cmd/envy schema generate
go run ./cmd/envy example generate
go run ./cmd/envy hooks install

# MCP server (stdio) for AI agents
ENVY_ROOT=. ENVY_AGENT=default go run ./cmd/envy-mcp
```

## Agent skill

Reusable rules for coding agents live in `skills/env-management/SKILL.md`.


## Tests

```bash
gofmt -l $(find . -name '*.go' -not -path './.git/*')
go vet ./...
go test ./...
go build ./...
```

## MCP tools

Agent-facing tools (`env_list`, `env_check`, `env_diff`, `env_metadata`, `env_set`, …) redact secrets as `[REDACTED]`, enforce least-privilege agent permissions from `envy.yaml`, and write audit rows under `.envy/audit.jsonl`.

## Keyboard (TUI)

| Key | Action |
|-----|--------|
| `a` | Add variable |
| `e` | Edit variable |
| `x` | Delete variable (confirm with `y`) |
| `c` | Compare environments |
| `v` | Run validations (status area) |
| `p` | View providers |
| `g` | Agent activity |
| `A` | Open pending agent approval |
| `q` | Quit |
| `tab` | Cycle focus |

## Configuration

Envy reads project configuration from `envy.yaml` and discovers local environment files without printing their values. Use `.env.example` for shareable placeholders and keep real `.env*` files out of version control. The `check`, `diff`, and `doctor` commands report metadata and validation results with sensitive values redacted.

## Contributing

1. Fork the repository and create a focused branch.
2. Make the smallest change that addresses the problem.
3. Run `gofmt`, `go vet ./...`, `go test ./...`, and `go build ./...`.
4. Open a pull request describing the behavior change and verification performed.

Please do not include credentials, production environment values, private configuration, or generated `.envy/` data in commits or issue reports.

## License

Envy is released under the [MIT License](LICENSE).
