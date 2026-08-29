// Package cli implements the scriptable Envy command surface.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
)

// Execute runs the envy CLI with the given args (no program name).
// Returns a process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	dir := "."
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--dir" && i+1 < len(args) {
			dir = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--dir=") {
			dir = strings.TrimPrefix(a, "--dir=")
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 {
		fmt.Fprintln(stderr, "usage: envy <command> [args]")
		fmt.Fprintln(stderr, "commands: list, check, doctor, diff, run, get, set, delete, import, export, agent")
		return 2
	}

	cmd := filtered[0]
	rest := filtered[1:]

	// Commands that need a project.
	p, err := project.Open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}

	switch cmd {
	case "list":
		return cmdList(p, rest, stdout, stderr)
	case "check":
		return cmdCheck(p, rest, stdout, stderr)
	case "doctor":
		return cmdDoctor(p, rest, stdout, stderr)
	case "diff":
		return cmdDiff(p, rest, stdout, stderr)
	case "run":
		return cmdRun(p, rest, stdout, stderr)
	case "get":
		return cmdGet(p, rest, stdout, stderr)
	case "set":
		return cmdSet(p, rest, stdout, stderr)
	case "delete":
		return cmdDelete(p, rest, stdout, stderr)
	case "import":
		return cmdImport(p, rest, stdout, stderr)
	case "export":
		return cmdExport(p, rest, stdout, stderr)
	case "agent":
		return cmdAgent(p, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "envy: unknown command %q\n", cmd)
		return 2
	}
}

func parseEnvFlag(args []string) (env string, rest []string) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--env" && i+1 < len(args) {
			env = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--env=") {
			env = strings.TrimPrefix(a, "--env=")
			continue
		}
		rest = append(rest, a)
	}
	return env, rest
}

func ensureEnv(p *project.Project, env string) error {
	if env == "" {
		return nil
	}
	return p.SelectEnvironment(env)
}

func cmdList(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	_ = rest
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	target := p.ActiveEnvironment()
	vars, err := p.VariablesFor(target)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "ENV: %s\n\n", target)
	maxKey := 0
	for _, v := range vars {
		if len(v.Key) > maxKey {
			maxKey = len(v.Key)
		}
	}
	for _, v := range vars {
		marker := "  "
		if v.Missing {
			marker = "⚠ "
		}
		fmt.Fprintf(stdout, "%s%-*s  %s\n", marker, maxKey, v.Key, v.Display)
	}
	st := p.Status()
	fmt.Fprintf(stdout, "\n%d variables", st.VariableCount)
	if st.MissingCount > 0 {
		fmt.Fprintf(stdout, "   ⚠ %d missing", st.MissingCount)
	}
	fmt.Fprintf(stdout, "   ✓ secrets hidden\n")
	return 0
}

func cmdCheck(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	ci := false
	filtered := make([]string, 0, len(rest))
	for _, a := range rest {
		if a == "--ci" {
			ci = true
			continue
		}
		filtered = append(filtered, a)
	}
	_ = filtered
	if env == "" {
		env = p.ActiveEnvironment()
	}
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}

	if ci {
		ok, report := p.CICheck(env)
		if !ok {
			fmt.Fprintln(stdout, report)
			return 1
		}
		fmt.Fprintln(stdout, "ok")
		return 0
	}

	res, err := p.ValidateEnv(env)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	failed := false
	if len(res.Missing) > 0 {
		failed = true
		fmt.Fprintln(stdout, "Missing required keys:")
		for _, f := range res.Missing {
			fmt.Fprintf(stdout, "  ✗ %s — %s\n", f.Key, f.Message)
		}
	}
	if len(res.Invalid) > 0 {
		failed = true
		fmt.Fprintln(stdout, "Invalid keys:")
		for _, f := range res.Invalid {
			fmt.Fprintf(stdout, "  ✗ %s — %s\n", f.Key, f.Message)
		}
	}
	if !failed {
		fmt.Fprintf(stdout, "✓ %s is valid\n", env)
		return 0
	}
	return 1
}

