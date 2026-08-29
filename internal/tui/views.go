package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fxckcode/envy/internal/project"
)

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

// View renders the UI.
func (m Model) View() string {
	switch m.view {
	case ViewCompare:
		return m.viewCompare()
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
	case ViewConfirmDelete:
		return m.viewConfirmDelete()
	default:
		return m.viewDashboard()
	}
}

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

	envLines := make([]string, 0, max(len(envs), len(vars))+1)
	envLines = append(envLines, " Environments")
	for i, e := range envs {
		line := "   " + e
		if i == m.envIdx {
			line = fmt.Sprintf(" > %s", e)
			if m.focus == FocusEnvs {
				line = styleSelected.Render(line)
			} else {
				line = styleAccent.Render(line)
			}
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
		if v.Missing || v.Invalid {
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
		left, right := "", ""
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
	b.WriteString("│ [a] add  [e] edit  [x] delete  [c] compare  [v] validate  [p] providers  [g] activity  [q] quit\n")
	if m.statusMsg != "" {
		b.WriteString("│ " + styleMuted.Render(m.statusMsg) + "\n")
	}
	b.WriteString("└" + strings.Repeat("─", max(20, m.width-4)) + "┘\n")
	return b.String()
}

func (m Model) viewCompare() string {
	envs := m.compare.Envs
	if len(envs) == 0 {
		envs = []string{m.compare.LeftEnv, m.compare.RightEnv}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("┌─ Compare: %s ─\n", strings.Join(envs, " · ")))
	header := fmt.Sprintf("│ %-20s", "KEY")
	for _, e := range envs {
		header += fmt.Sprintf(" %-14s", strings.ToUpper(e))
	}
	b.WriteString(header + "\n")
	for _, k := range m.compare.Keys {
		row := m.compare.Cells[k]
		line := fmt.Sprintf("│ %-20s", k)
		for _, e := range envs {
			cell := row[e]
			line += fmt.Sprintf(" %-14s", cell.Display)
		}
		b.WriteString(line + "\n")
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
	if m.formSecret || (m.view == ViewEdit && m.formSecret) {
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
	b.WriteString(fmt.Sprintf("│ New value: %s\n", newD))
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

func (m Model) viewConfirmDelete() string {
	var b strings.Builder
	b.WriteString("┌─ Confirm delete ─\n")
	b.WriteString(fmt.Sprintf("│ Delete %s from %s?\n", styleDanger.Render(m.deleteKey), m.proj.ActiveEnvironment()))
	b.WriteString("│ [y] confirm  [n] cancel\n└─\n")
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(plain int, s string, width int) string {
	if plain <= width {
		return s + strings.Repeat(" ", width-plain)
	}
	return s
}

func plainLen(s string) int {
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
