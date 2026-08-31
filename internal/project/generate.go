package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fxckcode/envy/internal/config"
	"github.com/fxckcode/envy/internal/envfile"
	"gopkg.in/yaml.v3"
)

var secretNameHint = regexp.MustCompile(`(?i)(SECRET|PASSWORD|TOKEN|PRIVATE|CREDENTIAL|API[_-]?KEY|_KEY$|PASSWD)`)

// GenerateSchema inspects env files (and any existing schema) and writes a
// schema section to envy.yaml. Secret values are never embedded.
func (p *Project) GenerateSchema() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	discovered := map[string]config.SchemaField{}
	for envName, vars := range p.envs {
		_ = envName
		for _, key := range vars.Keys() {
			field := discovered[key]
			val, _ := vars.Get(key)
			inferred := inferSchemaField(key, val)
			field = mergeSchemaField(field, inferred)
			discovered[key] = field
		}
	}
	// Preserve explicit existing schema markers where present.
	for key, existing := range p.cfg.Schema {
		merged := mergeSchemaField(discovered[key], existing)
		// An existing schema declaration is authoritative, including explicit
		// optionality (required: false).
		merged.Required = existing.Required
		discovered[key] = merged
	}
	if len(discovered) == 0 {
		return 0, fmt.Errorf("no environment keys found to generate schema")
	}

	p.cfg.Schema = discovered
	if err := saveConfigLocked(p); err != nil {
		return 0, err
	}
	p.appendAuditLocked("human", "schema_generate", "*", "", AuditOK, fmt.Sprintf("keys=%d", len(discovered)))
	return len(discovered), nil
}

func inferSchemaField(key, value string) config.SchemaField {
	// Discovery cannot establish a requirement from a key appearing in one
	// environment. Non-empty values are the useful signal; empty bindings are
	// preserved as optional until a human declares them required.
	f := config.SchemaField{Required: value != "", Type: "string"}
	lower := strings.ToLower(key)
	if secretNameHint.MatchString(key) || strings.Contains(lower, "dsn") {
		f.Secret = true
	}
	switch {
	case looksLikeURL(value):
		f.Type = "url"
		if strings.Contains(value, "://") && (strings.Contains(value, "@") || strings.HasPrefix(value, "postgres") || strings.HasPrefix(value, "mysql") || strings.HasPrefix(value, "redis")) {
			f.Secret = true
		}
	case value == "true" || value == "false":
		f.Type = "boolean"
	case isAllDigits(value) && value != "":
		f.Type = "integer"
	}
	// Never persist live values into schema defaults for secrets.
	if !f.Secret && value != "" && f.Type == "integer" {
		f.Default = value
	}
	if !f.Secret && f.Type == "boolean" {
		f.Default = value
	}
	return f
}

func mergeSchemaField(base, extra config.SchemaField) config.SchemaField {
	out := base
	if extra.Required {
		out.Required = true
	}
	if extra.Secret {
		out.Secret = true
	}
	if extra.Type != "" {
		out.Type = extra.Type
	}
	if extra.Default != "" && !out.Secret {
		out.Default = extra.Default
	}
	if len(extra.Values) > 0 {
		out.Values = append([]string{}, extra.Values...)
	}
	if extra.Min != nil {
		out.Min = extra.Min
	}
	if extra.Max != nil {
		out.Max = extra.Max
	}
	if out.Type == "" {
		out.Type = "string"
	}
	return out
}

func looksLikeURL(v string) bool {
	return strings.Contains(v, "://") || strings.HasPrefix(v, "http")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func saveConfigLocked(p *Project) error {
	path := filepath.Join(p.root, "envy.yaml")
	data, err := yaml.Marshal(p.cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteExampleFile writes .env.example from schema defaults/empties (never live secrets).
func (p *Project) WriteExampleFile() (string, error) {
	payload := p.GenerateExample()
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, envfile.FormatValue(payload[k]))
	}
	path := filepath.Join(p.Root(), ".env.example")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	p.AppendAudit("human", "example_generate", "*", "", AuditOK, filepath.Base(path))
	return path, nil
}

const preCommitHook = `#!/bin/sh
# Envy pre-commit: block accidental .env staging and secret leaks in public files.
set -e
staged=$(git diff --cached --name-only --diff-filter=ACMR)
fail=0
for f in $staged; do
  base=$(basename "$f")
  case "$base" in
    .env.example)
      ;;
    .env|.env.*)
      echo "envy: refusing to commit $f (environment secrets file)" >&2
      fail=1
      ;;
  esac
  case "$base" in
    .env.example|README.md|README|*.md)
      if git diff --cached -U0 -- "$f" | grep -Eiq '^\+.*(postgres://[^ ]+@|mysql://[^ ]+@|AKIA[0-9A-Z]{16}|sk_live_|sk_test_[A-Za-z0-9]{8,})'; then
        echo "envy: refusing to commit possible secret material in $f" >&2
        fail=1
      fi
      ;;
  esac
done
if [ "$fail" -ne 0 ]; then
  exit 1
fi
exit 0
`

// InstallHooks writes an executable git pre-commit hook under .git/hooks.
func (p *Project) InstallHooks() (string, error) {
	gitDir := filepath.Join(p.Root(), ".git")
	info, err := os.Stat(gitDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a git repository (.git missing)")
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(gitDir, "hooks", "pre-commit")
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("pre-commit hook already exists; refusing to overwrite %s", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check existing pre-commit hook: %w", err)
	}
	if err := os.WriteFile(path, []byte(preCommitHook), 0o755); err != nil {
		return "", err
	}
	p.AppendAudit("human", "hooks_install", "*", "", AuditOK, "pre-commit")
	return path, nil
}
