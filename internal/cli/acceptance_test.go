package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fxckcode/envy/internal/cli"
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
	// Ensure .gitignore covers .env for healthy doctor baseline.
	_ = os.WriteFile(filepath.Join(dst, ".gitignore"), []byte(".env\n.env.*\n!.env.example\n"), 0o644)
	return dst
}

func runCLI(t *testing.T, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	code = cli.Execute(append([]string{"--dir", root}, args...), &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

func TestListMasksSecrets(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 1: envy list for development — schema secrets redacted.
	out, _, code := runCLI(t, root, "list", "--env", "development")
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "ENV: development") {
		t.Fatalf("expected development env header:\n%s", out)
	}
	if !strings.Contains(out, "REDIS_URL") {
		t.Fatalf("expected REDIS_URL in list:\n%s", out)
	}
	if !strings.Contains(out, redact.Mask()) {
		t.Fatalf("expected masked secret:\n%s", out)
	}
	if strings.Contains(out, "postgres://") || strings.Contains(out, "sk_test") || strings.Contains(out, "redis://") {
		t.Fatalf("secret plaintext leaked:\n%s", out)
	}
}

func TestCheckMissingKeysNonZeroNoSecrets(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 2: required REDIS_URL absent → check --env development non-zero.
	envPath := filepath.Join(root, ".env")
	data, _ := os.ReadFile(envPath)
	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, l := range lines {
		if strings.HasPrefix(l, "REDIS_URL=") {
			continue
		}
		kept = append(kept, l)
	}
	_ = os.WriteFile(envPath, []byte(strings.Join(kept, "\n")), 0o600)

	out, errOut, code := runCLI(t, root, "check", "--env", "development")
	if code == 0 {
		t.Fatalf("expected non-zero, got 0\nout=%s\nerr=%s", out, errOut)
	}
	combined := out + errOut
	if !strings.Contains(combined, "REDIS_URL") {
		t.Fatalf("expected REDIS_URL missing:\n%s", combined)
	}
	if strings.Contains(combined, "postgres://") || strings.Contains(combined, "sk_test") {
		t.Fatalf("secret leaked in check:\n%s", combined)
	}
}

func TestCheckInvalidTypeReportsReason(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 3: PORT=abc → envy check reports expected integer.
	envPath := filepath.Join(root, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "PORT=3000", "PORT=abc", 1)
	if err := os.WriteFile(envPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, root, "check")
	if code == 0 {
		t.Fatal("expected non-zero for invalid PORT")
	}
	combined := out + errOut
	if !strings.Contains(combined, "PORT") {
		t.Fatalf("expected PORT invalid:\n%s", combined)
	}
	if !strings.Contains(strings.ToLower(combined), "integer") {
		t.Fatalf("expected integer reason:\n%s", combined)
	}
	if strings.Contains(combined, "postgres://") {
		t.Fatalf("secret leaked:\n%s", combined)
	}
}

func TestDiffPresenceAndMaskedSecrets(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "diff", "staging", "production")
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "STRIPE_SECRET") {
		t.Fatalf("expected STRIPE_SECRET row:\n%s", out)
	}
	if strings.Contains(out, "sk_test") || strings.Contains(out, "postgres://") {
		t.Fatalf("secret leaked in diff:\n%s", out)
	}
}

func TestDiffWarnsMissingKey(t *testing.T) {
	root := sampleRoot(t)
	out, errOut, _ := runCLI(t, root, "diff", "staging", "production")
	combined := out + errOut
	if !strings.Contains(combined, "STRIPE_SECRET") || !strings.Contains(combined, "production") {
		t.Fatalf("expected missing key warning:\n%s", combined)
	}
	if !strings.Contains(combined, "⚠") {
		t.Fatalf("expected warn glyph:\n%s", combined)
	}
}

