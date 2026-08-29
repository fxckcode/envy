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

func TestScenario1_DashboardShowsEnvVarsFooter(t *testing.T) {
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
	if !strings.Contains(view, "secrets hidden") {
		t.Fatal("missing secrets hidden status")
	}
	if !strings.Contains(view, "variables") {
		t.Fatal("missing variable count")
	}
	if strings.Contains(view, "postgres://dev") {
		t.Fatal("secret plaintext in dashboard")
	}
	if !strings.Contains(view, redact.Mask()) {
		t.Fatal("expected secret mask glyphs")
	}
}

func TestScenario2_AddVariable(t *testing.T) {
	m, p := newModel(t)
	m = send(m, key('a'))
	if m.ViewID() != tui.ViewAdd {
		t.Fatalf("view=%v", m.ViewID())
	}
	// Type key name into focused key input via runes, tab to value, type, enter.
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
	raw, ok := p.RawValue(p.ActiveEnvironment(), "EXTRA_KEY")
	if !ok || raw != "hello" {
		t.Fatalf("not saved: ok=%v raw=%q", ok, raw)
	}
}

func TestScenario3_EditRemasks(t *testing.T) {
	m, p := newModel(t)
	// Focus vars, find REDIS_URL index by scanning variables.
	vars := p.Variables()
	idx := -1
	for i, v := range vars {
		if v.Key == "REDIS_URL" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("REDIS_URL missing")
	}
	for i := 0; i < idx; i++ {
		m = send(m, keyStr("down"))
	}
	m = send(m, key('e'))
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

func TestScenario5_SwitchEnvironmentUpdatesHeader(t *testing.T) {
	m, p := newModel(t)
	m = send(m, keyStr("tab")) // focus envs
	// development is first; move to staging (index 2 alphabetically: development, production, staging)
	envs := p.EnvironmentNames()
	target := "staging"
	cur := 0
	for i, e := range envs {
		if e == p.ActiveEnvironment() {
			cur = i
			break
		}
	}
	targetIdx := 0
	for i, e := range envs {
		if e == target {
			targetIdx = i
			break
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

func TestScenario6_CompareView(t *testing.T) {
	m, _ := newModel(t)
	m = send(m, key('c'))
	if m.ViewID() != tui.ViewCompare {
		t.Fatalf("view=%v", m.ViewID())
	}
	view := m.View()
	if !strings.Contains(view, "Compare") {
		t.Fatal("missing compare title")
	}
	if !strings.Contains(view, "⚠") {
		t.Fatal("expected warnings")
	}
	if strings.Contains(view, "sk_test") || strings.Contains(view, "postgres://") {
		t.Fatal("secrets in compare view")
	}
}

func TestScenario7_ValidateView(t *testing.T) {
	m, p := newModel(t)
	_ = p.SelectEnvironment("production")
	m = send(m, key('v'))
	if m.ViewID() != tui.ViewValidate {
		t.Fatalf("view=%v", m.ViewID())
	}
	view := m.View()
	if !strings.Contains(view, "missing") && !strings.Contains(view, "invalid") {
		t.Fatalf("expected findings: %s", view)
	}
	if strings.Contains(view, "postgres://prod") {
		t.Fatal("secret in validation view")
	}
}

func TestScenario8_ProvidersView(t *testing.T) {
	m, _ := newModel(t)
	m = send(m, key('p'))
	view := m.View()
	if !strings.Contains(view, "Providers") {
		t.Fatal("missing providers")
	}
	if !strings.Contains(view, "aws-secrets-manager") {
		t.Fatal("missing provider path metadata")
	}
	if !strings.Contains(view, "file:.env") {
		t.Fatal("missing file source")
	}
}

func TestScenario9_AgentActivityView(t *testing.T) {
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

func TestScenario10_12_ApprovalAllowOnce(t *testing.T) {
	m, p := newModel(t)
	before, _ := p.RawValue("development", "REDIS_URL")
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://once", "fix")
	m = m.ShowApproval()
	view := m.View()
	if !strings.Contains(view, "Allow once") || !strings.Contains(view, "Deny") {
		t.Fatalf("missing actions: %s", view)
	}
	if !strings.Contains(view, redact.Placeholder) {
		t.Fatal("expected masked values")
	}
	if strings.Contains(view, "redis://once") || strings.Contains(view, before) {
		t.Fatal("cleartext in approval modal")
	}
	m = send(m, key('a'))
	val, _ := p.RawValue("development", "REDIS_URL")
	if val != "redis://once" {
		t.Fatalf("not applied: %q", val)
	}
	// subsequent still requires approval
	_, _ = p.EnqueueApproval("Claude Code", "development", "REDIS_URL", "redis://twice", "again")
	if len(p.PendingApprovals()) == 0 {
		t.Fatal("expected pending after allow-once")
	}
	val, _ = p.RawValue("development", "REDIS_URL")
	if val != "redis://once" {
		t.Fatal("second write applied early")
	}
}

func TestScenario11_Deny(t *testing.T) {
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

func TestScenario13_ProtectedBlocksEdit(t *testing.T) {
	m, p := newModel(t)
	_ = p.SelectEnvironment("production")
	m = send(m, key('e'))
	if m.ViewID() != tui.ViewBlocked {
		t.Fatalf("expected blocked view, got %v (%s)", m.ViewID(), m.View())
	}
	if !strings.Contains(m.BlockMessage(), "explicit authorization is required") {
		t.Fatalf("msg=%q", m.BlockMessage())
	}
	view := m.View()
	if !strings.Contains(view, "protected") && !strings.Contains(view, "authorization") {
		t.Fatalf("view=%s", view)
	}
}

func TestScenario14_QuitWithoutPartialWrites(t *testing.T) {
	m, p := newModel(t)
	before, _ := p.RawValue("development", "PORT")
	m = send(m, key('e'))
	// type something but quit from form via esc first path; scenario: quit when no modal
	m = send(m, keyStr("esc"))
	m = send(m, key('q'))
	if !m.QuitRequested() {
		t.Fatal("expected quit")
	}
	after, _ := p.RawValue("development", "PORT")
	if after != before {
		t.Fatal("partial edit was written")
	}
}