func cmdDoctor(p *project.Project, args []string, stdout, stderr io.Writer) int {
	_ = args
	_ = stderr
	res := p.Doctor()
	fmt.Fprintln(stdout, "ENVIRONMENT HEALTH")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Score: %d/100\n", res.Score)
	fmt.Fprintln(stdout)
	exitFail := false
	for _, c := range res.Checks {
		glyph := "✓"
		switch c.Status {
		case project.DoctorFail:
			glyph = "✗"
			exitFail = true
		case project.DoctorWarn:
			glyph = "⚠"
		}
		line := fmt.Sprintf("%s %s", glyph, c.Label)
		if c.Detail != "" && c.Status != project.DoctorPass {
			// For leak checks, label already names the key; still avoid values.
			if c.Status == project.DoctorFail && strings.Contains(c.Label, "found in") {
				fmt.Fprintln(stdout, line)
				continue
			}
			line = fmt.Sprintf("%s %s (%s)", glyph, c.Label, c.Detail)
		}
		fmt.Fprintln(stdout, line)
	}
	if exitFail {
		return 1
	}
	return 0
}

func cmdDiff(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: envy diff <left> <right>")
		return 2
	}
	left, right := args[0], args[1]
	res, err := p.Diff(left, right)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	leftH := strings.ToUpper(left)
	rightH := strings.ToUpper(right)
	maxKey := len("KEY")
	maxL, maxR := len(leftH), len(rightH)
	for _, row := range res.Rows {
		if len(row.Key) > maxKey {
			maxKey = len(row.Key)
		}
		if len(row.LeftDisplay) > maxL {
			maxL = len(row.LeftDisplay)
		}
		if len(row.RightDisplay) > maxR {
			maxR = len(row.RightDisplay)
		}
	}
	fmt.Fprintf(stdout, "%-*s  %-*s  %-*s\n", maxKey, "KEY", maxL, leftH, maxR, rightH)
	for _, row := range res.Rows {
		fmt.Fprintf(stdout, "%-*s  %-*s  %-*s\n", maxKey, row.Key, maxL, row.LeftDisplay, maxR, row.RightDisplay)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Warnings")
		for _, w := range res.Warnings {
			fmt.Fprintln(stdout, w)
		}
	} else {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "No differences")
	}
	return 0
}

func cmdRun(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy run <env> -- <command>...")
		return 2
	}
	env := args[0]
	rest := args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "usage: envy run <env> -- <command>...")
		return 2
	}
	code, err := p.Run(env, rest, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	return code
}

func cmdGet(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	reveal := false
	filtered := make([]string, 0, len(rest))
	for _, a := range rest {
		if a == "--reveal" {
			reveal = true
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) < 1 {
		fmt.Fprintln(stderr, "usage: envy get [--env name] [--reveal] <KEY>")
		return 2
	}
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	val, _, err := p.GetVariable(filtered[0], reveal)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, val)
	return 0
}

func parseAsAgentFlag(args []string) (agent string, rest []string) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--as-agent" && i+1 < len(args) {
			agent = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--as-agent=") {
			agent = strings.TrimPrefix(a, "--as-agent=")
			continue
		}
		rest = append(rest, a)
	}
	return agent, rest
}

func cmdSet(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	agent, filtered := parseAsAgentFlag(rest)
	if len(filtered) < 2 {
		fmt.Fprintln(stderr, "usage: envy set [--env name] [--as-agent id] <KEY> <value>")
		return 2
	}
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	key, value := filtered[0], strings.Join(filtered[1:], " ")
	var err error
	if agent != "" {
		err = p.SetVariableAsAgent(agent, key, value)
	} else {
		err = p.SetVariable(key, value)
	}
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "set %s (%s)\n", key, redact.Placeholder)
	return 0
}