func TestDiffShowsNonSecretCleartext(t *testing.T) {
	root := sampleRoot(t)
	out, errOut, _ := runCLI(t, root, "diff", "staging", "production")
	combined := out + errOut
	// AWS_REGION differs: us-east-1 vs us-west-2
	if !strings.Contains(out, "us-east-1") || !strings.Contains(out, "us-west-2") {
		t.Fatalf("expected cleartext region values:\n%s", out)
	}
	if !strings.Contains(combined, "AWS_REGION") {
		t.Fatalf("expected AWS_REGION warning/row:\n%s", combined)
	}
}

func TestDoctorHealthyScore(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "doctor")
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "Score:") {
		t.Fatalf("expected health score:\n%s", out)
	}
	for _, needle := range []string{"required", "duplicat", "ignored", "✓"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(needle)) {
			t.Fatalf("expected doctor output to mention %q:\n%s", needle, out)
		}
	}
}

func TestDoctorDetectsLeakInExample(t *testing.T) {
	root := sampleRoot(t)
	example := "DATABASE_URL=postgres://leaked:secret@db/app\nREDIS_URL=\n"
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, root, "doctor")
	combined := out + errOut
	if code == 0 && !strings.Contains(combined, "✗") {
		t.Fatalf("expected fail for leaked secret:\n%s", combined)
	}
	if !strings.Contains(combined, "DATABASE_URL") || !strings.Contains(combined, ".env.example") {
		t.Fatalf("expected leaked key identification:\n%s", combined)
	}
	if strings.Contains(combined, "postgres://leaked") {
		t.Fatalf("secret value printed:\n%s", combined)
	}
}

func TestRunPropagatesEnvAndExitCode(t *testing.T) {
	root := sampleRoot(t)
	out, errOut, code := runCLI(t, root, "run", "development", "--", "sh", "-c", `printf '%s' "$REDIS_URL"; exit 42`)
	if code != 42 {
		t.Fatalf("expected exit 42, got %d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "redis://localhost:6379") {
		t.Fatalf("child did not receive env: %q", out)
	}
}

func TestGetRedactsUnlessReveal(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "get", "REDIS_URL")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(out, "redis://") {
		t.Fatalf("should be redacted: %q", out)
	}
	if !strings.Contains(out, redact.Placeholder) && !strings.Contains(out, redact.Mask()) {
		t.Fatalf("expected redaction token: %q", out)
	}
	out2, _, code2 := runCLI(t, root, "get", "REDIS_URL", "--reveal")
	if code2 != 0 {
		t.Fatalf("reveal exit=%d", code2)
	}
	if !strings.Contains(out2, "redis://localhost:6379") {
		t.Fatalf("reveal should show value: %q", out2)
	}
}

func TestSetPersistsMasked(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 6: human set --env development writes provider; list shows key configured.
	_, _, code := runCLI(t, root, "set", "REDIS_URL", "redis://localhost:6379", "--env", "development")
	if code != 0 {
		t.Fatalf("set exit=%d", code)
	}
	out, _, _ := runCLI(t, root, "get", "REDIS_URL", "--env", "development")
	if strings.Contains(out, "6379") {
		t.Fatalf("get should mask: %q", out)
	}
	listOut, _, listCode := runCLI(t, root, "list", "--env", "development")
	if listCode != 0 {
		t.Fatalf("list exit=%d", listCode)
	}
	if !strings.Contains(listOut, "REDIS_URL") {
		t.Fatalf("list should show configured key:\n%s", listOut)
	}
	if strings.Contains(listOut, "redis://") {
		t.Fatalf("list should mask secret value: %q", listOut)
	}
	for _, line := range strings.Split(listOut, "\n") {
		if strings.Contains(line, "REDIS_URL") && strings.HasPrefix(strings.TrimSpace(line), "⚠") {
			t.Fatalf("REDIS_URL should be configured (not missing):\n%s", line)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "REDIS_URL=redis://localhost:6379") {
		t.Fatalf("not persisted: %s", raw)
	}
}

func TestDeleteRemovesKey(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 7: delete --env development removes key; not present in list/check.
	_, _, code := runCLI(t, root, "delete", "REDIS_URL", "--env", "development")
	if code != 0 {
		t.Fatalf("delete exit=%d", code)
	}
	listOut, _, _ := runCLI(t, root, "list", "--env", "development")
	for _, line := range strings.Split(listOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "REDIS_URL") && !strings.HasPrefix(trimmed, "⚠") {
			t.Fatalf("REDIS_URL must not appear as present after delete:\n%s", listOut)
		}
	}
	out, errOut, checkCode := runCLI(t, root, "check", "--env", "development")
	if checkCode == 0 {
		t.Fatal("check should fail after deleting required key")
	}
	combined := out + errOut
	if !strings.Contains(combined, "REDIS_URL") {
		t.Fatalf("expected missing REDIS_URL: %s", combined)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("read .env after delete: %v", err)
	}
	if strings.Contains(string(raw), "REDIS_URL=") {
		t.Fatalf("REDIS_URL still on disk: %s", raw)
	}
}

