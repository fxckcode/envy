// Package mcpapi is the agent-facing deep module for Envy MCP tools.
// Callers and tests interact only through Service; policy and redaction live here.
package mcpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
)

// Service exposes MCP tool operations with agent policy and secret redaction.
type Service struct {
	project *project.Project
	actor   string
}

// New binds a project and actor identity (agent name).
func New(p *project.Project, actor string) *Service {
	if actor == "" {
		actor = "default"
	}
	return &Service{project: p, actor: actor}
}

// Actor returns the current agent identity.
func (s *Service) Actor() string { return s.actor }

// --- Response types (JSON-serializable, never contain live secrets) ---

type listItem struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
	Value  string `json:"value,omitempty"`
}

// ListResult is env_list output.
type ListResult struct {
	Environment string     `json:"environment"`
	Keys        []listItem `json:"keys"`
}

// EnvironmentsResult is env_list_environments output.
type EnvironmentsResult struct {
	Environments []project.EnvInfo `json:"environments"`
}

// SchemaResult is env_get_schema output.
type SchemaResult struct {
	Fields []project.SchemaFieldView `json:"fields"`
}

// CheckResult is env_check output.
type CheckResult struct {
	Environment string            `json:"environment"`
	Status      string            `json:"status"`
	Missing     []project.Finding `json:"missing"`
	Invalid     []project.Finding `json:"invalid"`
}

// DiffResult wraps compare output for agents.
type DiffResult struct {
	project.CompareResult
}

// ExistsResult is env_exists output.
type ExistsResult struct {
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Exists      bool   `json:"exists"`
}

