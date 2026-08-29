// Package tui implements the Envy keyboard-driven terminal UI.
package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fxckcode/envy/internal/project"
)

// ViewID identifies the active full-screen or overlay surface.
type ViewID int

const (
	ViewDashboard ViewID = iota
	ViewCompare
	ViewValidate
	ViewProviders
	ViewActivity
	ViewAdd
	ViewEdit
	ViewApproval
	ViewBlocked
)

// FocusRegion is the dashboard focus target.
type FocusRegion int

const (
	FocusEnvs FocusRegion = iota
	FocusVars
)

// Model is the Bubble Tea application model.
type Model struct {
	proj   *project.Project
	width  int
	height int

	view   ViewID
	focus  FocusRegion
	envIdx int
	varIdx int

	compareLeft  string
	compareRight string
	compare      project.CompareResult

	validation project.ValidationResult
	blockMsg   string

	// forms
	keyInput   textinput.Model
	valueInput textinput.Model
	formSecret bool
	editKey    string

	approval *project.ApprovalRequest
	quit     bool
	statusMsg string
	dirtyForm bool // true while editing unsaved form — discarded on quit without save
}

// New constructs a TUI model bound to a project.
func New(p *project.Project) Model {
	envs := p.EnvironmentNames()
	envIdx := 0
	active := p.ActiveEnvironment()
	for i, n := range envs {
		if n == active {
			envIdx = i
			break
		}
	}
	ki := textinput.New()
	ki.Placeholder = "KEY"
	ki.CharLimit = 128
	vi := textinput.New()
	vi.Placeholder = "value"
	vi.CharLimit = 4096
	vi.EchoMode = textinput.EchoPassword
	vi.EchoCharacter = '•'

	return Model{
		proj:         p,
		width:        100,
		height:       30,
		view:         ViewDashboard,
		focus:        FocusVars,
		envIdx:       envIdx,
		keyInput:     ki,
		valueInput:   vi,
		compareLeft:  firstEnv(envs, active),
		compareRight: secondEnv(envs, active),
	}
}

func firstEnv(envs []string, active string) string {
	if len(envs) == 0 {
		return active
	}
	return envs[0]
}

