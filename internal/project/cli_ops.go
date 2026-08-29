package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fxckcode/envy/internal/envfile"
	"github.com/fxckcode/envy/internal/redact"
)

// ErrNoConfig is returned when neither envy.yaml nor discoverable .env files exist.
var ErrNoConfig = errors.New("no envy.yaml or discoverable .env files found")

// DiffRow is one key comparison for CLI diff output.
type DiffRow struct {
	Key          string
	LeftDisplay  string
	RightDisplay string
	LeftKind     CellKind
	RightKind    CellKind
	Secret       bool
}

// DiffResult is a CLI-oriented comparison (non-secret cleartext allowed).
type DiffResult struct {
	LeftEnv  string
	RightEnv string
	Rows     []DiffRow
	Warnings []string
}

// DoctorStatus is the outcome of one doctor check.
type DoctorStatus string

const (
	DoctorPass DoctorStatus = "pass"
	DoctorFail DoctorStatus = "fail"
	DoctorWarn DoctorStatus = "warn"
)

// DoctorCheck is one health check result (never contains secret values).
type DoctorCheck struct {
	Label  string
	Status DoctorStatus
	Detail string // may name keys/paths, never secret values
}

// DoctorResult aggregates health checks and a score.
type DoctorResult struct {
	Checks []DoctorCheck
	Score  int
}

