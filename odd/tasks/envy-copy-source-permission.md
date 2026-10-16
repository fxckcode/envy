# Envy env_copy source-read authorization

## Objective
Close the source-read permission bypass in the MCP `env_copy` operation without changing human CLI copy semantics.

## Problem and rationale
Independent verification found `Service.Copy` checks the target's write permission, then calls `Project.CopyKey`; that method reads the source value and writes it to the destination. Without source `ReadValues`, an agent can copy a denied value into an environment it may read. The PRD defines per-environment `READ VALUES` and `WRITE VALUES` as separate permissions (`PRD.md:397-410`). Matt and Ponytail both recommend requiring source `ReadValues` while preserving the existing target write/protected checks. No source Metadata permission is required unless the MCP response or approval prompt exposes source metadata.

## Scope authorized
- `internal/mcpapi/service.go`
- `internal/mcpapi/permissions_test.go`
- `internal/mcpapi/acceptance_test.go` — owner-authorized update of scenarios 3, 5, 12, and 13 to reflect the PRD's ReadValues contract; retain denial and allowed-path coverage.
- `odd/tasks/envy-copy-source-permission.md`
- Do not modify `Project.CopyKey` or human CLI copy semantics unless caller inspection proves that is the only safe shared boundary; if so, stop and request revised scope.
- No push, PR, release, or unrelated changes.

## Tasks
- [x] ECP1 — Require source ReadValues before MCP env_copy can read or transfer a value; add regression tests.

## Acceptance criteria
- Source `ReadValues=false` denies the MCP copy before calling `Project.CopyKey`, mutation, or approval side effects; denial is audited.
- Source `ReadValues=true` preserves the current target Write and protected-environment rules.
- Tests prove denied source access leaves the target value unchanged, queues no approval, and records denial; a permitted copy still succeeds.
- Tests run using the already-installed Go 1.23.12 toolchain and public modules from `https://proxy.golang.org` only, without credentials. No direct VCS fallback or other network source.

## Routing and risk
- ECP1: delegated direct to one writer because this is an access-control behavior change with tests; high verification tier. Require writer self-verification and an independent verifier, then parent spot check.
- Forecast: approximately 35 authored lines. No push or PR is authorized.

## Verification / progress
- Base branch `fxckcode/mcp-permission-hardening` contains uncommitted broader Envy permission/audit changes; preserve them.
- Final independent verifier for the broader task found this distinct source-read gap. No fix has been made yet.
- Matt and Ponytail independently recommend checking source ReadValues before any copy side effect and retaining target Write checks.
- Dependencies were previously unavailable in cache. User has now explicitly authorized anonymous downloads from `proxy.golang.org` for verification; no other network access is authorized.
- Engram mirror pending because the store does not resolve Envy; do not guess a project key.
- ECP1 implementation is in `internal/mcpapi/service.go`; regression tests in `internal/mcpapi/permissions_test.go` verify source denial/audit/no mutation/no approval and allowed copy with source ReadValues but without source Metadata.
- RED confirmed the source-read bypass: the denied-source test initially copied successfully. The follow-up now checks source ReadValues before Project.CopyKey; only the MCP service caller exists, so human CLI copy semantics remain unchanged.
- Owner authorized updating `internal/mcpapi/acceptance_test.go` to configure ReadValues for successful scenarios 3, 5, 12, and 13 while preserving separate denied-path tests.
- Focused and full tests pass offline with cached dependencies. Final independent verifier confirmed source permission, target checks, audit, acceptance cases, and E3 descriptions; `gofmt -d` and `git diff --check` pass.
- Dependencies were downloaded only through `https://proxy.golang.org`; all post-cache test attempts used `GOPROXY=off`.
- ECP1 was included in local permission commit `5d95d5438a56951f4a71802d9220599d070a87d2`.
- No push or PR was made.
