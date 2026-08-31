# Envy

Secure environment management for humans, CI, and AI agents — TUI + CLI over a shared core.
Browse environments, edit variables, compare diffs, validate health, and grant temporary agent
access without exposing secrets by default.

## Requirements

- Go 1.23+

## Quick start

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
go test ./...
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