func secondEnv(envs []string, active string) string {
	if len(envs) < 2 {
		return active
	}
	for _, e := range envs {
		if e != envs[0] {
			return e
		}
	}
	return envs[0]
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// QuitRequested reports whether the model asked to exit.
func (m Model) QuitRequested() bool {
	return m.quit
}

// ViewID returns the current view (for tests).
func (m Model) ViewID() ViewID {
	return m.view
}

// StatusMessage returns the last status/toast line.
func (m Model) StatusMessage() string {
	return m.statusMsg
}

// BlockMessage returns the protected-edit explanation, if any.
func (m Model) BlockMessage() string {
	return m.blockMsg
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Modal / overlay views first.
	switch m.view {
	case ViewApproval:
		return m.handleApprovalKey(key)
	case ViewAdd, ViewEdit:
		return m.handleFormKey(msg)
	case ViewBlocked:
		if key == "esc" || key == "enter" || key == "q" {
			m.view = ViewDashboard
			m.blockMsg = ""
		}
		return m, nil
	case ViewCompare:
		return m.handleCompareKey(key)
	case ViewValidate, ViewProviders, ViewActivity:
		if key == "esc" || key == "q" {
			if key == "q" && m.view != ViewDashboard {
				m.view = ViewDashboard
				return m, nil
			}
			if key == "esc" {
				m.view = ViewDashboard
				return m, nil
			}
		}
		if key == "q" {
			m.quit = true
			m.dirtyForm = false
			return m, tea.Quit
		}
		return m, nil
	}

	// Dashboard
	switch key {
	case "q", "ctrl+c":
		// Quit only when no modal is open (we're on dashboard).
		m.quit = true
		m.dirtyForm = false
		return m, tea.Quit
	case "tab":
		if m.focus == FocusEnvs {
			m.focus = FocusVars
		} else {
			m.focus = FocusEnvs
		}
		return m, nil
	case "up", "k":
		if m.focus == FocusEnvs {
			if m.envIdx > 0 {
				m.envIdx--
			}
		} else if m.varIdx > 0 {
			m.varIdx--
		}
		return m, nil
	case "down", "j":
		if m.focus == FocusEnvs {
			envs := m.proj.EnvironmentNames()
			if m.envIdx < len(envs)-1 {
				m.envIdx++
			}
		} else {
			vars := m.proj.Variables()
			if m.varIdx < len(vars)-1 {
				m.varIdx++
			}
		}
		return m, nil
	case "enter":
		if m.focus == FocusEnvs {
			return m.selectEnv()
		}
		return m, nil
	case "a":
		return m.beginAdd()
	case "e":
		return m.beginEdit()
	case "c":
		return m.openCompare()
	case "v":
		m.validation = m.proj.Validate()
		m.view = ViewValidate
		return m, nil
	case "p":
		m.view = ViewProviders
		return m, nil
	case "g":
		m.view = ViewActivity
		return m, nil
	case "A": // show pending approval if any (tests / agent bridge)
		return m.openPendingApproval()
	}
	return m, nil
}

func (m Model) selectEnv() (tea.Model, tea.Cmd) {
	envs := m.proj.EnvironmentNames()
	if m.envIdx < 0 || m.envIdx >= len(envs) {
		return m, nil
	}
	name := envs[m.envIdx]
	if err := m.proj.SelectEnvironment(name); err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	m.varIdx = 0
	m.statusMsg = fmt.Sprintf("switched to %s", name)
	return m, nil
}

func (m Model) beginAdd() (tea.Model, tea.Cmd) {
	if err := m.ensureWritable(); err != nil {
		return m.showBlocked(err.Error())
	}
	m.view = ViewAdd
	m.dirtyForm = true
	m.formSecret = false
	m.keyInput.SetValue("")
	m.valueInput.SetValue("")
	m.valueInput.EchoMode = textinput.EchoNormal
	m.keyInput.Focus()
	m.valueInput.Blur()
	return m, nil
}

func (m Model) beginEdit() (tea.Model, tea.Cmd) {
	if err := m.ensureWritable(); err != nil {
		return m.showBlocked(err.Error())
	}
	vars := m.proj.Variables()
	if m.varIdx < 0 || m.varIdx >= len(vars) {
		m.statusMsg = "no variable selected"
		return m, nil
	}
	v := vars[m.varIdx]
	if v.Missing {
		m.statusMsg = "cannot edit missing key; use add"
		return m, nil
	}
	m.view = ViewEdit
	m.dirtyForm = true
	m.editKey = v.Key
	m.formSecret = v.Secret
	m.keyInput.SetValue(v.Key)
	m.keyInput.Blur()
	m.valueInput.SetValue("")
	m.valueInput.EchoMode = textinput.EchoPassword
	if !v.Secret {
		m.valueInput.EchoMode = textinput.EchoNormal
	}
	m.valueInput.Focus()
	return m, nil
}

func (m Model) ensureWritable() error {
	env := m.proj.ActiveEnvironment()
	if m.proj.IsProtected(env) && !m.proj.HasElevatedTrust() {
		return fmt.Errorf("%w: %q", project.ErrProtected, env)
	}
	return nil
}

func (m Model) showBlocked(msg string) (tea.Model, tea.Cmd) {
	m.view = ViewBlocked
	m.blockMsg = msg
	m.statusMsg = msg
	return m, nil
}

func (m Model) openCompare() (tea.Model, tea.Cmd) {
	envs := m.proj.EnvironmentNames()
	if len(envs) < 2 {
		m.statusMsg = "Need two environments to compare"
		return m, nil
	}
	left := m.compareLeft
	right := m.compareRight
	if left == right {
		right = secondEnv(envs, left)
	}
	res, err := m.proj.Compare(left, right)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	m.compare = res
	m.view = ViewCompare
	return m, nil
}

func (m Model) openPendingApproval() (tea.Model, tea.Cmd) {
	pending := m.proj.PendingApprovals()
	if len(pending) == 0 {
		m.statusMsg = "no pending approvals"
		return m, nil
	}
	req := pending[0]
	m.approval = &req
	m.view = ViewApproval
	return m, nil
}

// ShowApproval opens the approval modal for the first pending request (agent bridge).
func (m Model) ShowApproval() Model {
	nm, _ := m.openPendingApproval()
	return nm.(Model)
}

func (m Model) handleCompareKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.view = ViewDashboard
	case "q":
		// Prefer back over quit from nested view when used as "leave".
		m.view = ViewDashboard
	case "r":
		res, err := m.proj.Compare(m.compare.LeftEnv, m.compare.RightEnv)
		if err != nil {
			m.statusMsg = err.Error()
		} else {
			m.compare = res
		}
	}
	return m, nil
}

