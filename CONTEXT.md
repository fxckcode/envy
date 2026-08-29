# Envy — Domain Glossary

## Terms

- **Environment**: A named configuration target (e.g. development, staging, production) backed by a file or external provider.
- **Variable**: A key/value pair belonging to an environment. May be marked secret.
- **Secret**: A variable whose value must be masked in all human surfaces by default.
- **Schema**: Declarative rules for keys (required, type, secret) in `envy.yaml`.
- **Compare matrix**: Presence/difference grid across environments; never shows secret cleartext.
- **Diff listing**: CLI comparison of two environments; secrets masked, non-secret differing values shown in cleartext.
- **Validation**: Schema check producing missing and invalid key findings without revealing secrets.
- **Doctor**: Project health scan (required keys, duplicates, gitignore coverage, public secret leaks) with a numeric score.
- **Provider metadata**: Source description (file path or provider path) shown without values.
- **Audit entry**: Record of an actor/action/key/environment/time/result with redacted values, persisted under `.envy/audit.jsonl`.
- **Approval request**: Agent-proposed write to a protected target awaiting Allow once / Allow for project / Deny.
- **Protected environment**: Environment (typically production) that blocks edits without elevated trust.
- **Elevated trust**: Explicit authorization flag required to mutate protected environments.
- **Agent grant**: Temporary permission grant for an agent identity (write/delete/read-secrets) with TTL; read-secrets denied by default. Agent mutations use `--as-agent` and are denied without an active grant.
- **Status footer**: Counts of variables, missing required keys, and secrets-hidden indicator.
- **CI mode**: Non-interactive check that fails the process on missing/invalid/leaked/policy violations.

## Seams under test

1. `internal/project.Project` — domain operations (load, mutate, compare, validate, doctor, import/export, run, agent grants).
2. `internal/cli.Execute` — scriptable CLI presentation over Project (list/check/diff/doctor/run/get/set/delete/import/export/agent).
3. `internal/tui.Model` — keyboard-driven view state via Bubble Tea `Update`/`View`.