func TestImportMergesWithoutEcho(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 9: import into development without printing secrets.
	importPath := filepath.Join(root, "import.env")
	_ = os.WriteFile(importPath, []byte("NEW_KEY=hello\nREDIS_URL=redis://imported:6379\n"), 0o600)
	out, errOut, code := runCLI(t, root, "import", importPath, "--env", "development")
	if code != 0 {
		t.Fatalf("import exit=%d out=%s err=%s", code, out, errOut)
	}
	combined := out + errOut
	if strings.Contains(combined, "redis://imported") || strings.Contains(combined, "hello") {
		// Non-secret NEW_KEY might be mentioned by key name only — values must not echo.
		if strings.Contains(combined, "redis://imported") {
			t.Fatalf("secret value echoed: %s", combined)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(raw), "NEW_KEY=hello") {
		t.Fatalf("NEW_KEY not merged: %s", raw)
	}
	if !strings.Contains(string(raw), "redis://imported:6379") {
		t.Fatalf("REDIS_URL not merged: %s", raw)
	}
}

func TestExportRedactsSecrets(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "export", "production")
	if code != 0 {
		t.Fatalf("export exit=%d out=%s", code, out)
	}
	if strings.Contains(out, "postgres://") {
		t.Fatalf("secret exported cleartext: %s", out)
	}
	if !strings.Contains(out, "AWS_REGION=us-west-2") {
		t.Fatalf("expected plain non-secret: %s", out)
	}
	if !strings.Contains(out, "DATABASE_URL=") {
		t.Fatalf("expected DATABASE_URL key: %s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "DATABASE_URL=") {
			val := strings.TrimPrefix(line, "DATABASE_URL=")
			if val != redact.Placeholder {
				t.Fatalf("DATABASE_URL should be placeholder, got %q", val)
			}
		}
	}
	revealed, _, code := runCLI(t, root, "export", "production", "--reveal")
	if code != 0 {
		t.Fatalf("export --reveal exit=%d", code)
	}
	if !strings.Contains(revealed, "postgres://") {
		t.Fatalf("reveal should include secret cleartext: %s", revealed)
	}
}

func TestDoctorFailsGitignoreAndExampleLeak(t *testing.T) {
	root := sampleRoot(t)
	// Scenario 11: .env not gitignored + secret in .env.example.
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".env.example"), []byte("DATABASE_URL=postgres://leaked:secret@db/app\n"), 0o644)
	// Initialize a git repo so gitignore check is a hard fail (not warn).
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, root, "doctor")
	combined := out + errOut
	if code == 0 {
		t.Fatalf("expected non-zero doctor: %s", combined)
	}
	if !strings.Contains(combined, "Score:") {
		t.Fatalf("expected health score: %s", combined)
	}
	if !strings.Contains(combined, "✗") {
		t.Fatalf("expected failing checks: %s", combined)
	}
	if !strings.Contains(combined, "gitignore") && !strings.Contains(combined, ".gitignore") {
		t.Fatalf("expected gitignore failure: %s", combined)
	}
	if !strings.Contains(combined, "DATABASE_URL") || !strings.Contains(combined, ".env.example") {
		t.Fatalf("expected example leak failure: %s", combined)
	}
	if strings.Contains(combined, "postgres://leaked") {
		t.Fatalf("secret value must not appear in report: %s", combined)
	}
}

