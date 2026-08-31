package project

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/fxckcode/envy/internal/config"
	"github.com/fxckcode/envy/internal/redact"
)

// ErrPermission is returned when agent policy denies an operation.
var ErrPermission = errors.New("permission denied")

// SchemaConstraints captures optional validation bounds for a field.
type SchemaConstraints struct {
	Min    *int     `json:"min,omitempty"`
	Max    *int     `json:"max,omitempty"`
	Values []string `json:"values,omitempty"`
}

// SchemaFieldView is agent-safe schema metadata.
type SchemaFieldView struct {
	Key         string             `json:"key"`
	Type        string             `json:"type"`
	Required    bool               `json:"required"`
	Secret      bool               `json:"secret"`
	Default     string             `json:"default,omitempty"`
	Values      []string           `json:"values,omitempty"`
	Constraints *SchemaConstraints `json:"constraints,omitempty"`
}

// EnvInfo is environment name + source/provider without values.
type EnvInfo struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Provider  string `json:"provider,omitempty"`
	Protected bool   `json:"protected"`
}

// Config returns the loaded config (read-only use by callers).
func (p *Project) Config() *config.Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg
}

// ListEnvironments returns names and sources without secret payloads.
func (p *Project) ListEnvironments() []EnvInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := p.cfg.EnvironmentNames()
	out := make([]EnvInfo, 0, len(names))
	for _, n := range names {
		envCfg := p.cfg.Environments[n]
		out = append(out, EnvInfo{
			Name:      n,
			Source:    p.cfg.SourceLabel(n),
			Provider:  envCfg.Provider,
			Protected: p.cfg.IsProtected(n),
		})
	}
	return out
}

// SchemaViews returns declared schema fields for agents.
func (p *Project) SchemaViews() []SchemaFieldView {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make([]string, 0, len(p.cfg.Schema))
	for k := range p.cfg.Schema {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]SchemaFieldView, 0, len(keys))
	for _, k := range keys {
		f := p.cfg.Schema[k]
		typ := f.Type
		if typ == "" {
			typ = "string"
		}
		view := SchemaFieldView{
			Key:      k,
			Type:     typ,
			Required: f.Required,
			Secret:   f.Secret || f.Type == "secret",
			Default:  f.Default,
			Values:   append([]string(nil), f.Values...),
		}
		if f.Min != nil || f.Max != nil || len(f.Values) > 0 {
			view.Constraints = &SchemaConstraints{
				Min:    f.Min,
				Max:    f.Max,
				Values: append([]string(nil), f.Values...),
			}
		}
		out = append(out, view)
	}
	return out
}

// Exists reports whether a key is present without revealing its value.
func (p *Project) Exists(env, key string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	vars, ok := p.envs[env]
	if !ok {
		return false, fmt.Errorf("unknown environment %q", env)
	}
	_, present := vars.Get(key)
	return present, nil
}

// Metadata returns status/type/secret/source with redacted value placeholder.
func (p *Project) Metadata(env, key string) (VariableView, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[env]; !ok {
		return VariableView{}, fmt.Errorf("unknown environment %q", env)
	}
	for _, v := range p.variablesLocked(env) {
		if v.Key == key {
			if v.Present && v.Secret {
				v.Display = redact.MCPPlaceholder
			}
			return v, nil
		}
	}
	secret := p.isSecretLocked(key)
	return VariableView{
		Key:     key,
		Secret:  secret,
		Missing: p.cfg.IsRequired(key),
		Present: false,
		Source:  p.cfg.SourceLabel(env),
		Display: "",
	}, nil
}

// SetInEnv writes a key in a named environment when the write guard allows.
// Caller (MCP layer) enforces agent policy before invoking.
func (p *Project) SetInEnv(env, key, value, actor string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if _, ok := p.envs[env]; !ok {
		return fmt.Errorf("unknown environment %q", env)
	}
	if err := p.guardWriteLocked(env); err != nil {
		p.appendAuditLocked(actor, "env_set", key, env, AuditDenied, "protected")
		return err
	}
	p.envs[env].Set(key, value)
	if err := p.persistLocked(env); err != nil {
		p.appendAuditLocked(actor, "env_set", key, env, AuditError, "persist failed")
		return err
	}
	p.appendAuditLocked(actor, "env_set", key, env, AuditOK, "")
	return nil
}