// AgentGrant describes a temporary agent permission grant.
type AgentGrant struct {
	Agent       string    `json:"agent"`
	Environment string    `json:"environment"`
	Write       bool      `json:"write"`
	Delete      bool      `json:"delete"`
	ReadSecrets bool      `json:"read_secrets"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// GrantDisplay is a safe presentation of an agent grant.
type GrantDisplay struct {
	Agent       string
	Environment string
	Metadata    bool
	ReadSecrets bool
	Write       bool
	Delete      bool
	ExpiresAt   time.Time
	ExpiresIn   time.Duration
}

// ValidateEnv runs schema validation on a named environment.
func (p *Project) ValidateEnv(env string) (ValidationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[env]; !ok {
		return ValidationResult{}, fmt.Errorf("unknown environment %q", env)
	}
	return p.validateLocked(env), nil
}

// VariablesFor returns display projections for a named environment.
func (p *Project) VariablesFor(env string) ([]VariableView, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[env]; !ok {
		return nil, fmt.Errorf("unknown environment %q", env)
	}
	return p.variablesLocked(env), nil
}

// SetVariable upserts a key in the active environment (or selected via SelectEnvironment).
func (p *Project) SetVariable(key, value string) error {
	return p.setVariableAs("human", key, value)
}

// SetVariableAsAgent upserts a key under an agent identity; requires an active write grant.
func (p *Project) SetVariableAsAgent(agent, key, value string) error {
	if strings.TrimSpace(agent) == "" {
		return fmt.Errorf("agent identity is required")
	}
	return p.setVariableAs(agent, key, value)
}

func (p *Project) setVariableAs(actor, key, value string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if err := p.authorizeMutationLocked(actor, "set", key, p.active, false); err != nil {
		return err
	}
	p.envs[p.active].Set(key, value)
	if err := p.persistLocked(p.active); err != nil {
		p.appendAuditLocked(actor, "set", key, p.active, AuditError, "persist failed")
		return err
	}
	p.appendAuditLocked(actor, "set", key, p.active, AuditOK, "")
	return nil
}

// DeleteVariable removes a key from the active environment.
func (p *Project) DeleteVariable(key string) error {
	return p.deleteVariableAs("human", key)
}

// DeleteVariableAsAgent removes a key under an agent identity; requires an active delete or write grant.
func (p *Project) DeleteVariableAsAgent(agent, key string) error {
	if strings.TrimSpace(agent) == "" {
		return fmt.Errorf("agent identity is required")
	}
	return p.deleteVariableAs(agent, key)
}

func (p *Project) deleteVariableAs(actor, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if err := p.authorizeMutationLocked(actor, "delete", key, p.active, true); err != nil {
		return err
	}
	if _, ok := p.envs[p.active].Get(key); !ok {
		return fmt.Errorf("key %q not found", key)
	}
	p.envs[p.active].Delete(key)
	if err := p.persistLocked(p.active); err != nil {
		p.appendAuditLocked(actor, "delete", key, p.active, AuditError, "persist failed")
		return err
	}
	p.appendAuditLocked(actor, "delete", key, p.active, AuditOK, "")
	return nil
}

// authorizeMutationLocked enforces protected-env and agent-grant policy, auditing denials.
// deleteOp selects delete vs write grant for agent actors.
func (p *Project) authorizeMutationLocked(actor, action, key, env string, deleteOp bool) error {
	if actor != "human" {
		g, ok := p.agentGrants[actor]
		denied := !ok || p.now().After(g.ExpiresAt) || g.Environment != env
		if !denied {
			if deleteOp {
				denied = !(g.Delete || g.Write)
			} else {
				denied = !g.Write
			}
		}
		if denied {
			p.appendAuditLocked(actor, action, key, env, AuditDenied, "no active grant")
			return fmt.Errorf("agent write denied: no active grant for %q on %q", actor, env)
		}
	}
	if err := p.guardWriteLocked(env); err != nil {
		p.appendAuditLocked(actor, action, key, env, AuditDenied, "protected")
		return err
	}
	return nil
}

// GetVariable returns a display value for a key in the active environment.
// When reveal is false, secrets are redacted.
func (p *Project) GetVariable(key string, reveal bool) (string, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, ok := p.envs[p.active].Get(key)
	if !ok {
		return "", false, fmt.Errorf("key %q not found", key)
	}
	secret := p.isSecretLocked(key)
	if secret && !reveal {
		return redact.Placeholder, true, nil
	}
	return raw, secret, nil
}

// Diff compares two environments for CLI output: secrets masked, non-secrets cleartext.
func (p *Project) Diff(left, right string) (DiffResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[left]; !ok {
		return DiffResult{}, fmt.Errorf("unknown environment %q", left)
	}
	if _, ok := p.envs[right]; !ok {
		return DiffResult{}, fmt.Errorf("unknown environment %q", right)
	}
	keySet := map[string]struct{}{}
	for _, k := range p.envs[left].Keys() {
		keySet[k] = struct{}{}
	}
	for _, k := range p.envs[right].Keys() {
		keySet[k] = struct{}{}
	}
	for k := range p.cfg.Schema {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rows []DiffRow
	var warnings []string
	for _, k := range keys {
		lv, lok := p.envs[left].Get(k)
		rv, rok := p.envs[right].Get(k)
		secret := p.isSecretLocked(k)
		row := DiffRow{Key: k, Secret: secret}
		switch {
		case lok && rok:
			if lv == rv {
				row.LeftKind, row.RightKind = CellPresent, CellPresent
				row.LeftDisplay = displayDiffValue(lv, secret, true)
				row.RightDisplay = displayDiffValue(rv, secret, true)
			} else {
				row.LeftKind, row.RightKind = CellDiff, CellDiff
				row.LeftDisplay = displayDiffValue(lv, secret, false)
				row.RightDisplay = displayDiffValue(rv, secret, false)
				warnings = append(warnings, fmt.Sprintf("⚠ %s differs between %s and %s", k, left, right))
			}
		case lok && !rok:
			row.LeftKind, row.RightKind = CellOnly, CellAbsent
			row.LeftDisplay = displayDiffValue(lv, secret, false)
			row.RightDisplay = "✗"
			warnings = append(warnings, fmt.Sprintf("⚠ %s is missing %s", right, k))
			warnings = append(warnings, fmt.Sprintf("⚠ %s exists only in %s", k, left))
		case !lok && rok:
			row.LeftKind, row.RightKind = CellAbsent, CellOnly
			row.LeftDisplay = "✗"
			row.RightDisplay = displayDiffValue(rv, secret, false)
			warnings = append(warnings, fmt.Sprintf("⚠ %s is missing %s", left, k))
			warnings = append(warnings, fmt.Sprintf("⚠ %s exists only in %s", k, right))
		default:
			row.LeftKind, row.RightKind = CellAbsent, CellAbsent
			row.LeftDisplay, row.RightDisplay = "✗", "✗"
		}
		rows = append(rows, row)
	}
	return DiffResult{
		LeftEnv:  left,
		RightEnv: right,
		Rows:     rows,
		Warnings: warnings,
	}, nil
}

func displayDiffValue(value string, secret, same bool) string {
	if secret {
		if same {
			return "✓"
		}
		return redact.Mask()
	}
	if same {
		return "✓"
	}
	return value
}

// Doctor runs project health checks without revealing secret values.
func (p *Project) Doctor() DoctorResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doctorLocked()
}

func (p *Project) gitignoreCheckLocked() DoctorCheck {
	label := ".env is ignored by Git"
	gitDir := filepath.Join(p.root, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return DoctorCheck{Label: label, Status: DoctorWarn, Detail: "Not a git repository"}
	}
	giPath := filepath.Join(p.root, ".gitignore")
	data, err := os.ReadFile(giPath)
	if err != nil {
		return DoctorCheck{Label: label, Status: DoctorFail, Detail: ".gitignore missing"}
	}
	content := string(data)
	if gitignoresEnv(content) {
		return DoctorCheck{Label: label, Status: DoctorPass}
	}
	return DoctorCheck{Label: label, Status: DoctorFail, Detail: ".env not covered by .gitignore"}
}

func gitignoresEnv(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == ".env" || line == ".env*" || line == ".env.*" || line == "*" {
			return true
		}
	}
	return false
}

func (p *Project) publicLeakChecksLocked() []DoctorCheck {
	examplePath := filepath.Join(p.root, ".env.example")
	label := "Secrets not present in public files"
	vars, err := envfile.ParseFile(examplePath)
	if err != nil || vars.Len() == 0 {
		// No example file or empty — pass.
		if _, statErr := os.Stat(examplePath); os.IsNotExist(statErr) {
			return []DoctorCheck{{Label: label, Status: DoctorPass, Detail: "no .env.example"}}
		}
		return []DoctorCheck{{Label: label, Status: DoctorPass}}
	}
	var leaked []string
	for _, k := range vars.Keys() {
		val, ok := vars.Get(k)
		if !ok || val == "" {
			continue
		}
		if p.isSecretLocked(k) || looksLikeSecretValue(val) {
			leaked = append(leaked, k)
		}
	}
	if len(leaked) == 0 {
		return []DoctorCheck{{Label: label, Status: DoctorPass}}
	}
	out := make([]DoctorCheck, 0, len(leaked))
	for _, k := range leaked {
		out = append(out, DoctorCheck{
			Label:  fmt.Sprintf("%s found in .env.example", k),
			Status: DoctorFail,
			Detail: k,
		})
	}
	return out
}

func looksLikeSecretValue(v string) bool {
	low := strings.ToLower(v)
	if strings.HasPrefix(low, "sk_") || strings.HasPrefix(low, "pk_live") {
		return true
	}
	if strings.HasPrefix(strings.ToUpper(v), "AKIA") {
		return true
	}
	if strings.Contains(low, "://") && (strings.Contains(low, "password") || strings.Contains(v, "@")) {
		// URL with userinfo often indicates credentials.
		if strings.Contains(v, "@") && strings.Contains(v, "://") {
			return true
		}
	}
	return false
}

func (p *Project) prodCredsInDevLocked() DoctorCheck {
	label := "No production credentials in development"
	dev, ok := p.envs["development"]
	if !ok {
		return DoctorCheck{Label: label, Status: DoctorPass}
	}
	var suspects []string
	for _, k := range dev.Keys() {
		val, _ := dev.Get(k)
		low := strings.ToLower(val)
		if strings.Contains(low, "prod") && (strings.Contains(low, "://") || p.isSecretLocked(k)) {
			suspects = append(suspects, k)
		}
	}
	if len(suspects) == 0 {
		return DoctorCheck{Label: label, Status: DoctorPass}
	}
	return DoctorCheck{
		Label:  label,
		Status: DoctorWarn,
		Detail: strings.Join(suspects, ", ") + " appears to contain production credentials",
	}
}

func scoreDoctor(checks []DoctorCheck) int {
	if len(checks) == 0 {
		return 100
	}
	score := 100
	for _, c := range checks {
		switch c.Status {
		case DoctorFail:
			score -= 20
		case DoctorWarn:
			score -= 8
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

// ImportFile merges keys from a dotenv file into the active environment.
// Does not echo values; returns count of keys merged.
func (p *Project) ImportFile(path string) (int, error) {
	incoming, err := envfile.ParseFile(path)
	if err != nil {
		return 0, fmt.Errorf("import: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.guardWriteLocked(p.active); err != nil {
		p.appendAuditLocked("human", "import", "", p.active, AuditDenied, "protected")
		return 0, err
	}
	keys := incoming.Keys()
	p.envs[p.active].Merge(incoming)
	if err := p.persistLocked(p.active); err != nil {
		return 0, err
	}
	p.appendAuditLocked("human", "import", "", p.active, AuditOK, fmt.Sprintf("%d keys", len(keys)))
	return len(keys), nil
}

// ExportEnv returns dotenv text for an environment.
// Secrets are replaced with redact.Placeholder unless reveal is true.
func (p *Project) ExportEnv(env string, reveal bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	vars, ok := p.envs[env]
	if !ok {
		return "", fmt.Errorf("unknown environment %q", env)
	}
	var b strings.Builder
	for _, k := range vars.Keys() {
		val, _ := vars.Get(k)
		if p.isSecretLocked(k) && !reveal {
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(redact.Placeholder)
			b.WriteString("\n")
			continue
		}
		if needsQuote(val) {
			b.WriteString(fmt.Sprintf("%s=%q\n", k, val))
		} else {
			b.WriteString(fmt.Sprintf("%s=%s\n", k, val))
		}
	}
	return b.String(), nil
}

func needsQuote(s string) bool {
	return strings.ContainsAny(s, " \t#\"'")
}

// ResolvedEnv returns the full key/value map for spawning child processes,
// including non-secret schema defaults when a key is unset.
func (p *Project) ResolvedEnv(env string) (map[string]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	vars, ok := p.envs[env]
	if !ok {
		return nil, fmt.Errorf("unknown environment %q", env)
	}
	out := map[string]string{}
	for _, k := range vars.Keys() {
		val, _ := vars.Get(k)
		out[k] = val
	}
	for k, field := range p.cfg.Schema {
		if _, present := out[k]; present {
			continue
		}
		if field.Default == "" {
			continue
		}
		// Only inject non-secret defaults into child process env.
		if field.Secret || field.Type == "secret" {
			continue
		}
		out[k] = field.Default
	}
	return out, nil
}

// Run executes cmd with the resolved environment merged into the process env.
// Returns the child exit code. stdout/stderr may be any io.Writer.
func (p *Project) Run(env string, command []string, stdout, stderr io.Writer) (int, error) {
	if len(command) == 0 {
		return 1, fmt.Errorf("no command provided")
	}
	resolved, err := p.ResolvedEnv(env)
	if err != nil {
		return 1, err
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = p.Root()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if f, ok := stdout.(*os.File); ok && f == os.Stdout {
		cmd.Stdin = os.Stdin
	}
	base := os.Environ()
	envMap := map[string]string{}
	for _, e := range base {
		if i := strings.IndexByte(e, '='); i > 0 {
			envMap[e[:i]] = e[i+1:]
		}
	}
	for k, v := range resolved {
		envMap[k] = v
	}
	cmd.Env = make([]string, 0, len(envMap))
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

// Root returns the project root path.
func (p *Project) Root() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.root
}

// GrantAgent creates a temporary agent permission grant.
func (p *Project) GrantAgent(agent, env string, write, deletePerm, readSecrets bool, ttl time.Duration) (GrantDisplay, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if agent == "" {
		return GrantDisplay{}, fmt.Errorf("agent identity is required")
	}
	if _, ok := p.envs[env]; !ok {
		return GrantDisplay{}, fmt.Errorf("unknown environment %q", env)
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	expires := p.now().Add(ttl)
	grant := AgentGrant{
		Agent:       agent,
		Environment: env,
		Write:       write,
		Delete:      deletePerm,
		ReadSecrets: readSecrets,
		ExpiresAt:   expires,
	}
	if p.agentGrants == nil {
		p.agentGrants = map[string]AgentGrant{}
	}
	p.agentGrants[agent] = grant
	if err := p.persistGrantsLocked(); err != nil {
		return GrantDisplay{}, err
	}
	p.appendAuditLocked(agent, "grant", "", env, AuditOK, "write="+fmt.Sprint(write))
	return GrantDisplay{
		Agent:       agent,
		Environment: env,
		Metadata:    true,
		ReadSecrets: readSecrets,
		Write:       write,
		Delete:      deletePerm,
		ExpiresAt:   expires,
		ExpiresIn:   ttl,
	}, nil
}

// RevokeAgent withdraws an agent's elevated permissions immediately.
func (p *Project) RevokeAgent(agent string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.agentGrants == nil {
		p.agentGrants = map[string]AgentGrant{}
	}
	if _, ok := p.agentGrants[agent]; !ok {
		return fmt.Errorf("no active grant for agent %q", agent)
	}
	delete(p.agentGrants, agent)
	if err := p.persistGrantsLocked(); err != nil {
		return err
	}
	p.appendAuditLocked(agent, "revoke", "", "", AuditOK, "")
	return nil
}

// HasAgentWrite reports whether agent currently has write on env.
func (p *Project) HasAgentWrite(agent, env string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	g, ok := p.agentGrants[agent]
	if !ok {
		return false
	}
	if p.now().After(g.ExpiresAt) {
		return false
	}
	return g.Write && g.Environment == env
}

// HasAgentDelete reports whether agent currently may delete on env.
func (p *Project) HasAgentDelete(agent, env string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	g, ok := p.agentGrants[agent]
	if !ok {
		return false
	}
	if p.now().After(g.ExpiresAt) {
		return false
	}
	if g.Environment != env {
		return false
	}
	return g.Delete || g.Write
}

func (p *Project) persistGrantsLocked() error {
	dir := filepath.Join(p.root, ".envy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "grants.json")
	data, err := json.MarshalIndent(p.agentGrants, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (p *Project) loadGrantsLocked() {
	path := filepath.Join(p.root, ".envy", "grants.json")
	data, err := os.ReadFile(path)
	if err != nil {
		p.agentGrants = map[string]AgentGrant{}
		return
	}
	var grants map[string]AgentGrant
	if err := json.Unmarshal(data, &grants); err != nil {
		p.agentGrants = map[string]AgentGrant{}
		return
	}
	p.agentGrants = grants
}

// CICheck runs validation plus doctor leak/policy checks for pipeline use.
// Returns findings text and whether the check passed.
func (p *Project) CICheck(env string) (ok bool, report string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.envs[env]; !exists {
		return false, fmt.Sprintf("unknown environment %q", env)
	}
	prev := p.active
	p.active = env
	defer func() { p.active = prev }()

	var parts []string
	failed := false
	vres := p.validateLocked(env)
	for _, f := range vres.Missing {
		failed = true
		parts = append(parts, fmt.Sprintf("missing required key: %s", f.Key))
	}
	for _, f := range vres.Invalid {
		failed = true
		parts = append(parts, fmt.Sprintf("invalid %s: %s", f.Key, f.Message))
	}
	doc := p.doctorLocked()
	for _, c := range doc.Checks {
		if c.Status == DoctorFail {
			failed = true
			parts = append(parts, "policy: "+c.Label)
			if c.Detail != "" {
				parts = append(parts, "  "+c.Detail)
			}
		}
	}
	if len(parts) == 0 {
		return true, "ok"
	}
	return !failed, strings.Join(parts, "\n")
}

func (p *Project) doctorLocked() DoctorResult {
	var checks []DoctorCheck

	// Required variables for the active environment.
	vres := p.validateLocked(p.active)
	if len(vres.Missing) > 0 {
		keys := make([]string, 0, len(vres.Missing))
		for _, f := range vres.Missing {
			keys = append(keys, f.Key)
		}
		checks = append(checks, DoctorCheck{
			Label:  "All required variables exist",
			Status: DoctorFail,
			Detail: "missing: " + strings.Join(keys, ", "),
		})
	} else {
		checks = append(checks, DoctorCheck{
			Label:  "All required variables exist",
			Status: DoctorPass,
		})
	}

	var dupDetails []string
	for _, env := range p.cfg.EnvironmentNames() {
		ec := p.cfg.Environments[env]
		if ec.File == "" {
			continue
		}
		dups, err := envfile.ScanDuplicates(filepath.Join(p.root, ec.File))
		if err != nil {
			continue
		}
		for _, d := range dups {
			dupDetails = append(dupDetails, fmt.Sprintf("%s in %s", d.Key, ec.File))
		}
	}
	if len(dupDetails) > 0 {
		checks = append(checks, DoctorCheck{Label: "No duplicated keys", Status: DoctorFail, Detail: strings.Join(dupDetails, "; ")})
	} else {
		checks = append(checks, DoctorCheck{Label: "No duplicated keys", Status: DoctorPass})
	}
	checks = append(checks, p.gitignoreCheckLocked())
	checks = append(checks, p.publicLeakChecksLocked()...)
	checks = append(checks, p.prodCredsInDevLocked())
	return DoctorResult{Checks: checks, Score: scoreDoctor(checks)}
}