func TestAgentWriteAllowedThenDeniedAfterRevoke(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "agent", "grant", "claude-code", "--env", "development", "--write", "--ttl", "30m")
	if code != 0 {
		t.Fatalf("grant exit=%d out=%s", code, out)
	}
	_, _, code = runCLI(t, root, "set", "--as-agent", "claude-code", "--env", "development", "REDIS_URL", "redis://agent:6379")
	if code != 0 {
		t.Fatalf("agent write under grant should succeed, exit=%d", code)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "redis://agent:6379") {
		t.Fatalf("agent write not persisted: %s", raw)
	}
	_, _, code = runCLI(t, root, "agent", "revoke", "claude-code")
	if code != 0 {
		t.Fatalf("revoke exit=%d", code)
	}
	out, errOut, code := runCLI(t, root, "set", "--as-agent", "claude-code", "--env", "development", "REDIS_URL", "redis://denied:6379")
	if code == 0 {
		t.Fatal("agent write after revoke should be denied")
	}
	combined := out + errOut
	if !strings.Contains(strings.ToLower(combined), "denied") && !strings.Contains(strings.ToLower(combined), "grant") {
		t.Fatalf("expected denial message: %s", combined)
	}
	raw, err = os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "redis://denied:6379") {
		t.Fatal("denied write must not persist")
	}
}

func TestAgentGrantExpiresViaCLI(t *testing.T) {
	// Scenario 13: write allowed until TTL expiry (CLI surface).
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "agent", "grant", "claude-code", "--env", "development", "--write", "--ttl", "100ms")
	if code != 0 {
		t.Fatalf("grant exit=%d out=%s", code, out)
	}
	_, _, code = runCLI(t, root, "set", "--as-agent", "claude-code", "--env", "development", "REDIS_URL", "redis://before-ttl:6379")
	if code != 0 {
		t.Fatalf("write under grant should succeed, exit=%d", code)
	}
	// Wait past TTL before attempting another write.
	time.Sleep(250 * time.Millisecond)
	out, errOut, code := runCLI(t, root, "set", "--as-agent", "claude-code", "--env", "development", "REDIS_URL", "redis://after-ttl:6379")
	if code == 0 {
		t.Fatalf("agent write after TTL should be denied\nout=%s\nerr=%s", out, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "redis://after-ttl:6379") {
		t.Fatal("expired write must not persist")
	}
	if !strings.Contains(string(raw), "redis://before-ttl:6379") {
		t.Fatalf("pre-expiry write should remain: %s", raw)
	}
}

func TestAuditRecordsMutationWithoutSecrets(t *testing.T) {
	root := sampleRoot(t)
	_, _, code := runCLI(t, root, "set", "--env", "development", "REDIS_URL", "redis://audited:6379")
	if code != 0 {
		t.Fatalf("set exit=%d", code)
	}
	_, _, code = runCLI(t, root, "set", "--env", "production", "AWS_REGION", "eu-west-1")
	if code == 0 {
		t.Fatal("protected set should fail")
	}

	// Audit must survive process exit (fresh Open loads .envy/audit.jsonl).
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	entries := p.AgentActivity()
	if len(entries) < 2 {
		t.Fatalf("expected persisted audit entries, got %d", len(entries))
	}
	var sawOK, sawDenied bool
	for _, e := range entries {
		if e.Action == "set" && e.Result == project.AuditOK && e.Key == "REDIS_URL" && e.Environment == "development" && e.Actor != "" {
			sawOK = true
		}
		if e.Action == "set" && e.Result == project.AuditDenied && e.Environment == "production" {
			sawDenied = true
		}
		if strings.Contains(e.Detail, "redis://") {
			t.Fatalf("audit stored plaintext secret: %+v", e)
		}
		if e.Actor == "" || e.Action == "" || e.Time.IsZero() {
			t.Fatalf("incomplete audit entry: %+v", e)
		}
	}
	if !sawOK {
		t.Fatalf("missing successful set audit: %+v", entries)
	}
	if !sawDenied {
		t.Fatalf("missing denied set audit: %+v", entries)
	}
}