func (m Model) handleApprovalKey(key string) (tea.Model, tea.Cmd) {
	if m.approval == nil {
		m.view = ViewDashboard
		return m, nil
	}
	id := m.approval.ID
	var decision project.ApprovalDecision
	switch key {
	case "a":
		decision = project.AllowOnce
	case "p":
		decision = project.AllowForProject
	case "d", "esc":
		decision = project.Deny
	case "q":
		// Quit blocked while modal open.
		m.statusMsg = "resolve approval before quitting"
		return m, nil
	default:
		return m, nil
	}
	if err := m.proj.RespondApproval(id, decision); err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	switch decision {
	case project.AllowOnce:
		m.statusMsg = "Allowed once"
	case project.AllowForProject:
		m.statusMsg = "Allowed for project"
	case project.Deny:
		m.statusMsg = "Denied"
	}
	m.approval = nil
	m.view = ViewDashboard
	return m, nil
}

func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.view = ViewDashboard
		m.dirtyForm = false
		m.statusMsg = "edit cancelled"
		return m, nil
	case "ctrl+s":
		// Toggle secret marking for add form (affects echo + post-save display via schema/heuristic).
		m.formSecret = !m.formSecret
		if m.formSecret {
			m.valueInput.EchoMode = textinput.EchoPassword
		} else {
			m.valueInput.EchoMode = textinput.EchoNormal
		}
		return m, nil
	case "enter":
		return m.submitForm()
	case "tab":
		if m.view == ViewAdd {
			if m.keyInput.Focused() {
				m.keyInput.Blur()
				m.valueInput.Focus()
			} else {
				m.valueInput.Blur()
				m.keyInput.Focus()
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.keyInput.Focused() {
		m.keyInput, cmd = m.keyInput.Update(msg)
	} else {
		m.valueInput, cmd = m.valueInput.Update(msg)
	}
	return m, cmd
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	val := m.valueInput.Value()
	if m.view == ViewAdd {
		key := strings.TrimSpace(m.keyInput.Value())
		if key == "" {
			m.statusMsg = "key is required"
			return m, nil
		}
		secret := m.formSecret || m.proj.IsSecretKey(key)
		if err := m.proj.AddVariableSecret(key, val, secret); err != nil {
			if isProtectedErr(err) {
				return m.showBlocked(err.Error())
			}
			m.statusMsg = err.Error()
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("added %s", key)
	} else {
		if err := m.proj.EditVariable(m.editKey, val); err != nil {
			if isProtectedErr(err) {
				return m.showBlocked(err.Error())
			}
			m.statusMsg = err.Error()
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("updated %s", m.editKey)
	}
	m.dirtyForm = false
	m.view = ViewDashboard
	return m, nil
}

func isProtectedErr(err error) bool {
	return errors.Is(err, project.ErrProtected)
}

// View renders the UI.
func (m Model) View() string {
	switch m.view {
	case ViewCompare:
		return m.viewCompare()
	case ViewValidate:
		return m.viewValidate()
	case ViewProviders:
		return m.viewProviders()
	case ViewActivity:
		return m.viewActivity()
	case ViewAdd, ViewEdit:
		return m.viewForm()
	case ViewApproval:
		return m.viewApproval()
	case ViewBlocked:
		return m.viewBlocked()
	default:
		return m.viewDashboard()
	}
}

var (
	styleChrome   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9AA8B8"))
	styleTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8EEF6")).Bold(true)
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9FD4"))
	styleMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7A8C"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6B84D"))
	styleSuccess  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3DCC8A"))
	styleDanger   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E85D5D"))
	styleSecret   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7A8C"))
	styleSelected = lipgloss.NewStyle().Background(lipgloss.Color("#243044")).Foreground(lipgloss.Color("#E8EEF6"))
)

func (m Model) viewDashboard() string {
	envs := m.proj.EnvironmentNames()
	active := m.proj.ActiveEnvironment()
	vars := m.proj.Variables()
	st := m.proj.Status()

	var b strings.Builder
	b.WriteString(styleChrome.Render("┌─ ENVY "))
	b.WriteString(strings.Repeat("─", max(10, m.width-12)))
	b.WriteString("┐\n")
	b.WriteString(fmt.Sprintf("│ %s", styleTitle.Render(m.proj.Name())))
	pad := m.width - len(m.proj.Name()) - len(active) - 12
	if pad < 1 {
		pad = 1
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(styleAccent.Render(fmt.Sprintf("ENV: %s", active)))
	b.WriteString(" │\n")
	b.WriteString("├" + strings.Repeat("─", 22) + "┬" + strings.Repeat("─", max(20, m.width-26)) + "┤\n")

	// Two panes
	envLines := make([]string, 0, max(len(envs), len(vars))+1)
	envLines = append(envLines, " Environments")
	for i, e := range envs {
		prefix := "   "
		line := fmt.Sprintf("%s%s", prefix, e)
		if i == m.envIdx {
			line = fmt.Sprintf(" > %s", e)
			if m.focus == FocusEnvs {
				line = styleSelected.Render(line)
			} else {
				line = styleAccent.Render(line)
			}
		} else {
			line = "   " + e
		}
		envLines = append(envLines, line)
	}

	varLines := []string{" Variables"}
	for i, v := range vars {
		val := v.Display
		if v.Secret {
			val = styleSecret.Render(val)
		}
		marker := "  "
		if v.Missing {
			marker = styleWarn.Render("⚠ ")
		} else if v.Invalid {
			marker = styleWarn.Render("⚠ ")
		}
		line := fmt.Sprintf("%s%-20s %s", marker, v.Key, val)
		if i == m.varIdx && m.focus == FocusVars {
			line = styleSelected.Render(fmt.Sprintf(" %-20s %s", v.Key, v.Display))
		}
		varLines = append(varLines, line)
	}
	if len(vars) == 0 {
		varLines = append(varLines, styleMuted.Render(" No variables. [a] add"))
	}

	rows := max(len(envLines), len(varLines))
	for i := 0; i < rows; i++ {
		left := ""
		right := ""
		if i < len(envLines) {
			left = envLines[i]
		}
		if i < len(varLines) {
			right = varLines[i]
		}
		b.WriteString(fmt.Sprintf("│%-22s│ %s\n", truncate(plainLen(left), left, 22), right))
	}

	b.WriteString("├" + strings.Repeat("─", max(20, m.width-4)) + "┤\n")
	missingSeg := styleSuccess.Render("✓ 0 missing")
	if st.MissingCount > 0 {
		missingSeg = styleWarn.Render(fmt.Sprintf("⚠ %d missing", st.MissingCount))
	}
	secretSeg := styleSuccess.Render("✓ secrets hidden")
	if !st.SecretsHidden {
		secretSeg = styleWarn.Render("⚠ secrets visible")
	}
	b.WriteString(fmt.Sprintf("│ %d variables   %s   %s\n", st.VariableCount, missingSeg, secretSeg))
	b.WriteString("├" + strings.Repeat("─", max(20, m.width-4)) + "┤\n")
	b.WriteString("│ [a] add  [e] edit  [c] compare  [v] validate  [p] providers  [g] activity  [q] quit\n")
	if m.statusMsg != "" {
		b.WriteString("│ " + styleMuted.Render(m.statusMsg) + "\n")
	}
	b.WriteString("└" + strings.Repeat("─", max(20, m.width-4)) + "┘\n")
	return b.String()
}

func (m Model) viewCompare() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("┌─ Compare: %s ↔ %s ─\n", m.compare.LeftEnv, m.compare.RightEnv))
	b.WriteString(fmt.Sprintf("│ %-20s %-12s %-12s\n", "", strings.ToUpper(m.compare.LeftEnv), strings.ToUpper(m.compare.RightEnv)))
	for _, k := range m.compare.Keys {
		row := m.compare.Cells[k]
		left := row[m.compare.LeftEnv]
		right := row[m.compare.RightEnv]
		b.WriteString(fmt.Sprintf("│ %-20s %-12s %-12s\n", k, left.Display, right.Display))
	}
	b.WriteString("├─ Warnings ─\n")
	if len(m.compare.Warnings) == 0 {
		b.WriteString("│ No differences\n")
	} else {
		for _, w := range m.compare.Warnings {
			b.WriteString("│ " + styleWarn.Render(w) + "\n")
		}
	}
	b.WriteString("│ [r] refresh  [esc] back\n")
	b.WriteString("└─\n")
	return b.String()
}