// DeleteInEnv removes a key from a named environment when the write guard allows.
func (p *Project) DeleteInEnv(env, key, actor string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.envs[env]; !ok {
		return fmt.Errorf("unknown environment %q", env)
	}
	if err := p.authorizeMutationLocked(actor, "env_delete", key, env, true); err != nil {
		return err
	}
	if _, ok := p.envs[env].Get(key); !ok {
		p.appendAuditLocked(actor, "env_delete", key, env, AuditError, "key not found")
		return fmt.Errorf("key %q not found", key)
	}
	p.envs[env].Delete(key)
	if err := p.persistLocked(env); err != nil {
		p.appendAuditLocked(actor, "env_delete", key, env, AuditError, "persist failed")
		return err
	}
	p.appendAuditLocked(actor, "env_delete", key, env, AuditOK, "")
	return nil
}

// CopyKey copies a key from source to target without exposing cleartext to callers.
func (p *Project) CopyKey(fromEnv, toEnv, key, actor string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	src, ok := p.envs[fromEnv]
	if !ok {
		return fmt.Errorf("unknown environment %q", fromEnv)
	}
	if _, ok := p.envs[toEnv]; !ok {
		return fmt.Errorf("unknown environment %q", toEnv)
	}
	val, present := src.Get(key)
	if !present {
		return fmt.Errorf("key %q not found in %s", key, fromEnv)
	}
	if err := p.guardWriteLocked(toEnv); err != nil {
		p.appendAuditLocked(actor, "env_copy", key, toEnv, AuditDenied, "protected")
		return err
	}
	p.envs[toEnv].Set(key, val)
	if err := p.persistLocked(toEnv); err != nil {
		p.appendAuditLocked(actor, "env_copy", key, toEnv, AuditError, "persist failed")
		return err
	}
	p.appendAuditLocked(actor, "env_copy", key, toEnv, AuditOK, "from="+fromEnv)
	return nil
}

// GenerateExample builds an example env payload with defaults/empty, never live secrets.
func (p *Project) GenerateExample() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make([]string, 0, len(p.cfg.Schema))
	for k := range p.cfg.Schema {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		f := p.cfg.Schema[k]
		if f.Secret || f.Type == "secret" {
			out[k] = ""
			continue
		}
		if f.Default != "" {
			out[k] = f.Default
			continue
		}
		out[k] = ""
	}
	return out
}

// QueueApproval records a pending agent write without changing values.
// Cleartext is kept only for human approval surfaces — never returned by MCP tools.
func (p *Project) QueueApproval(actor, env, key, newValue, reason string) (ApprovalRequest, error) {
	return p.EnqueueApproval(actor, env, key, newValue, reason)
}

// AppendAudit records an operation without secret values.
func (p *Project) AppendAudit(actor, operation, key, env string, result AuditResult, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appendAuditLocked(actor, operation, key, env, result, detail)
}

// AuditLog returns redacted audit entries (newest last).
func (p *Project) AuditLog() []AuditEntry {
	return p.AgentActivity()
}

// AgentPerms resolves permissions for an agent in an environment.
// Defaults: metadata true; read_values/write/delete false.
func (p *Project) AgentPerms(agent, env string) config.AgentPerms {
	p.mu.Lock()
	defer p.mu.Unlock()
	defaults := config.AgentPerms{Metadata: true}
	if p.cfg.Agents == nil {
		return defaults
	}
	byEnv, ok := p.cfg.Agents[agent]
	if !ok {
		if byEnv, ok = p.cfg.Agents["default"]; !ok {
			return defaults
		}
	}
	perms, ok := byEnv[env]
	if !ok {
		return defaults
	}
	return perms
}