func cmdDelete(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	agent, filtered := parseAsAgentFlag(rest)
	if len(filtered) < 1 {
		fmt.Fprintln(stderr, "usage: envy delete [--env name] [--as-agent id] <KEY>")
		return 2
	}
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	var err error
	if agent != "" {
		err = p.DeleteVariableAsAgent(agent, filtered[0])
	} else {
		err = p.DeleteVariable(filtered[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted %s\n", filtered[0])
	return 0
}

func cmdImport(p *project.Project, args []string, stdout, stderr io.Writer) int {
	env, rest := parseEnvFlag(args)
	if len(rest) < 1 {
		fmt.Fprintln(stderr, "usage: envy import [--env name] <file>")
		return 2
	}
	if err := ensureEnv(p, env); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	path := rest[0]
	if !filepath.IsAbs(path) {
		// Allow relative to cwd; also try project root.
		if _, err := os.Stat(path); err != nil {
			alt := filepath.Join(p.Root(), path)
			if _, err2 := os.Stat(alt); err2 == nil {
				path = alt
			}
		}
	}
	n, err := p.ImportFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "imported %d keys into %s\n", n, p.ActiveEnvironment())
	return 0
}

func cmdExport(p *project.Project, args []string, stdout, stderr io.Writer) int {
	reveal := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--reveal" {
			reveal = true
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) < 1 {
		fmt.Fprintln(stderr, "usage: envy export [--reveal] <env>")
		return 2
	}
	text, err := p.ExportEnv(filtered[0], reveal)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, text)
	return 0
}

func cmdAgent(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent <grant|revoke> ...")
		return 2
	}
	switch args[0] {
	case "grant":
		return cmdAgentGrant(p, args[1:], stdout, stderr)
	case "revoke":
		return cmdAgentRevoke(p, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "envy: unknown agent subcommand %q\n", args[0])
		return 2
	}
}

func cmdAgentGrant(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent grant <identity> --env <env> [--write] [--delete] [--read-secrets] [--ttl 30m]")
		return 2
	}
	agent := args[0]
	env := ""
	write, del, readSecrets := false, false, false
	ttl := 30 * time.Minute
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--env" && i+1 < len(rest):
			env = rest[i+1]
			i++
		case strings.HasPrefix(a, "--env="):
			env = strings.TrimPrefix(a, "--env=")
		case a == "--write":
			write = true
		case a == "--delete":
			del = true
		case a == "--read-secrets":
			readSecrets = true
		case a == "--ttl" && i+1 < len(rest):
			d, err := time.ParseDuration(rest[i+1])
			if err != nil {
				fmt.Fprintf(stderr, "envy: invalid ttl: %v\n", err)
				return 2
			}
			ttl = d
			i++
		case strings.HasPrefix(a, "--ttl="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--ttl="))
			if err != nil {
				fmt.Fprintf(stderr, "envy: invalid ttl: %v\n", err)
				return 2
			}
			ttl = d
		}
	}
	if env == "" {
		fmt.Fprintln(stderr, "envy: --env is required")
		return 2
	}
	g, err := p.GrantAgent(agent, env, write, del, readSecrets, ttl)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, displayName(agent))
	fmt.Fprintf(stdout, "Environment: %s\n\n", g.Environment)
	fmt.Fprintf(stdout, "read metadata     %s\n", boolGlyph(true))
	fmt.Fprintf(stdout, "read_values       %s\n", boolGlyph(g.ReadSecrets))
	fmt.Fprintf(stdout, "write             %s\n", boolGlyph(g.Write))
	fmt.Fprintf(stdout, "delete            %s\n", boolGlyph(g.Delete))
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Expires in: %s\n", formatTTL(g.ExpiresIn))
	return 0
}

func cmdAgentRevoke(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent revoke <identity>")
		return 2
	}
	if err := p.RevokeAgent(args[0]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "revoked %s\n", args[0])
	return 0
}

func boolGlyph(v bool) string {
	if v {
		return "✓"
	}
	return "✗"
}

func displayName(agent string) string {
	parts := strings.Split(agent, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func formatTTL(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
