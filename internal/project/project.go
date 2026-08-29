// Package project is the deep domain module for Envy environment management.
// Callers and tests interact only through Project's public interface.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fxckcode/envy/internal/config"
	"github.com/fxckcode/envy/internal/envfile"
	"github.com/fxckcode/envy/internal/redact"
)

// VariableView is a safe display projection of a binding.
type VariableView struct {
	Key       string
	Display   string // masked if secret
	Secret    bool
	Missing   bool
	Invalid   bool
	InvalidReason string
}

// Status summarizes the active environment footer.
type Status struct {
	VariableCount int
	MissingCount  int
	SecretsHidden bool
}

// ProviderMeta is environment source metadata only.
type ProviderMeta struct {
	Environment string
	Source      string
}

// AuditResult is the outcome of an audited operation.
type AuditResult string

const (
	AuditOK     AuditResult = "ok"
	AuditDenied AuditResult = "denied"
	AuditError  AuditResult = "error"
)

// AuditEntry is a redacted activity timeline row.
type AuditEntry struct {
	Actor       string
	Action      string
	Key         string
	Environment string
	Time        time.Time
	Result      AuditResult
	Detail      string // never contains cleartext secrets
}

// ApprovalDecision is a human response to an agent write request.
type ApprovalDecision string

const (
	AllowOnce       ApprovalDecision = "allow_once"
	AllowForProject ApprovalDecision = "allow_project"
	Deny            ApprovalDecision = "deny"
)

// ApprovalRequest is a pending agent write awaiting human gate.
type ApprovalRequest struct {
	ID          string
	Actor       string
	Environment string
	Key         string
	OldValue    string
	NewValue    string
	Secret      bool
	Reason      string
}

// CellKind describes a compare-matrix cell.
type CellKind string

const (
	CellPresent CellKind = "present"
	CellAbsent  CellKind = "absent"
	CellDiff    CellKind = "diff"
	CellOnly    CellKind = "only"
)

// CompareCell is one matrix cell (no cleartext secrets).
type CompareCell struct {
	Kind    CellKind
	Display string // glyph or mask — never secret cleartext
}

// CompareResult is the compare matrix + warnings.
type CompareResult struct {
	LeftEnv  string
	RightEnv string
	Keys     []string
	Cells    map[string]map[string]CompareCell // key -> env -> cell
	Warnings []string
}

// Finding is a validation issue without secret values.
type Finding struct {
	Key     string
	Kind    string // missing | invalid
	Message string
}

// ValidationResult holds schema validation findings.
type ValidationResult struct {
	Missing []Finding
	Invalid []Finding
}

// Project is the mutable local project state behind the TUI and CLI.
type Project struct {
	mu            sync.Mutex
	root          string
	cfg           *config.Config
	name          string
	active        string
	envs          map[string]*envfile.Vars
	elevatedTrust bool
	audit         []AuditEntry
	pending       []ApprovalRequest
	projectGrants map[string]bool // "env.key" or "*" for project-wide agent write grant
	secretKeys    map[string]bool // runtime secret marks (add-form) beyond schema
	agentGrants   map[string]AgentGrant
	now           func() time.Time
	idSeq         int
}

// Open loads envy.yaml and environment files from root.
// Returns ErrNoConfig when neither envy.yaml nor discoverable .env files exist.
func Open(root string) (*Project, error) {
	cfgPath := filepath.Join(root, "envy.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if os.IsNotExist(unwrapPathError(err)) || isNotExistMsg(err) {
			if !hasDiscoverableEnv(root) {
				return nil, fmt.Errorf("%w in %s", ErrNoConfig, root)
			}
			return nil, fmt.Errorf("configuration error: envy.yaml not found in %s (discoverable .env files require envy.yaml for CLI mutations/checks)", root)
		}
		return nil, err
	}
	name := cfg.ProjectName
	if name == "" {
		name = filepath.Base(root)
	}
	p := &Project{
		root:          root,
		cfg:           cfg,
		name:          name,
		envs:          map[string]*envfile.Vars{},
		projectGrants: map[string]bool{},
		secretKeys:    map[string]bool{},
		agentGrants:   map[string]AgentGrant{},
		now:           time.Now,
	}
	names := cfg.EnvironmentNames()
	if len(names) == 0 {
		return nil, fmt.Errorf("no environments declared in envy.yaml")
	}
	for _, n := range names {
		envCfg := cfg.Environments[n]
		vars := envfile.New()
		if envCfg.File != "" {
			vars, err = envfile.ParseFile(filepath.Join(root, envCfg.File))
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", n, err)
			}
		}
		p.envs[n] = vars
	}
	// Prefer development as default active when present.
	p.active = names[0]
	for _, n := range names {
		if n == "development" {
			p.active = n
			break
		}
	}
	p.loadGrantsLocked()
	p.loadAuditLocked()
	return p, nil
}

func unwrapPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func isNotExistMsg(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "cannot find")
}

func hasDiscoverableEnv(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			return true
		}
	}
	return false
}

// Name returns the project display name.
func (p *Project) Name() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.name
}

// EnvironmentNames returns sorted environment names.
func (p *Project) EnvironmentNames() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg.EnvironmentNames()
}

// ActiveEnvironment returns the selected environment name.
func (p *Project) ActiveEnvironment() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// SelectEnvironment switches the active environment.
func (p *Project) SelectEnvironment(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[name]; !ok {
		return fmt.Errorf("unknown environment %q", name)
	}
	p.active = name
	return nil
}

// IsProtected reports whether the named environment is protected.
func (p *Project) IsProtected(env string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg.IsProtected(env)
}

// SetElevatedTrust enables or disables production-level edit authorization.
func (p *Project) SetElevatedTrust(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.elevatedTrust = v
}

// HasElevatedTrust reports whether elevated trust is active.
func (p *Project) HasElevatedTrust() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.elevatedTrust
}

// Variables returns display projections for the active environment.
func (p *Project) Variables() []VariableView {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.variablesLocked(p.active)
}

func (p *Project) variablesLocked(env string) []VariableView {
	vars := p.envs[env]
	keys := vars.Keys()
	// Include required schema keys that are missing.
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	for k, field := range p.cfg.Schema {
		if field.Required && !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]VariableView, 0, len(keys))
	for _, k := range keys {
		secret := p.isSecretLocked(k)
		raw, ok := vars.Get(k)
		missing := p.cfg.IsRequired(k) && !ok
		invalid, reason := validateValue(p.cfg, k, raw, ok)
		display := "—"
		if ok {
			display = redact.Display(raw, secret)
		} else if missing {
			display = "—"
		}
		out = append(out, VariableView{
			Key:           k,
			Display:       display,
			Secret:        secret,
			Missing:       missing,
			Invalid:       invalid,
			InvalidReason: reason,
		})
	}
	return out
}

// Status returns footer counts for the active environment.
func (p *Project) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	views := p.variablesLocked(p.active)
	missing := 0
	for _, v := range views {
		if v.Missing {
			missing++
		}
	}
	// Count actual bindings, not schema-only missing rows, for "N variables".
	count := p.envs[p.active].Len()
	return Status{
		VariableCount: count,
		MissingCount:  missing,
		SecretsHidden: true,
	}
}

// AddVariable creates a new key in the active environment.
func (p *Project) AddVariable(key, value string) error {
	return p.AddVariableSecret(key, value, false)
}

// AddVariableSecret creates a key and optionally marks it secret for display.
func (p *Project) AddVariableSecret(key, value string, secret bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if err := p.guardWriteLocked(p.active); err != nil {
		return err
	}
	if _, exists := p.envs[p.active].Get(key); exists {
		return fmt.Errorf("key %q already exists", key)
	}
	p.envs[p.active].Set(key, value)
	if secret {
		p.secretKeys[key] = true
	}
	if err := p.persistLocked(p.active); err != nil {
		return err
	}
	p.appendAuditLocked("human", "add", key, p.active, AuditOK, "")
	return nil
}

// EditVariable updates an existing key in the active environment.
func (p *Project) EditVariable(key, value string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.guardWriteLocked(p.active); err != nil {
		return err
	}
	if _, ok := p.envs[p.active].Get(key); !ok {
		return fmt.Errorf("key %q not found", key)
	}
	p.envs[p.active].Set(key, value)
	if err := p.persistLocked(p.active); err != nil {
		return err
	}
	p.appendAuditLocked("human", "edit", key, p.active, AuditOK, "")
	return nil
}

// RawValue returns the underlying value (for apply paths / tests). Prefer Variables() for UI.
func (p *Project) RawValue(env, key string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	vars, ok := p.envs[env]
	if !ok {
		return "", false
	}
	return vars.Get(key)
}

// ErrProtected is returned when a write targets a protected environment without elevated trust.
var ErrProtected = errors.New("environment is protected: explicit authorization is required")

func (p *Project) guardWriteLocked(env string) error {
	if p.cfg.IsProtected(env) && !p.elevatedTrust {
		return fmt.Errorf("%w: %q", ErrProtected, env)
	}
	return nil
}

func (p *Project) persistLocked(env string) error {
	envCfg, ok := p.cfg.Environments[env]
	if !ok || envCfg.File == "" {
		// Provider-backed envs are in-memory only for MVP local store.
		return nil
	}
	path := filepath.Join(p.root, envCfg.File)
	return p.envs[env].WriteFile(path)
}

