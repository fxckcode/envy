// Package tui implements the Envy keyboard-driven terminal UI.
package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
)

// ViewID identifies the active full-screen or overlay surface.
type ViewID int

const (
	ViewDashboard ViewID = iota
	ViewCompare
	ViewProviders
	ViewActivity
	ViewAdd
	ViewEdit
	ViewApproval
	ViewBlocked
	ViewConfirmDelete
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

	compare project.CompareResult

	validation project.ValidationResult
	blockMsg   string
	deleteKey  string

	// forms
	keyInput   textinput.Model
	valueInput textinput.Model
	formSecret bool
	editKey    string

	approval  *project.ApprovalRequest
	quit      bool
	statusMsg string
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
		proj:     p,
		width:    100,
		height:   30,
		view:     ViewDashboard,
		focus:    FocusVars,
		envIdx:   envIdx,
		keyInput: ki,
		valueInput: vi,
	}
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

	switch m.view {
	case ViewApproval:
		return m.handleApprovalKey(key)
	case ViewAdd, ViewEdit:
		return m.handleFormKey(msg)
	case ViewConfirmDelete:
		return m.handleDeleteConfirm(key)
	case ViewBlocked:
		if key == "esc" || key == "enter" || key == "q" {
			m.view = ViewDashboard
			m.blockMsg = ""
		}
		return m, nil
	case ViewCompare:
		return m.handleCompareKey(key)
	case ViewProviders, ViewActivity:
		if key == "esc" || key == "q" {
			m.view = ViewDashboard
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		m.quit = true
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
	case "x":
		return m.beginDelete()
	case "c":
		return m.openCompare()
	case "v":
		return m.runValidation()
	case "p":
		m.view = ViewProviders
		return m, nil
	case "g":
		m.view = ViewActivity
		return m, nil
	case "A":
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
	m.formSecret = false
	m.keyInput.SetValue("")
	m.valueInput.SetValue("")
	m.valueInput.EchoMode = textinput.EchoNormal
	m.valueInput.Placeholder = "value"
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
	m.editKey = v.Key
	m.formSecret = v.Secret
	m.keyInput.SetValue(v.Key)
	m.keyInput.Blur()
	// Never prefill secret cleartext. Empty field + password echo + masked
	// placeholder so the prior value appears masked, not omitted or leaked.
	m.valueInput.SetValue("")
	if v.Secret {
		m.valueInput.EchoMode = textinput.EchoPassword
		m.valueInput.Placeholder = redact.Placeholder
	} else {
		m.valueInput.EchoMode = textinput.EchoNormal
		m.valueInput.Placeholder = "value"
	}
	m.valueInput.Focus()
	return m, nil
}

func (m Model) beginDelete() (tea.Model, tea.Cmd) {
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
		m.statusMsg = "nothing to delete"
		return m, nil
	}
	m.deleteKey = v.Key
	m.view = ViewConfirmDelete
	return m, nil
}

func (m Model) handleDeleteConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "enter":
		keyName := m.deleteKey
		if err := m.proj.DeleteVariable(keyName); err != nil {
			if isProtectedErr(err) {
				return m.showBlocked(err.Error())
			}
			m.statusMsg = err.Error()
			m.view = ViewDashboard
			m.deleteKey = ""
			return m, nil
		}
		if m.varIdx > 0 {
			m.varIdx--
		}
		m.statusMsg = fmt.Sprintf("deleted %s", keyName)
		m.deleteKey = ""
		m.view = ViewDashboard
		return m, nil
	case "n", "esc":
		m.statusMsg = "delete cancelled"
		m.deleteKey = ""
		m.view = ViewDashboard
		return m, nil
	case "q":
		m.statusMsg = "resolve confirmation before quitting"
		return m, nil
	}
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
	res, err := m.proj.CompareAll(envs...)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	m.compare = res
	m.view = ViewCompare
	return m, nil
}

func (m Model) runValidation() (tea.Model, tea.Cmd) {
	m.validation = m.proj.Validate()
	m.statusMsg = formatValidationStatus(m.validation)
	m.view = ViewDashboard
	return m, nil
}

func formatValidationStatus(v project.ValidationResult) string {
	if len(v.Missing) == 0 && len(v.Invalid) == 0 {
		return "✓ validation passed"
	}
	parts := make([]string, 0, 4)
	if len(v.Missing) > 0 {
		keys := make([]string, 0, len(v.Missing))
		for _, f := range v.Missing {
			keys = append(keys, f.Key)
		}
		parts = append(parts, fmt.Sprintf("missing: %s", strings.Join(keys, ", ")))
	}
	if len(v.Invalid) > 0 {
		keys := make([]string, 0, len(v.Invalid))
		for _, f := range v.Invalid {
			keys = append(keys, f.Key)
		}
		parts = append(parts, fmt.Sprintf("invalid: %s", strings.Join(keys, ", ")))
	}
	return strings.Join(parts, " · ")
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
	case "esc", "q":
		m.view = ViewDashboard
	case "r":
		envs := m.compare.Envs
		if len(envs) == 0 {
			envs = m.proj.EnvironmentNames()
		}
		res, err := m.proj.CompareAll(envs...)
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
		m.statusMsg = "edit cancelled"
		return m, nil
	case "ctrl+s":
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
		if strings.TrimSpace(val) == "" {
			m.statusMsg = "value is required"
			return m, nil
		}
		if err := m.proj.EditVariable(m.editKey, val); err != nil {
			if isProtectedErr(err) {
				return m.showBlocked(err.Error())
			}
			m.statusMsg = err.Error()
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("updated %s", m.editKey)
	}
	m.view = ViewDashboard
	return m, nil
}

func isProtectedErr(err error) bool {
	return errors.Is(err, project.ErrProtected)
}

// Run starts the Bubble Tea program.
func Run(p *project.Project) error {
	prog := tea.NewProgram(New(p), tea.WithAltScreen())
	_, err := prog.Run()
	return err
}
