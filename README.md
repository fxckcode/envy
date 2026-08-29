# Envy

Secure environment management for humans, CI, and AI agents — TUI + CLI over a shared core.
Browse environments, edit variables, compare diffs, validate health, and grant temporary agent
access without exposing secrets by default.

## Requirements

- Go 1.22+

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
```

## Tests

```bash
go test ./...
```

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
| `q` | Quit |
| `tab` | Cycle focus |
