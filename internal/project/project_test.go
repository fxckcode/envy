package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
)

func sampleRoot(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "sample-project")
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dst
}

func openSample(t *testing.T) *project.Project {
	t.Helper()
	p, err := project.Open(sampleRoot(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return p
}

func TestOpenShowsActiveEnvVariablesAndStatus(t *testing.T) {
	p := openSample(t)
	if p.ActiveEnvironment() == "" {
		t.Fatal("expected active environment")
	}
	vars := p.Variables()
	if len(vars) == 0 {
		t.Fatal("expected variables")
	}
	st := p.Status()
	if st.VariableCount == 0 {
		t.Fatal("expected variable count")
	}
	if !st.SecretsHidden {
		t.Fatal("secrets should be hidden")
	}
	for _, v := range vars {
		if v.Secret && v.Display != redact.Mask() {
			t.Fatalf("secret %s display = %q, want mask", v.Key, v.Display)
		}
		if v.Secret && strings.Contains(v.Display, "postgres://") {
			t.Fatalf("secret leaked plaintext for %s", v.Key)
		}
	}
}

func TestAddVariableMaskedIfSecret(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("development")
	if err := p.AddVariableSecret("NEW_SECRET", "super-secret", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, v := range p.Variables() {
		if v.Key == "NEW_SECRET" {
			if v.Display != redact.Mask() {
				t.Fatalf("NEW_SECRET should be masked, got %q", v.Display)
			}
		}
	}
	if err := p.EditVariable("REDIS_URL", "redis://new"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	for _, v := range p.Variables() {
		if v.Key == "REDIS_URL" {
			if v.Display != redact.Mask() {
				t.Fatalf("REDIS_URL should be masked, got %q", v.Display)
			}
		}
	}
	raw, ok := p.RawValue("development", "NEW_SECRET")
	if !ok || raw != "super-secret" {
		t.Fatalf("NEW_SECRET not saved")
	}
}

func TestEditPersistsAndRemasks(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("development")
	if err := p.EditVariable("DATABASE_URL", "postgres://edited/db"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	raw, _ := p.RawValue("development", "DATABASE_URL")
	if raw != "postgres://edited/db" {
		t.Fatalf("raw not updated: %q", raw)
	}
	for _, v := range p.Variables() {
		if v.Key == "DATABASE_URL" && v.Display != redact.Mask() {
			t.Fatalf("still must mask after save, got %q", v.Display)
		}
	}
}

func TestSecretListNeverPlaintext(t *testing.T) {
	p := openSample(t)
	for _, v := range p.Variables() {
		if !v.Secret {
			continue
		}
		if v.Display != redact.Mask() {
			t.Fatalf("%s: want mask got %q", v.Key, v.Display)
		}
	}
}

func TestSelectEnvironmentReloadsVariables(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("development")
	devRegion := ""
	for _, v := range p.Variables() {
		if v.Key == "AWS_REGION" {
			devRegion = v.Display
		}
	}
	_ = p.SelectEnvironment("production")
	if p.ActiveEnvironment() != "production" {
		t.Fatalf("active=%s", p.ActiveEnvironment())
	}
	prodRegion := ""
	for _, v := range p.Variables() {
		if v.Key == "AWS_REGION" {
			prodRegion = v.Display
		}
	}
	if devRegion == "" || prodRegion == "" {
		t.Fatal("missing AWS_REGION")
	}
	if prodRegion != "us-west-2" {
		t.Fatalf("production region=%q", prodRegion)
	}
}

func TestCompareMatrixMasksAndWarns(t *testing.T) {
	p := openSample(t)
	res, err := p.Compare("staging", "production")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "STRIPE_SECRET") {
		t.Fatalf("expected missing STRIPE_SECRET warning, got %s", joined)
	}
	blob := joined
	for _, k := range res.Keys {
		row := res.Cells[k]
		blob += row["staging"].Display + row["production"].Display
	}
	if strings.Contains(blob, "sk_test") || strings.Contains(blob, "postgres://") {
		t.Fatalf("secret plaintext leaked in compare: %s", blob)
	}
	foundDiff := false
	for _, k := range res.Keys {
		if res.Cells[k]["staging"].Kind == project.CellDiff {
			foundDiff = true
		}
	}
	if !foundDiff {
		t.Fatal("expected differing cells")
	}
}

func TestValidateHighlightsWithoutSecrets(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("production")
	res := p.Validate()
	if len(res.Missing) == 0 {
		t.Fatal("expected missing findings")
	}
	if len(res.Invalid) == 0 {
		t.Fatal("expected invalid PORT")
	}
	for _, f := range append(res.Missing, res.Invalid...) {
		if strings.Contains(f.Message, "postgres://") || strings.Contains(f.Message, "sk_") {
			t.Fatalf("secret in finding: %+v", f)
		}
	}
}

func TestProvidersMetadataOnly(t *testing.T) {
	p := openSample(t)
	metas := p.Providers()
	if len(metas) < 3 {
		t.Fatalf("expected 3 providers, got %d", len(metas))
	}
	var prod project.ProviderMeta
	for _, m := range metas {
		if m.Environment == "production" {
			prod = m
		}
		if strings.Contains(m.Source, "postgres://") {
			t.Fatalf("value leaked in provider meta: %s", m.Source)
		}
	}
	if !strings.Contains(prod.Source, "aws-secrets-manager") {
		t.Fatalf("production source=%q", prod.Source)
	}
	if !strings.Contains(prod.Source, "sample-api/production") {
		t.Fatalf("production path missing: %q", prod.Source)
	}
}

func TestAgentActivityRedacted(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("development")
	_, err := p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://agent", "fix redis")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	entries := p.AgentActivity()
	if len(entries) == 0 {
		t.Fatal("expected audit entries")
	}
	e := entries[len(entries)-1]
	if e.Actor != "Claude Code" || e.Action == "" || e.Key != "REDIS_URL" || e.Environment != "development" {
		t.Fatalf("bad entry: %+v", e)
	}
	if strings.Contains(e.Detail, "redis://agent") {
		t.Fatalf("secret in audit detail: %q", e.Detail)
	}
}

func TestApprovalModalDisplayMasked(t *testing.T) {
	p := openSample(t)
	req, err := p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://agent", "missing")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	oldD, newD := project.ApprovalDisplay(req)
	if oldD != redact.Placeholder || newD != redact.Placeholder {
		t.Fatalf("old=%q new=%q", oldD, newD)
	}
}

func TestDenyLeavesValueUnchangedAndAudits(t *testing.T) {
	p := openSample(t)
	before, _ := p.RawValue("development", "REDIS_URL")
	req, _ := p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://nope", "x")
	if err := p.RespondApproval(req.ID, project.Deny); err != nil {
		t.Fatalf("deny: %v", err)
	}
	after, _ := p.RawValue("development", "REDIS_URL")
	if after != before {
		t.Fatalf("value changed on deny: %q -> %q", before, after)
	}
	found := false
	for _, e := range p.AgentActivity() {
		if e.Action == "deny" && e.Result == project.AuditDenied {
			found = true
		}
	}
	if !found {
		t.Fatal("deny not audited")
	}
}

func TestAllowOnceAppliesExactlyOnce(t *testing.T) {
	p := openSample(t)
	req, _ := p.EnqueueApproval("Claude Code", "development", "PORT", "4000", "bump")
	if err := p.RespondApproval(req.ID, project.AllowOnce); err != nil {
		t.Fatalf("allow: %v", err)
	}
	val, _ := p.RawValue("development", "PORT")
	if val != "4000" {
		t.Fatalf("not applied: %q", val)
	}
	// Subsequent write still requires approval (queued, not auto-applied).
	req2, err := p.EnqueueApproval("Claude Code", "development", "PORT", "5000", "again")
	if err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	pending := p.PendingApprovals()
	found := false
	for _, r := range pending {
		if r.ID == req2.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("second write should still require approval")
	}
	val, _ = p.RawValue("development", "PORT")
	if val != "4000" {
		t.Fatalf("second write applied without approval: %q", val)
	}
}

func TestProtectedProductionBlocksEdit(t *testing.T) {
	p := openSample(t)
	_ = p.SelectEnvironment("production")
	err := p.EditVariable("AWS_REGION", "eu-west-1")
	if err == nil {
		t.Fatal("expected protected error")
	}
	if !strings.Contains(err.Error(), "explicit authorization is required") {
		t.Fatalf("msg=%q", err.Error())
	}
	val, _ := p.RawValue("production", "AWS_REGION")
	if val != "us-west-2" {
		t.Fatalf("value should be unchanged: %q", val)
	}
}
