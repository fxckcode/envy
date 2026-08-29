package mcpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxckcode/envy/internal/mcpapi"
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
	// Doctor gitignore check expects a git repo.
	_ = os.MkdirAll(filepath.Join(dst, ".git"), 0o755)
	return dst
}

func openSvc(t *testing.T, actor string) (*mcpapi.Service, *project.Project) {
	t.Helper()
	p, err := project.Open(sampleRoot(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return mcpapi.New(p, actor), p
}

func assertNoSecretLeak(t *testing.T, blob string) {
	t.Helper()
	leaks := []string{
		"postgres://dev:", "postgres://stg:", "postgres://prod:",
		"sk_test_dev", "sk_test_staging", "AKIAIOSFODNN7EXAMPLE",
		"super-secret-password",
	}
	for _, leak := range leaks {
		if strings.Contains(blob, leak) {
			t.Fatalf("secret leaked in response: %q found in %s", leak, blob)
		}
	}
}

// 1. env_list — keys + secret flags; secret values "[REDACTED]"
func TestScenario1_EnvListRedactsSecrets(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res, err := svc.List("development")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Keys) == 0 {
		t.Fatal("expected keys")
	}
	foundDB := false
	for _, k := range res.Keys {
		if k.Key == "DATABASE_URL" {
			foundDB = true
			if !k.Secret {
				t.Fatal("DATABASE_URL should be secret")
			}
			if k.Value != redact.MCPPlaceholder {
				t.Fatalf("value = %q, want %s", k.Value, redact.MCPPlaceholder)
			}
		}
	}
	if !foundDB {
		t.Fatal("DATABASE_URL missing from list")
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 2. env_list_environments — configured names without secret values
func TestScenario2_EnvListEnvironments(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res := svc.ListEnvironments()
	if len(res.Environments) < 3 {
		t.Fatalf("want >=3 envs, got %d", len(res.Environments))
	}
	names := map[string]project.EnvInfo{}
	for _, e := range res.Environments {
		names[e.Name] = e
	}
	for _, n := range []string{"development", "staging", "production"} {
		if _, ok := names[n]; !ok {
			t.Fatalf("missing env %s", n)
		}
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 3. env_get_schema — types, required, defaults, secret markers; no live secrets
func TestScenario3_EnvGetSchema(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res := svc.GetSchema()
	foundDB, foundPort := false, false
	for _, f := range res.Fields {
		if f.Key == "DATABASE_URL" {
			foundDB = true
			if f.Type != "url" || !f.Required || !f.Secret {
				t.Fatalf("DATABASE_URL schema = %+v", f)
			}
		}
		if f.Key == "PORT" {
			foundPort = true
			if f.Type != "integer" || f.Default != "3000" {
				t.Fatalf("PORT schema = %+v", f)
			}
		}
	}
	if !foundDB || !foundPort {
		t.Fatal("schema missing DATABASE_URL or PORT")
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 4. env_check — REDIS_URL + SENTRY_DSN missing, PORT non-integer → invalid
func TestScenario4_EnvCheckInvalidMissing(t *testing.T) {
	root := sampleRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PORT=abc\nAWS_REGION=us-east-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	svc := mcpapi.New(p, "default")
	res, err := svc.Check("development")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res.Status != "invalid" {
		t.Fatalf("status = %q", res.Status)
	}
	missing := map[string]bool{}
	for _, m := range res.Missing {
		missing[m.Key] = true
	}
	for _, k := range []string{"REDIS_URL", "SENTRY_DSN"} {
		if !missing[k] {
			t.Fatalf("expected missing %s in %+v", k, res.Missing)
		}
	}
	foundPort := false
	for _, inv := range res.Invalid {
		if inv.Key == "PORT" {
			foundPort = true
			if inv.Reason == "" && inv.Message == "" {
				t.Fatal("PORT invalid needs reason")
			}
		}
	}
	if !foundPort {
		t.Fatalf("expected invalid PORT: %+v", res.Invalid)
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 5. env_diff — presence + non-secret value diffs; secrets redacted
func TestScenario5_EnvDiff(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res, err := svc.Diff("staging", "production")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(res.Keys) == 0 {
		t.Fatal("expected keys")
	}
	stripe := res.Cells["STRIPE_SECRET"]
	if stripe == nil {
		t.Fatal("missing STRIPE_SECRET row")
	}
	if stripe["staging"].Kind != project.CellOnly {
		t.Fatalf("staging STRIPE_SECRET kind = %s", stripe["staging"].Kind)
	}
	if stripe["production"].Kind != project.CellAbsent {
		t.Fatalf("production STRIPE_SECRET kind = %s", stripe["production"].Kind)
	}
	if stripe["staging"].Value != redact.MCPPlaceholder {
		t.Fatalf("secret value = %q, want redacted", stripe["staging"].Value)
	}
	aws := res.Cells["AWS_REGION"]
	if aws["staging"].Kind != project.CellDiff || aws["production"].Kind != project.CellDiff {
		t.Fatalf("AWS_REGION should differ: %+v", aws)
	}
	if aws["staging"].Value == "" || aws["production"].Value == "" {
		t.Fatal("diff should include non-secret value sides")
	}
	if aws["staging"].Value == aws["production"].Value {
		t.Fatal("differing AWS_REGION values should not match")
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 6. env_exists — true without revealing value
func TestScenario6_EnvExists(t *testing.T) {
	svc, _ := openSvc(t, "default")
	yes, err := svc.Exists("development", "DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if !yes.Exists {
		t.Fatal("DATABASE_URL should exist")
	}
	blob, _ := mcpapi.Marshal(yes)
	assertNoSecretLeak(t, blob)
	if strings.Contains(blob, "postgres://") {
		t.Fatal("exists must not include value")
	}
}

// 7. env_metadata — key, status, type, secret, source, value "[REDACTED]"
func TestScenario7_EnvMetadataSecret(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res, err := svc.Metadata("development", "DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if res.Key != "DATABASE_URL" {
		t.Fatalf("key = %q", res.Key)
	}
	if res.Status != "configured" {
		t.Fatalf("status = %q", res.Status)
	}
	if res.Type != "url" || !res.Secret {
		t.Fatalf("meta = %+v", res)
	}
	if res.Source == "" {
		t.Fatal("expected source")
	}
	if res.Value != redact.MCPPlaceholder {
		t.Fatalf("value = %q, want %s", res.Value, redact.MCPPlaceholder)
	}
}

// 8. default permissions — reading secret plaintext is denied
func TestScenario8_ReadSecretPlaintextDenied(t *testing.T) {
	svc, _ := openSvc(t, "default")
	val, err := svc.ReadValue("development", "DATABASE_URL")
	if err == nil {
		t.Fatal("expected denial")
	}
	if val != "" {
		t.Fatalf("must not return secret, got %q", val)
	}
	if !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "denied") && !strings.Contains(err.Error(), "read_values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 9. write on development without read_values — set applies or queues; never echoes other secrets
func TestScenario9_EnvSetWithWriteNoEcho(t *testing.T) {
	svc, p := openSvc(t, "writer-agent")
	res, err := svc.Set("development", "REDIS_URL", "redis://localhost:6380", "Redis connection is missing")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if res.Status != "applied" && res.Status != "pending_approval" {
		t.Fatalf("status = %q", res.Status)
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
	if strings.Contains(blob, "redis://localhost:6380") {
		t.Fatal("set response must not echo the written value without read_values")
	}
	if res.Status == "applied" {
		raw, ok := p.RawValue("development", "REDIS_URL")
		if !ok || raw != "redis://localhost:6380" {
			t.Fatalf("stored = %q ok=%v", raw, ok)
		}
	}
}

// 10. production write denied — unauthorized
func TestScenario10_EnvSetProductionUnauthorized(t *testing.T) {
	svc, p := openSvc(t, "writer-agent")
	res, err := svc.Set("production", "AWS_REGION", "eu-west-1", "need EU region")
	if err == nil && res.OK {
		t.Fatal("expected rejection")
	}
	if res.Status != "unauthorized" && res.Status != "denied" && res.Status != "policy_error" {
		t.Fatalf("status = %q", res.Status)
	}
	raw, _ := p.RawValue("production", "AWS_REGION")
	if raw != "us-west-2" {
		t.Fatalf("production mutated: %q", raw)
	}
}

// 11. delete denied — key unchanged
func TestScenario11_EnvDeleteDenied(t *testing.T) {
	svc, p := openSvc(t, "write-only-agent")
	before, ok := p.RawValue("development", "PORT")
	if !ok {
		t.Fatal("PORT should exist")
	}
	res, err := svc.Delete("development", "PORT")
	if err == nil && res.OK {
		t.Fatal("expected delete rejection")
	}
	if res.Status != "denied" && res.Status != "unauthorized" {
		t.Fatalf("status = %q", res.Status)
	}
	after, ok := p.RawValue("development", "PORT")
	if !ok || after != before {
		t.Fatalf("key must remain: before=%q after=%q ok=%v", before, after, ok)
	}
}

// 12. env_copy non-secret within allowed scope
func TestScenario12_EnvCopy(t *testing.T) {
	svc, p := openSvc(t, "writer-agent")
	res, err := svc.Copy("staging", "development", "DEBUG")
	if err != nil || !res.OK {
		t.Fatalf("copy: %+v %v", res, err)
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
	raw, ok := p.RawValue("development", "DEBUG")
	if !ok || raw != "false" {
		t.Fatalf("copied value = %q ok=%v", raw, ok)
	}
}

// 13. env_generate_example — keys + non-secret defaults only
func TestScenario13_EnvGenerateExample(t *testing.T) {
	svc, _ := openSvc(t, "default")
	res := svc.GenerateExample()
	if res.Payload["PORT"] != "3000" {
		t.Fatalf("PORT default = %q", res.Payload["PORT"])
	}
	if res.Payload["DATABASE_URL"] != "" {
		t.Fatalf("secret should be empty placeholder, got %q", res.Payload["DATABASE_URL"])
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
}

// 14. env_doctor — structured checks + score; sensitive findings redacted
func TestScenario14_EnvDoctor(t *testing.T) {
	root := sampleRoot(t)
	example := "DATABASE_URL=\nREDIS_URL=\n# leaked\nAWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE\n"
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte(example), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	svc := mcpapi.New(p, "default")
	res := svc.Doctor()
	if res.Score < 0 || res.Score > 100 {
		t.Fatalf("score = %d", res.Score)
	}
	if len(res.Checks) == 0 {
		t.Fatal("expected checks")
	}
	foundLeak := false
	for _, c := range res.Checks {
		if c.Name == "leaked_secret_in_example" && c.Status == "fail" {
			foundLeak = true
			if c.Key != "AWS_SECRET_ACCESS_KEY" {
				t.Fatalf("key = %q", c.Key)
			}
		}
	}
	if !foundLeak {
		t.Fatalf("expected leaked_secret_in_example finding: %+v", res.Checks)
	}
	blob, _ := mcpapi.Marshal(res)
	assertNoSecretLeak(t, blob)
	if strings.Contains(blob, "AKIA") {
		t.Fatal("leak value must not appear — key only")
	}
}

// 15. audit — actor, tool, key/env, timestamp, result; no plaintext secrets
func TestScenario15_AuditRecords(t *testing.T) {
	svc, _ := openSvc(t, "default")
	_, _ = svc.List("development")
	_, _ = svc.Metadata("development", "DATABASE_URL")
	_, _ = svc.Check("development")
	_, _ = svc.Set("development", "PORT", "8080", "bump port")
	audit := svc.Audit()
	if len(audit.Entries) < 3 {
		t.Fatalf("expected audit entries, got %d", len(audit.Entries))
	}
	foundMeta, foundSet := false, false
	for _, e := range audit.Entries {
		if e.Actor == "" || e.Tool == "" || e.Timestamp.IsZero() || e.Result == "" {
			t.Fatalf("incomplete audit entry: %+v", e)
		}
		if e.Tool == "env_metadata" {
			foundMeta = true
			if e.Key != "DATABASE_URL" || e.Environment != "development" {
				t.Fatalf("metadata audit key/env = %+v", e)
			}
		}
		if e.Tool == "env_set" {
			foundSet = true
			if e.Key != "PORT" || e.Environment != "development" {
				t.Fatalf("set audit key/env = %+v", e)
			}
		}
	}
	if !foundMeta || !foundSet {
		t.Fatal("expected metadata and set audit entries with key/environment")
	}
	blob, _ := mcpapi.Marshal(audit)
	assertNoSecretLeak(t, blob)
}