func TestRunAppliesNonSecretSchemaDefaults(t *testing.T) {
	root := sampleRoot(t)
	yamlPath := filepath.Join(root, "envy.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(data) + "\n  LOG_LEVEL:\n    type: string\n    default: info\n"
	if err := os.WriteFile(yamlPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, root, "run", "development", "--", "sh", "-c", `printf '%s' "$LOG_LEVEL"`)
	if code != 0 {
		t.Fatalf("run exit=%d err=%s", code, errOut)
	}
	if out != "info" {
		t.Fatalf("expected schema default LOG_LEVEL=info, got %q", out)
	}
}

func TestCheckCIFailsOnIssues(t *testing.T) {
	root := sampleRoot(t)
	_, _, code := runCLI(t, root, "check", "--env", "production", "--ci")
	if code == 0 {
		t.Fatal("CI check should fail for production (missing STRIPE_SECRET, invalid PORT)")
	}
}

func TestAgentGrantAndRevoke(t *testing.T) {
	root := sampleRoot(t)
	out, _, code := runCLI(t, root, "agent", "grant", "claude-code", "--env", "development", "--write", "--ttl", "30m")
	if code != 0 {
		t.Fatalf("grant exit=%d out=%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "write") || !strings.Contains(out, "✓") {
		t.Fatalf("expected write granted display:\n%s", out)
	}
	if !strings.Contains(out, "read_values") {
		t.Fatalf("expected read_values line:\n%s", out)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "expir") {
		t.Fatalf("expected expiry:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "read_values") && strings.Contains(line, "✓") && !strings.Contains(line, "✗") {
			t.Fatalf("read_values should be denied by default: %s", line)
		}
	}

	_, _, code = runCLI(t, root, "agent", "revoke", "claude-code")
	if code != 0 {
		t.Fatalf("revoke exit=%d", code)
	}
}

func TestProtectedMutationRejected(t *testing.T) {
	root := sampleRoot(t)
	out, errOut, code := runCLI(t, root, "set", "--env", "production", "AWS_REGION", "eu-west-1")
	if code == 0 {
		t.Fatal("expected rejection")
	}
	combined := out + errOut
	if !strings.Contains(strings.ToLower(combined), "authorization") && !strings.Contains(strings.ToLower(combined), "protected") {
		t.Fatalf("expected auth message: %s", combined)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".env.production"))
	if strings.Contains(string(raw), "eu-west-1") {
		t.Fatal("production was written despite rejection")
	}

	_, _, code = runCLI(t, root, "delete", "--env", "production", "AWS_REGION")
	if code == 0 {
		t.Fatal("delete should also be rejected")
	}
}

func TestNoConfigClearError(t *testing.T) {
	empty := t.TempDir()
	out, errOut, code := runCLI(t, empty, "check")
	if code == 0 {
		t.Fatal("expected config error")
	}
	combined := out + errOut
	if !strings.Contains(strings.ToLower(combined), "envy.yaml") && !strings.Contains(strings.ToLower(combined), "config") {
		t.Fatalf("expected clear config error: %s", combined)
	}
	_, _, code = runCLI(t, empty, "set", "FOO", "bar")
	if code == 0 {
		t.Fatal("mutating without config should fail")
	}
}