// Compare builds a presence/diff matrix between two environments.
func (p *Project) Compare(left, right string) (CompareResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[left]; !ok {
		return CompareResult{}, fmt.Errorf("unknown environment %q", left)
	}
	if _, ok := p.envs[right]; !ok {
		return CompareResult{}, fmt.Errorf("unknown environment %q", right)
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

	cells := map[string]map[string]CompareCell{}
	var warnings []string
	for _, k := range keys {
		lv, lok := p.envs[left].Get(k)
		rv, rok := p.envs[right].Get(k)
		row := map[string]CompareCell{}
		switch {
		case lok && rok:
			if lv == rv {
				row[left] = CompareCell{Kind: CellPresent, Display: "✓"}
				row[right] = CompareCell{Kind: CellPresent, Display: "✓"}
			} else {
				// Differing values: show ≠ only — never cleartext (secret or not).
				row[left] = CompareCell{Kind: CellDiff, Display: "≠"}
				row[right] = CompareCell{Kind: CellDiff, Display: "≠"}
				warnings = append(warnings, fmt.Sprintf("⚠ %s differs between %s and %s", k, left, right))
			}
		case lok && !rok:
			row[left] = CompareCell{Kind: CellOnly, Display: "◇"}
			row[right] = CompareCell{Kind: CellAbsent, Display: "✗"}
			warnings = append(warnings, fmt.Sprintf("⚠ %s is missing %s", right, k))
			warnings = append(warnings, fmt.Sprintf("⚠ %s exists only in %s", k, left))
		case !lok && rok:
			row[left] = CompareCell{Kind: CellAbsent, Display: "✗"}
			row[right] = CompareCell{Kind: CellOnly, Display: "◇"}
			warnings = append(warnings, fmt.Sprintf("⚠ %s is missing %s", left, k))
			warnings = append(warnings, fmt.Sprintf("⚠ %s exists only in %s", k, right))
		default:
			row[left] = CompareCell{Kind: CellAbsent, Display: "✗"}
			row[right] = CompareCell{Kind: CellAbsent, Display: "✗"}
		}
		cells[k] = row
	}
	return CompareResult{
		LeftEnv:  left,
		RightEnv: right,
		Keys:     keys,
		Cells:    cells,
		Warnings: warnings,
	}, nil
}

// Validate runs schema validation on the active environment.
func (p *Project) Validate() ValidationResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.validateLocked(p.active)
}

func (p *Project) validateLocked(env string) ValidationResult {
	var missing, invalid []Finding
	vars := p.envs[env]
	for k, field := range p.cfg.Schema {
		raw, ok := vars.Get(k)
		if field.Required && !ok {
			missing = append(missing, Finding{
				Key:     k,
				Kind:    "missing",
				Message: "required key missing",
			})
			continue
		}
		if ok {
			bad, reason := validateValue(p.cfg, k, raw, true)
			if bad {
				invalid = append(invalid, Finding{
					Key:     k,
					Kind:    "invalid",
					Message: reason,
				})
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	sort.Slice(invalid, func(i, j int) bool { return invalid[i].Key < invalid[j].Key })
	return ValidationResult{Missing: missing, Invalid: invalid}
}

func validateValue(cfg *config.Config, key, value string, present bool) (bool, string) {
	if !present {
		return false, ""
	}
	field, ok := cfg.Schema[key]
	if !ok || field.Type == "" || field.Type == "string" || field.Type == "secret" {
		return false, ""
	}
	switch field.Type {
	case "integer":
		if _, err := strconv.Atoi(value); err != nil {
			return true, "expected integer"
		}
	case "boolean":
		low := strings.ToLower(value)
		if low != "true" && low != "false" && low != "1" && low != "0" {
			return true, "expected boolean"
		}
	case "url":
		if !strings.Contains(value, "://") {
			return true, "expected url"
		}
	case "enum":
		if len(field.Values) > 0 {
			found := false
			for _, v := range field.Values {
				if v == value {
					found = true
					break
				}
			}
			if !found {
				return true, "expected enum value"
			}
		}
	}
	return false, ""
}

// Providers returns source metadata for each environment.
func (p *Project) Providers() []ProviderMeta {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := p.cfg.EnvironmentNames()
	out := make([]ProviderMeta, 0, len(names))
	for _, n := range names {
		out = append(out, ProviderMeta{
			Environment: n,
			Source:      p.cfg.SourceLabel(n),
		})
	}
	return out
}

// AgentActivity returns the audit timeline (newest last).
func (p *Project) AgentActivity() []AuditEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]AuditEntry, len(p.audit))
	copy(out, p.audit)
	return out
}

func (p *Project) appendAuditLocked(actor, action, key, env string, result AuditResult, detail string) {
	p.audit = append(p.audit, AuditEntry{
		Actor:       actor,
		Action:      action,
		Key:         key,
		Environment: env,
		Time:        p.now(),
		Result:      result,
		Detail:      detail,
	})
	_ = p.persistAuditLocked()
}