func (m Model) viewValidate() string {
	var b strings.Builder
	b.WriteString("┌─ Validations ─\n")
	if len(m.validation.Missing) == 0 && len(m.validation.Invalid) == 0 {
		b.WriteString("│ " + styleSuccess.Render("✓ all checks passed") + "\n")
	}
	for _, f := range m.validation.Missing {
		b.WriteString(fmt.Sprintf("│ %s missing: %s\n", styleWarn.Render("⚠"), f.Key))
	}
	for _, f := range m.validation.Invalid {
		b.WriteString(fmt.Sprintf("│ %s invalid: %s (%s)\n", styleWarn.Render("⚠"), f.Key, f.Message))
	}
	b.WriteString("│ [esc] back\n└─\n")
	return b.String()
}

func (m Model) viewProviders() string {
	var b strings.Builder
	b.WriteString("┌─ Providers ─\n")
	for _, p := range m.proj.Providers() {
		b.WriteString(fmt.Sprintf("│ %-16s %s\n", p.Environment, p.Source))
	}
	b.WriteString("│ [esc] back\n└─\n")
	return b.String()
}

func (m Model) viewActivity() string {
	var b strings.Builder
	b.WriteString("┌─ Agent Activity ─\n")
	entries := m.proj.AgentActivity()
	if len(entries) == 0 {
		b.WriteString("│ " + styleMuted.Render("No agent activity yet") + "\n")
	}
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("│ %s  %s  %s  %s.%s  %s\n",
			e.Time.Format("15:04"),
			e.Actor,
			e.Action,
			e.Environment,
			e.Key,
			e.Result,
		))
	}
	b.WriteString("│ [esc] back\n└─\n")
	return b.String()
}

