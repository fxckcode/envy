package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
	"github.com/fxckcode/envy/internal/tui"
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

func newModel(t *testing.T) (tui.Model, *project.Project) {
	t.Helper()
	p, err := project.Open(sampleRoot(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	m := tui.New(p)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(tui.Model), p
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyStr(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m tui.Model, msgs ...tea.Msg) tui.Model {
	var model tea.Model = m
	for _, msg := range msgs {
		model, _ = model.Update(msg)
	}
	return model.(tui.Model)
}

func selectVar(m tui.Model, p *project.Project, keyName string) tui.Model {
	vars := p.Variables()
	idx := -1
	for i, v := range vars {
		if v.Key == keyName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return m
	}
	for i := 0; i < idx; i++ {
		m = send(m, keyStr("down"))
	}
	return m
}

// Scenario 1: TUI opens with project name, current env, variable list, status footer.
func TestScenario1_DashboardChrome(t *testing.T) {
	m, p := newModel(t)
	view := m.View()
	if !strings.Contains(view, "ENVY") {
		t.Fatal("missing chrome")
	}
	if !strings.Contains(view, p.Name()) {
		t.Fatal("missing project name")
	}
	if !strings.Contains(view, "ENV:") {
		t.Fatal("missing env badge")
	}
	if !strings.Contains(view, "variables") {
		t.Fatal("missing variable count")
	}
	if !strings.Contains(view, "Variables") {
		t.Fatal("missing variables pane")
	}
}

// Scenario 2: Secrets render as masked glyphs; plaintext not shown by default.
func TestScenario2_SecretsMasked(t *testing.T) {
	m, _ := newModel(t)
	view := m.View()
	if strings.Contains(view, "postgres://dev") || strings.Contains(view, "sk_test") {
		t.Fatal("secret plaintext in dashboard")
	}
	if !strings.Contains(view, redact.Mask()) {
		t.Fatal("expected secret mask glyphs")
	}
	if !strings.Contains(view, "secrets hidden") {
		t.Fatal("missing secrets hidden status")
	}
}

// Scenario 3: Selecting staging refreshes variables and header ENV: staging.
func TestScenario3_SwitchEnvironment(t *testing.T) {
	m, p := newModel(t)
	m = send(m, keyStr("tab")) // focus envs
	envs := p.EnvironmentNames()
	target := "staging"
	cur, targetIdx := 0, 0
	for i, e := range envs {
		if e == p.ActiveEnvironment() {
			cur = i
		}
		if e == target {
			targetIdx = i
		}
	}
	for cur < targetIdx {
		m = send(m, keyStr("down"))
		cur++
	}
	for cur > targetIdx {
		m = send(m, keyStr("up"))
		cur--
	}
	m = send(m, keyStr("enter"))
	if p.ActiveEnvironment() != "staging" {
		t.Fatalf("active=%s", p.ActiveEnvironment())
	}
	if !strings.Contains(m.View(), "ENV: staging") {
		t.Fatalf("header not updated: %s", m.View())
	}
}

// Scenario 4: Add variable persists to active environment provider.
func TestScenario4_AddVariable(t *testing.T) {
	m, p := newModel(t)
	m = send(m, key('a'))
	if m.ViewID() != tui.ViewAdd {
		t.Fatalf("view=%v", m.ViewID())
	}
	for _, r := range "EXTRA_KEY" {
		m = send(m, key(r))
	}
	m = send(m, keyStr("tab"))
	for _, r := range "hello" {
		m = send(m, key(r))
	}
	m = send(m, keyStr("enter"))
	if m.ViewID() != tui.ViewDashboard {
		t.Fatalf("expected dashboard after save, got %v", m.ViewID())
	}
	if !strings.Contains(m.View(), "EXTRA_KEY") {
		t.Fatal("added key not visible in list")
	}
	raw, ok := p.RawValue(p.ActiveEnvironment(), "EXTRA_KEY")
	if !ok || raw != "hello" {
		t.Fatalf("not saved: ok=%v raw=%q", ok, raw)
	}
}

// Scenario 5: Edit non-secret shows cleartext and persists.
func TestScenario5_EditNonSecretCleartext(t *testing.T) {
	m, p := newModel(t)
	m = selectVar(m, p, "AWS_REGION")
	m = send(m, key('e'))
	if m.ViewID() != tui.ViewEdit {
		t.Fatalf("view=%v", m.ViewID())
	}
	for _, r := range "eu-central-1" {
		m = send(m, key(r))
	}
	m = send(m, keyStr("enter"))
	view := m.View()
	if !strings.Contains(view, "eu-central-1") {
		t.Fatal("updated non-secret not shown in clear text")
	}
	raw, _ := p.RawValue("development", "AWS_REGION")
	if raw != "eu-central-1" {
		t.Fatalf("raw=%q", raw)
	}
}

// Scenario 6: Edit secret masks prior value; list stays masked after persist.
func TestScenario6_EditSecretRemasks(t *testing.T) {
	m, p := newModel(t)
	m = selectVar(m, p, "REDIS_URL")
	m = send(m, key('e'))
	form := m.View()
	if strings.Contains(form, "redis://localhost") {
		t.Fatal("prior secret value visible in editor")
	}
	for _, r := range "redis://edited" {
		m = send(m, key(r))
	}
	m = send(m, keyStr("enter"))
	view := m.View()
	if strings.Contains(view, "redis://edited") {
		t.Fatal("edited secret visible in list")
	}
	raw, _ := p.RawValue("development", "REDIS_URL")
	if raw != "redis://edited" {
		t.Fatalf("raw=%q", raw)
	}
}

// Scenario 7: Delete with confirmation removes key and updates footer counts.
func TestScenario7_DeleteVariable(t *testing.T) {
	m, p := newModel(t)
	before := p.Status().VariableCount
	m = selectVar(m, p, "DEBUG")
	m = send(m, key('x'))
	if m.ViewID() != tui.ViewConfirmDelete {
		t.Fatalf("expected confirm delete, got %v", m.ViewID())
	}
	m = send(m, key('y'))
	if _, ok := p.RawValue("development", "DEBUG"); ok {
		t.Fatal("DEBUG still present after delete")
	}
	after := p.Status().VariableCount
	if after != before-1 {
		t.Fatalf("footer count: before=%d after=%d", before, after)
	}
	if !strings.Contains(m.View(), "variables") {
		t.Fatal("missing updated footer")
	}
}

// Scenario 8: Compare across development, staging, production with presence + warnings.
func TestScenario8_CompareAllEnvironments(t *testing.T) {
	m, _ := newModel(t)
	m = send(m, key('c'))
	if m.ViewID() != tui.ViewCompare {
		t.Fatalf("view=%v", m.ViewID())
	}
	view := m.View()
	if !strings.Contains(view, "Compare") {
		t.Fatal("missing compare title")
	}
	for _, env := range []string{"development", "staging", "production"} {
		if !strings.Contains(strings.ToLower(view), env) && !strings.Contains(view, strings.ToUpper(env[:3])) {
			// Accept full name or abbreviated headers.
			if !strings.Contains(view, env) {
				t.Fatalf("missing env column for %s in:\n%s", env, view)
			}
		}
	}
	if !strings.Contains(view, "⚠") {
		t.Fatal("expected warnings")
	}
	if strings.Contains(view, "sk_test") || strings.Contains(view, "postgres://") {
		t.Fatal("secrets in compare view")
	}
	// Non-secret divergent DEBUG values may appear in cleartext.
	if !strings.Contains(view, "true") && !strings.Contains(view, "false") && !strings.Contains(view, "≠") {
		t.Fatal("expected divergent non-secret indication")
	}
}

// Scenario 9: Validation findings in status area without revealing secrets.
func TestScenario9_ValidateStatusArea(t *testing.T) {
	m, p := newModel(t)
	_ = p.SelectEnvironment("production")
	// Resync model after external select.
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = send(m, key('v'))
	view := m.View()
	status := m.StatusMessage()
	combined := view + "\n" + status
	if !strings.Contains(combined, "missing") && !strings.Contains(combined, "invalid") {
		t.Fatalf("expected findings in status area: view=%s status=%s", view, status)
	}
	if strings.Contains(combined, "postgres://prod") {
		t.Fatal("secret in validation status")
	}
	// Findings live in status area / dashboard footer — not a secret-leaking dump.
	if m.ViewID() != tui.ViewDashboard && m.ViewID() != tui.ViewValidate {
		t.Fatalf("unexpected view %v", m.ViewID())
	}
}

// Scenario 10: Provider detail shows identity/path without secret payloads.
func TestScenario10_ProvidersView(t *testing.T) {
	m, _ := newModel(t)
	m = send(m, key('p'))
	view := m.View()
	if !strings.Contains(view, "Providers") {
		t.Fatal("missing providers")
	}
	if !strings.Contains(view, "aws-secrets-manager") {
		t.Fatal("missing provider identity")
	}
	if !strings.Contains(view, "sample-api/production") && !strings.Contains(view, "file:.env") {
		t.Fatal("missing path/metadata")
	}
	if strings.Contains(view, "postgres://") || strings.Contains(view, "sk_test") {
		t.Fatal("secret payload in providers")
	}
}

// Scenario 11: Agent activity chronological audit with secrets redacted.
func TestScenario11_AgentActivity(t *testing.T) {
	m, p := newModel(t)
	_, _ = p.EnqueueApproval("Claude Code", "development", "PORT", "9", "reason")
	m = send(m, key('g'))
	view := m.View()
	if !strings.Contains(view, "Agent Activity") {
		t.Fatal("missing activity")
	}
	if !strings.Contains(view, "Claude Code") {
		t.Fatal("missing actor")
	}
	if strings.Contains(view, "redis://") {
		t.Fatal("secret in activity")
	}
}

// Scenario 12: Approval modal shows agent, env.key, masked old, proposed new, reason, actions.
func TestScenario12_ApprovalModalContents(t *testing.T) {
	m, p := newModel(t)
	before, _ := p.RawValue("development", "REDIS_URL")
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://proposed", "fix redis")
	m = m.ShowApproval()
	view := m.View()
	if !strings.Contains(view, "Claude Code") {
		t.Fatal("missing agent")
	}
	if !strings.Contains(view, "development.REDIS_URL") {
		t.Fatal("missing env.key")
	}
	if !strings.Contains(view, redact.Placeholder) {
		t.Fatal("expected masked old value")
	}
	if strings.Contains(view, before) {
		t.Fatal("old secret cleartext in modal")
	}
	if !strings.Contains(view, "redis://proposed") {
		t.Fatal("proposed new value should be visible for approval")
	}
	if !strings.Contains(view, "fix redis") {
		t.Fatal("missing reason")
	}
	if !strings.Contains(view, "Allow once") || !strings.Contains(view, "Allow for") || !strings.Contains(view, "Deny") {
		t.Fatalf("missing actions: %s", view)
	}
}

// Scenario 13: Deny leaves value unchanged and records denial.
func TestScenario13_Deny(t *testing.T) {
	m, p := newModel(t)
	before, _ := p.RawValue("development", "REDIS_URL")
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://denied", "x")
	m = m.ShowApproval()
	m = send(m, key('d'))
	after, _ := p.RawValue("development", "REDIS_URL")
	if after != before {
		t.Fatalf("changed on deny")
	}
	if m.StatusMessage() != "Denied" {
		t.Fatalf("status=%q", m.StatusMessage())
	}
}

// Scenario 14: Allow once applies only that request; subsequent still need approval.
func TestScenario14_AllowOnce(t *testing.T) {
	m, p := newModel(t)
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://once", "fix")
	m = m.ShowApproval()
	m = send(m, key('a'))
	val, _ := p.RawValue("development", "REDIS_URL")
	if val != "redis://once" {
		t.Fatalf("not applied: %q", val)
	}
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://twice", "again")
	if len(p.PendingApprovals()) == 0 {
		t.Fatal("expected pending after allow-once")
	}
	val, _ = p.RawValue("development", "REDIS_URL")
	if val != "redis://once" {
		t.Fatal("second write applied early")
	}
}

// Scenario 15: Allow for project grants matching writes until revoked.
func TestScenario15_AllowForProject(t *testing.T) {
	m, p := newModel(t)
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://granted", "first")
	m = m.ShowApproval()
	m = send(m, key('p'))
	if m.StatusMessage() != "Allowed for project" {
		t.Fatalf("status=%q", m.StatusMessage())
	}
	val, _ := p.RawValue("development", "REDIS_URL")
	if val != "redis://granted" {
		t.Fatalf("first write not applied: %q", val)
	}
	// Matching subsequent write in same env scope proceeds without modal.
	_, err := p.EnqueueApproval("Claude Code", "development", "PORT", "9999", "follow-up")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if len(p.PendingApprovals()) != 0 {
		t.Fatal("expected auto-apply under project grant")
	}
	port, _ := p.RawValue("development", "PORT")
	if port != "9999" {
		t.Fatalf("follow-up not applied: %q", port)
	}
}