func (p *Project) persistAuditLocked() error {
	dir := filepath.Join(p.root, ".envy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	entry := p.audit[len(p.audit)-1]
	type wire struct {
		Actor       string    `json:"actor"`
		Action      string    `json:"action"`
		Key         string    `json:"key"`
		Environment string    `json:"environment"`
		Time        time.Time `json:"timestamp"`
		Result      string    `json:"result"`
		Detail      string    `json:"detail,omitempty"`
	}
	data, err := json.Marshal(wire{
		Actor:       entry.Actor,
		Action:      entry.Action,
		Key:         entry.Key,
		Environment: entry.Environment,
		Time:        entry.Time,
		Result:      string(entry.Result),
		Detail:      entry.Detail,
	})
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func (p *Project) loadAuditLocked() {
	path := filepath.Join(p.root, ".envy", "audit.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var w struct {
			Actor       string    `json:"actor"`
			Action      string    `json:"action"`
			Key         string    `json:"key"`
			Environment string    `json:"environment"`
			Time        time.Time `json:"timestamp"`
			Result      string    `json:"result"`
			Detail      string    `json:"detail"`
		}
		if err := json.Unmarshal([]byte(line), &w); err != nil {
			continue
		}
		p.audit = append(p.audit, AuditEntry{
			Actor:       w.Actor,
			Action:      w.Action,
			Key:         w.Key,
			Environment: w.Environment,
			Time:        w.Time,
			Result:      AuditResult(w.Result),
			Detail:      w.Detail,
		})
	}
}

// EnqueueApproval queues an agent write request for human approval.
func (p *Project) EnqueueApproval(actor, env, key, newValue, reason string) (ApprovalRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[env]; !ok {
		return ApprovalRequest{}, fmt.Errorf("unknown environment %q", env)
	}
	old, _ := p.envs[env].Get(key)
	secret := p.isSecretLocked(key)
	p.idSeq++
	req := ApprovalRequest{
		ID:          fmt.Sprintf("apr-%d", p.idSeq),
		Actor:       actor,
		Environment: env,
		Key:         key,
		OldValue:    old,
		NewValue:    newValue,
		Secret:      secret,
		Reason:      reason,
	}
	// Project-wide grant short-circuits to apply.
	if p.projectGrants["*"] || p.projectGrants[env+"."+key] || p.projectGrants[env+".*"] {
		p.envs[env].Set(key, newValue)
		_ = p.persistLocked(env)
		p.appendAuditLocked(actor, "write", key, env, AuditOK, "applied via project grant")
		return req, nil
	}
	p.pending = append(p.pending, req)
	p.appendAuditLocked(actor, "request_write", key, env, AuditOK, "awaiting approval")
	return req, nil
}

// PendingApprovals returns queued approval requests.
func (p *Project) PendingApprovals() []ApprovalRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ApprovalRequest, len(p.pending))
	copy(out, p.pending)
	return out
}

// RespondApproval applies a human decision to a pending request.
func (p *Project) RespondApproval(id string, decision ApprovalDecision) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := -1
	var req ApprovalRequest
	for i, r := range p.pending {
		if r.ID == id {
			idx = i
			req = r
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("approval %q not found", id)
	}
	// Remove from pending first (immutable slice rebuild).
	p.pending = append(append([]ApprovalRequest{}, p.pending[:idx]...), p.pending[idx+1:]...)

	switch decision {
	case Deny:
		p.appendAuditLocked("human", "deny", req.Key, req.Environment, AuditDenied, req.Actor)
		return nil
	case AllowOnce:
		p.envs[req.Environment].Set(req.Key, req.NewValue)
		if err := p.persistLocked(req.Environment); err != nil {
			return err
		}
		p.appendAuditLocked("human", "allow_once", req.Key, req.Environment, AuditOK, req.Actor)
		return nil
	case AllowForProject:
		p.projectGrants["*"] = true
		p.envs[req.Environment].Set(req.Key, req.NewValue)
		if err := p.persistLocked(req.Environment); err != nil {
			return err
		}
		p.appendAuditLocked("human", "allow_project", req.Key, req.Environment, AuditOK, req.Actor)
		return nil
	default:
		return fmt.Errorf("unknown decision %q", decision)
	}
}

// ApprovalDisplay returns redacted old/new for the modal.
func ApprovalDisplay(req ApprovalRequest) (oldDisp, newDisp string) {
	return redact.ModalValue(req.OldValue, req.Secret), redact.ModalValue(req.NewValue, req.Secret)
}

// Config returns a copy-safe view of schema secret flags (for TUI).
func (p *Project) IsSecretKey(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isSecretLocked(key)
}

func (p *Project) isSecretLocked(key string) bool {
	if p.secretKeys[key] {
		return true
	}
	return p.cfg.IsSecret(key)
}