func (m Model) viewForm() string {
	title := "Add variable"
	if m.view == ViewEdit {
		title = fmt.Sprintf("Edit %s", m.editKey)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("┌─ %s ─\n", title))
	if m.view == ViewAdd {
		b.WriteString("│ Key:   " + m.keyInput.View() + "\n")
	} else {
		b.WriteString("│ Key:   " + m.editKey + "\n")
	}
	b.WriteString("│ Value: " + m.valueInput.View() + "\n")
	if m.formSecret || m.view == ViewEdit {
		b.WriteString("│ " + styleMuted.Render("(secret values are masked while typing)") + "\n")
	}
	if m.view == ViewAdd {
		secretState := "off"
		if m.formSecret {
			secretState = "on"
		}
		b.WriteString(fmt.Sprintf("│ secret mark: %s  [ctrl+s] toggle\n", secretState))
	}
	b.WriteString("│ [enter] save  [esc] cancel\n└─\n")
	return b.String()
}

func (m Model) viewApproval() string {
	if m.approval == nil {
		return "no approval"
	}
	req := *m.approval
	oldD, newD := project.ApprovalDisplay(req)
	var b strings.Builder
	b.WriteString("┌─ Agent Permission Request ─\n")
	b.WriteString(fmt.Sprintf("│ %s wants to modify:\n", styleAccent.Render(req.Actor)))
	b.WriteString(fmt.Sprintf("│ %s.%s\n", req.Environment, req.Key))
	b.WriteString(fmt.Sprintf("│ Old value: %s\n", styleSecret.Render(oldD)))
	b.WriteString(fmt.Sprintf("│ New value: %s\n", styleSecret.Render(newD)))
	reason := req.Reason
	if reason == "" {
		reason = "—"
	}
	b.WriteString(fmt.Sprintf("│ Reason: %s\n", reason))
	b.WriteString("│ [a] Allow once  [p] Allow for this project  [d] Deny\n")
	b.WriteString("└─\n")
	return b.String()
}

func (m Model) viewBlocked() string {
	var b strings.Builder
	b.WriteString("┌─ Change blocked ─\n")
	b.WriteString("│ " + styleDanger.Render(m.blockMsg) + "\n")
	b.WriteString("│ Explicit authorization is required for protected environments.\n")
	b.WriteString("│ [esc] back\n└─\n")
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(plain int, s string, width int) string {
	// Best-effort: pad/truncate based on visible approx.
	if plain <= width {
		return s + strings.Repeat(" ", width-plain)
	}
	return s
}

func plainLen(s string) int {
	// Strip ANSI roughly by counting runes without escapes — lipgloss lengths vary;
	// for layout padding we use a simple printable estimate.
	n := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	if n == 0 {
		return len(s)
	}
	return n
}

// Run starts the Bubble Tea program.
func Run(p *project.Project) error {
	prog := tea.NewProgram(New(p), tea.WithAltScreen())
	_, err := prog.Run()
	return err
}