// MetadataResult is env_metadata output.
type MetadataResult struct {
	Key         string `json:"key"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Type        string `json:"type"`
	Secret      bool   `json:"secret"`
	Source      string `json:"source"`
	Value       string `json:"value"`
}

// MutationResult is env_set / env_delete / env_copy output.
type MutationResult struct {
	OK          bool   `json:"ok"`
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Status      string `json:"status"` // applied | denied | pending_approval | unauthorized | policy_error
	Message     string `json:"message,omitempty"`
	ApprovalID  string `json:"approval_id,omitempty"`
}

// ExampleResult is env_generate_example output.
type ExampleResult struct {
	Payload map[string]string `json:"payload"`
}

// DoctorCheck is one health finding for agents.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
}

// DoctorOut is env_doctor output.
type DoctorOut struct {
	Checks []DoctorCheck `json:"checks"`
	Score  int           `json:"score"`
}

// AuditOut is the audit trail projection.
type AuditOut struct {
	Entries []auditView `json:"entries"`
}

type auditView struct {
	Actor       string    `json:"actor"`
	Tool        string    `json:"tool"`
	Key         string    `json:"key"`
	Environment string    `json:"environment"`
	Timestamp   time.Time `json:"timestamp"`
	Result      string    `json:"result"`
}

// List returns keys and secret flags; secret values are always [REDACTED]
// unless the agent has read_values (still never required by default policy).
func (s *Service) List(env string) (ListResult, error) {
	if env == "" {
		env = "development"
	}
	perms := s.project.AgentPerms(s.actor, env)
	if !perms.Metadata {
		s.project.AppendAudit(s.actor, "env_list", "", env, project.AuditDenied, "metadata denied")
		return ListResult{}, fmt.Errorf("%w: metadata", project.ErrPermission)
	}
	views, err := s.project.VariablesFor(env)
	if err != nil {
		return ListResult{}, err
	}
	items := make([]listItem, 0, len(views))
	for _, v := range views {
		item := listItem{Key: v.Key, Secret: v.Secret}
		if v.Present {
			item.Value = s.redactValue(v.Secret, env, v.Key, perms.ReadValues)
		}
		items = append(items, item)
	}
	s.project.AppendAudit(s.actor, "env_list", "", env, project.AuditOK, "")
	return ListResult{Environment: env, Keys: items}, nil
}

func (s *Service) redactValue(secret bool, env, key string, readValues bool) string {
	raw, ok := s.project.RawValue(env, key)
	if !ok {
		return ""
	}
	if secret {
		if readValues {
			return raw
		}
		return redact.MCPPlaceholder
	}
	return raw
}

// ListEnvironments returns environment names and sources without secret payloads.
func (s *Service) ListEnvironments() EnvironmentsResult {
	envs := s.project.ListEnvironments()
	s.project.AppendAudit(s.actor, "env_list_environments", "", "", project.AuditOK, "")
	return EnvironmentsResult{Environments: envs}
}

// GetSchema returns types, required flags, defaults, and secret markers.
func (s *Service) GetSchema() SchemaResult {
	fields := s.project.SchemaViews()
	s.project.AppendAudit(s.actor, "env_get_schema", "", "", project.AuditOK, "")
	return SchemaResult{Fields: fields}
}

// Check validates an environment; never includes secret values.
func (s *Service) Check(env string) (CheckResult, error) {
	if env == "" {
		env = "development"
	}
	val, err := s.project.ValidateEnv(env)
	if err != nil {
		return CheckResult{}, err
	}
	s.project.AppendAudit(s.actor, "env_check", "", env, project.AuditOK, val.Status)
	return CheckResult{
		Environment: env,
		Status:      val.Status,
		Missing:     val.Missing,
		Invalid:     val.Invalid,
	}, nil
}

// Diff compares two environments; secrets redacted, non-secret diffs include values.
func (s *Service) Diff(left, right string) (DiffResult, error) {
	cmp, err := s.project.Compare(left, right)
	if err != nil {
		return DiffResult{}, err
	}
	s.project.AppendAudit(s.actor, "env_diff", "", left+".."+right, project.AuditOK, "")
	return DiffResult{CompareResult: cmp}, nil
}

// Exists returns a boolean without revealing the value.
func (s *Service) Exists(env, key string) (ExistsResult, error) {
	if env == "" {
		env = "development"
	}
	ok, err := s.project.Exists(env, key)
	if err != nil {
		return ExistsResult{}, err
	}
	s.project.AppendAudit(s.actor, "env_exists", key, env, project.AuditOK, "")
	return ExistsResult{Environment: env, Key: key, Exists: ok}, nil
}

// Metadata returns status, type, secret flag, source, and redacted value placeholder.
func (s *Service) Metadata(env, key string) (MetadataResult, error) {
	if env == "" {
		env = "development"
	}
	perms := s.project.AgentPerms(s.actor, env)
	if !perms.Metadata {
		s.project.AppendAudit(s.actor, "env_metadata", key, env, project.AuditDenied, "metadata denied")
		return MetadataResult{}, fmt.Errorf("%w: metadata", project.ErrPermission)
	}
	view, err := s.project.Metadata(env, key)
	if err != nil {
		return MetadataResult{}, err
	}
	typ := "string"
	for _, f := range s.project.SchemaViews() {
		if f.Key == key {
			typ = f.Type
			break
		}
	}
	status := "missing"
	if view.Present {
		status = "configured"
	}
	if view.Invalid {
		status = "invalid"
	}
	value := ""
	if view.Present {
		value = s.redactValue(view.Secret, env, key, perms.ReadValues)
	}
	s.project.AppendAudit(s.actor, "env_metadata", key, env, project.AuditOK, status)
	return MetadataResult{
		Key:         key,
		Environment: env,
		Status:      status,
		Type:        typ,
		Secret:      view.Secret,
		Source:      view.Source,
		Value:       value,
	}, nil
}

// ReadValue attempts to return plaintext. Secrets require read_values; denials are audited.
func (s *Service) ReadValue(env, key string) (string, error) {
	if env == "" {
		env = "development"
	}
	perms := s.project.AgentPerms(s.actor, env)
	raw, ok := s.project.RawValue(env, key)
	if !ok {
		s.project.AppendAudit(s.actor, "env_read", key, env, project.AuditError, "not found")
		return "", fmt.Errorf("key %q not found", key)
	}
	secret := false
	if meta, err := s.project.Metadata(env, key); err == nil {
		secret = meta.Secret
	}
	if secret && !perms.ReadValues {
		s.project.AppendAudit(s.actor, "env_read", key, env, project.AuditDenied, "read_values denied")
		return "", fmt.Errorf("%w: read_values", project.ErrPermission)
	}
	if !perms.ReadValues {
		s.project.AppendAudit(s.actor, "env_read", key, env, project.AuditDenied, "read_values denied")
		return "", fmt.Errorf("%w: read_values", project.ErrPermission)
	}
	s.project.AppendAudit(s.actor, "env_read", key, env, project.AuditOK, "")
	return raw, nil
}

// Set stores a value when write is permitted; responses never echo unrelated secrets.
func (s *Service) Set(env, key, value, reason string) (MutationResult, error) {
	if env == "" {
		env = "development"
	}
	perms := s.project.AgentPerms(s.actor, env)
	out := MutationResult{Environment: env, Key: key}

	if s.project.Config().IsProtected(env) && !perms.Write {
		out.OK = false
		out.Status = "unauthorized"
		out.Message = "production write denied for agent"
		s.project.AppendAudit(s.actor, "env_set", key, env, project.AuditDenied, out.Message)
		return out, fmt.Errorf("%w: %s", project.ErrPermission, env)
	}

	if !perms.Write {
		req, err := s.project.QueueApproval(s.actor, env, key, value, reason)
		if err != nil {
			out.OK = false
			out.Status = "denied"
			out.Message = err.Error()
			s.project.AppendAudit(s.actor, "env_set", key, env, project.AuditDenied, out.Message)
			return out, err
		}
		out.OK = false
		out.Status = "pending_approval"
		out.ApprovalID = req.ID
		out.Message = "write denied; queued for human approval"
		s.project.AppendAudit(s.actor, "env_set", key, env, project.AuditPending, "awaiting approval")
		return out, nil
	}

	if err := s.project.SetInEnv(env, key, value, s.actor); err != nil {
		out.OK = false
		if errors.Is(err, project.ErrProtected) {
			out.Status = "unauthorized"
			out.Message = err.Error()
			return out, err
		}
		out.Status = "denied"
		out.Message = err.Error()
		return out, err
	}
	out.OK = true
	out.Status = "applied"
	return out, nil
}

// Delete removes a key when delete permission allows.
func (s *Service) Delete(env, key string) (MutationResult, error) {
	if env == "" {
		env = "development"
	}
	perms := s.project.AgentPerms(s.actor, env)
	out := MutationResult{Environment: env, Key: key}

	if !perms.Delete {
		out.OK = false
		out.Status = "denied"
		out.Message = "delete permission required"
		s.project.AppendAudit(s.actor, "env_delete", key, env, project.AuditDenied, out.Message)
		return out, fmt.Errorf("%w: delete", project.ErrPermission)
	}

	if err := s.project.DeleteInEnv(env, key, s.actor); err != nil {
		out.OK = false
		if errors.Is(err, project.ErrProtected) {
			out.Status = "unauthorized"
			out.Message = err.Error()
			return out, err
		}
		out.Status = "denied"
		out.Message = err.Error()
		return out, err
	}
	out.OK = true
	out.Status = "applied"
	return out, nil
}

// Copy copies a key between environments without exposing plaintext to the agent.
func (s *Service) Copy(fromEnv, toEnv, key string) (MutationResult, error) {
	permsTo := s.project.AgentPerms(s.actor, toEnv)
	out := MutationResult{Environment: toEnv, Key: key}
	if s.project.Config().IsProtected(toEnv) && !permsTo.Write {
		out.OK = false
		out.Status = "unauthorized"
		out.Message = "protected environment requires explicit grant"
		s.project.AppendAudit(s.actor, "env_copy", key, toEnv, project.AuditDenied, out.Message)
		return out, fmt.Errorf("%w: %s", project.ErrPermission, toEnv)
	}
	if !permsTo.Write {
		out.OK = false
		out.Status = "denied"
		out.Message = "write permission required on target"
		s.project.AppendAudit(s.actor, "env_copy", key, toEnv, project.AuditDenied, out.Message)
		return out, fmt.Errorf("%w: write", project.ErrPermission)
	}
	if err := s.project.CopyKey(fromEnv, toEnv, key, s.actor); err != nil {
		out.OK = false
		out.Status = "denied"
		out.Message = err.Error()
		return out, err
	}
	out.OK = true
	out.Status = "applied"
	return out, nil
}

// GenerateExample returns empty/default non-secret placeholders; never live secrets.
func (s *Service) GenerateExample() ExampleResult {
	payload := s.project.GenerateExample()
	s.project.AppendAudit(s.actor, "env_generate_example", "", "", project.AuditOK, "")
	return ExampleResult{Payload: payload}
}

// Doctor returns structured checks and score; sensitive findings redacted.
func (s *Service) Doctor() DoctorOut {
	d := s.project.Doctor()
	checks := make([]DoctorCheck, 0, len(d.Checks))
	for _, c := range d.Checks {
		name := doctorName(c.Label)
		key := ""
		if name == "leaked_secret_in_example" {
			key = strings.TrimSpace(c.Detail)
			if key == "" {
				key = extractKeyFromLeakLabel(c.Label)
			}
		}
		checks = append(checks, DoctorCheck{
			Name:    name,
			Status:  string(c.Status),
			Message: sanitizeDetail(c.Detail),
			Key:     key,
		})
	}
	s.project.AppendAudit(s.actor, "env_doctor", "", "", project.AuditOK, fmt.Sprintf("score=%d", d.Score))
	return DoctorOut{Checks: checks, Score: d.Score}
}

func doctorName(label string) string {
	low := strings.ToLower(label)
	switch {
	case strings.Contains(low, "required"):
		return "required_variables"
	case strings.Contains(low, "gitignore") || strings.Contains(low, "ignored by git"):
		return "gitignore_env"
	case strings.Contains(low, "duplicat"):
		return "duplicates"
	case strings.Contains(low, ".env.example") || strings.Contains(low, "public files"):
		return "leaked_secret_in_example"
	case strings.Contains(low, "production credentials"):
		return "prod_creds_in_dev"
	default:
		return strings.ReplaceAll(low, " ", "_")
	}
}

func extractKeyFromLeakLabel(label string) string {
	// "AWS_SECRET_ACCESS_KEY found in .env.example"
	parts := strings.Fields(label)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// Audit returns stored audit records without plaintext secrets.
func (s *Service) Audit() AuditOut {
	entries := s.project.AuditLog()
	views := make([]auditView, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditView{
			Actor:       e.Actor,
			Tool:        e.Action,
			Key:         e.Key,
			Environment: e.Environment,
			Timestamp:   e.Time,
			Result:      string(e.Result),
		})
	}
	return AuditOut{Entries: views}
}

// Marshal is a helper for MCP text results.
func Marshal(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sanitizeDetail(detail string) string {
	if detail == "" {
		return ""
	}
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "postgres://") ||
		strings.Contains(lower, "redis://") ||
		strings.Contains(lower, "sk_") ||
		strings.Contains(lower, "akia") ||
		strings.Contains(detail, "://") {
		return redact.MCPPlaceholder
	}
	return detail
}
