# Envy MCP permission and audit hardening

## Objective
Align Envy's MCP read behavior with the explicit `READ METADATA` / `READ VALUES` split in `PRD.md`, and make the README honest about audit persistence failures.

## Problem and rationale
`PRD.md:397-410` defaults `READ METADATA` on and `READ VALUES` off. The service currently returns non-secret values from `List` and `Metadata` while `ReadValue` denies values when `read_values` is off; `Diff` returns values without checking permissions. Metadata checks also exist on only two service methods. Separately, the README promises audit rows while persistence failures are silently discarded. Preserve non-blocking operation semantics on audit failure to avoid ambiguous mutation retries; make the failure observable and the documented guarantee accurate.

## Scope authorized
- `internal/mcpapi/service.go`
- `internal/mcpapi/*_test.go`
- `internal/project/project.go`
- `internal/project/*_test.go`
- `README.md`
- `internal/mcpserver/server.go` — owner-authorized correction of permission-dependent tool descriptions.
- `odd/tasks/mcp-permission-hardening.md`
- No GitHub API, push, PR, or release. Dependency downloads were authorized only from `proxy.golang.org` for Envy verification.

## Tasks
- [x] E1 — Enforce the per-environment metadata and value permissions across every MCP data-bearing method, including schema/defaults, generated examples, Doctor, and Diff; add regression tests.
- [ ] E2 — Surface audit persistence errors as best-effort diagnostics and make README wording accurate; add a focused test.
- [x] E3 — Correct MCP tool descriptions and response comments for all `ReadValues`-dependent value-bearing responses.

## Acceptance criteria
- Metadata-only access exposes no value payload from any MCP method when `ReadValues=false`: list/metadata values, diff values, schema defaults/enumerated values, and generated-example defaults must be omitted or redacted. `ReadValue` remains denied in that case. Values appear only when `ReadValues=true`.
- `Metadata=false` never exposes that environment's names/schema/status/existence/doctor/diff metadata. `ListEnvironments` must not reveal denied environment names; global schema/example/doctor output must fail closed if any configured environment is denied, with an explicit denial result rather than an empty result that looks successful. `Diff` must not return values without ReadValues and must require metadata permission for both environments.
- Secret values remain redacted unless ReadValues permits them.
- Audit write failures are visible in local diagnostics/logs but do not turn an already-applied mutation into an error; README says audit persistence is attempted/best-effort, not guaranteed.
- MCP tool descriptions and response comments for list/metadata/diff/schema/example methods must state that value payloads depend on `ReadValues`; they must not promise unconditional redaction where grants can return plaintext.
- Tests are added before behavior changes. Use only offline commands with `GOTOOLCHAIN=go1.23.12 GOPROXY=off GOSUMDB=off`; no dependency-network fallback. Go 1.23.12 was installed into the user's Go toolchain cache after explicit user authorization. If dependencies are not cached, report tests unavailable; do not download modules.

## Routing and risk
- E1 and E2 are delegated direct to one writer because they span multiple executable and test files; high risk due access-control behavior. A fresh independent verifier must inspect the diff. Parent runs a final bounded spot check.
- Forecast: approximately 120 authored lines; delivery strategy `single-pr` only if later explicitly requested. No push or PR is authorized now.

## Verification / progress
- Base: `c56e820f2a79397965562ced9e72adeb684eaca1`; clean `main` at clone time.
- CodeGraph was unavailable (no upstream executable); mapped via bounded source inspection.
- Matt and Ponytail independently recommended the PRD split: Metadata controls metadata, ReadValues controls every value payload; audit persistence should remain best-effort but observable; test permissions per read surface.
- No project-specific `.atl/skill-registry.md`; global `auditing-repo-claims`, `go-testing`, `test-driven-development`, and `work-unit-commits` skills apply.
- Go 1.23.12 was installed into the per-user toolchain cache after explicit owner authorization. Required modules were downloaded only from `https://proxy.golang.org`; all subsequent tests were offline.
- E1 completed: metadata gates and ReadValues filtering cover List, Metadata, ListEnvironments, GetSchema, Check, Diff, Exists, ReadValue, GenerateExample, and Doctor; the separately authorized `env_copy` follow-up enforces source ReadValues before transfer.
- E2 completed: persistence failures are logged but remain best-effort; README states the accurate contract. Audit failure does not turn an already-applied mutation into an error.
- E3 completed: MCP descriptions and response-type comments no longer promise unconditional secret redaction. The reviewer noted global schema/example values require ReadValues in every configured environment; descriptions state the permission dependency but do not spell out the all-environment rule.
- Acceptance scenarios 3, 5, 12, and 13 explicitly grant ReadValues when asserting value/default output. Final independent verifier verdict: PASS for permission/audit behavior and E3 claims.
- Full offline suite passed: `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /home/fxckcode/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.23.12.linux-amd64/bin/go test ./...`; `gofmt -d` and `git diff --check` passed.
- Engram mirror pending because the memory store does not resolve these repositories; do not guess a project key.
- Next: create a local permission work-unit commit for E1/E3/ECP1, then a separate E2 audit/README work-unit commit. No push or PR authorized.
