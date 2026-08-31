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
		printUsage(stderr)
		return 2
	}

	cmd := filtered[0]
	rest := filtered[1:]

	switch cmd {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	}

	if !KnownCommand(cmd) {
		fmt.Fprintf(stderr, "envy: unknown command %q\n", cmd)
		printUsage(stderr)
		return 2
	}

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
	case "schema":
		return cmdSchema(p, rest, stdout, stderr)
	case "example":
		return cmdExample(p, rest, stdout, stderr)
	case "hooks":
		return cmdHooks(p, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "envy: unknown command %q\n", cmd)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: envy <command> [args]")
	fmt.Fprintln(w, "commands: list, check, doctor, diff, run, get, set, delete, import, export,")
	fmt.Fprintln(w, "          agent, schema, example, hooks, help")
}

func ensureEnv(p *project.Project, env string) error {
	if env == "" {
		return nil
	}
	return p.SelectEnvironment(env)
}

func cmdList(p *project.Project, args []string, stdout, stderr io.Writer) int {
	_, vals, positionals, err := parseFlags(args, nil, []string{"env"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	env := vals["env"]
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
	bools, vals, positionals, err := parseFlags(args, []string{"ci"}, []string{"env"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	env := vals["env"]
	ci := bools["ci"]
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
	_, _, positionals, err := parseFlags(args, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
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
	_, _, positionals, err := parseFlags(args, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 2 {
		fmt.Fprintln(stderr, "usage: envy diff <left> <right>")
		return 2
	}
	if len(positionals) > 2 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[2])
		return 2
	}
	left, right := positionals[0], positionals[1]
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
	bools, vals, positionals, err := parseFlags(args, []string{"reveal"}, []string{"env"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "usage: envy get [--env name] [--reveal] <KEY>")
		return 2
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[1])
		return 2
	}
	if err := ensureEnv(p, vals["env"]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	val, _, err := p.GetVariable(positionals[0], bools["reveal"])
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, val)
	return 0
}

func cmdSet(p *project.Project, args []string, stdout, stderr io.Writer) int {
	_, vals, positionals, err := parseFlags(args, nil, []string{"env", "as-agent"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 2 {
		fmt.Fprintln(stderr, "usage: envy set [--env name] [--as-agent id] <KEY> <value>")
		return 2
	}
	if err := ensureEnv(p, vals["env"]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	key, value := positionals[0], strings.Join(positionals[1:], " ")
	agent := vals["as-agent"]
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
	_, vals, positionals, err := parseFlags(args, nil, []string{"env", "as-agent"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "usage: envy delete [--env name] [--as-agent id] <KEY>")
		return 2
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[1])
		return 2
	}
	if err := ensureEnv(p, vals["env"]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	agent := vals["as-agent"]
	if agent != "" {
		err = p.DeleteVariableAsAgent(agent, positionals[0])
	} else {
		err = p.DeleteVariable(positionals[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted %s\n", positionals[0])
	return 0
}

func cmdImport(p *project.Project, args []string, stdout, stderr io.Writer) int {
	_, vals, positionals, err := parseFlags(args, nil, []string{"env"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "usage: envy import [--env name] <file>")
		return 2
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[1])
		return 2
	}
	if err := ensureEnv(p, vals["env"]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	path := positionals[0]
	if !filepath.IsAbs(path) {
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
	bools, _, positionals, err := parseFlags(args, []string{"reveal"}, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "usage: envy export [--reveal] <env>")
		return 2
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[1])
		return 2
	}
	text, err := p.ExportEnv(positionals[0], bools["reveal"])
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, text)
	return 0
}

func cmdAgent(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent <grant|revoke|session> ...")
		return 2
	}
	switch args[0] {
	case "grant":
		return cmdAgentGrant(p, args[1:], stdout, stderr)
	case "revoke":
		return cmdAgentRevoke(p, args[1:], stdout, stderr)
	case "session":
		return cmdAgentSession(p, args[1:], stdout, stderr)
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
	bools, vals, positionals, err := parseFlags(args[1:], []string{"write", "delete", "read-secrets"}, []string{"env", "ttl"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	env := vals["env"]
	if env == "" {
		fmt.Fprintln(stderr, "envy: --env is required")
		return 2
	}
	ttl := 30 * time.Minute
	if raw, ok := vals["ttl"]; ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			fmt.Fprintf(stderr, "envy: invalid ttl: %v\n", err)
			return 2
		}
		ttl = d
	}
	g, err := p.GrantAgent(agent, env, bools["write"], bools["delete"], bools["read-secrets"], ttl)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	return printGrant(stdout, agent, g)
}

func cmdAgentRevoke(p *project.Project, args []string, stdout, stderr io.Writer) int {
	_, _, positionals, err := parseFlags(args, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent revoke <identity>")
		return 2
	}
	if len(positionals) > 1 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[1])
		return 2
	}
	if err := p.RevokeAgent(positionals[0]); err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "revoked %s\n", positionals[0])
	return 0
}

func cmdAgentSession(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent session <start|stop> ...")
		return 2
	}
	switch args[0] {
	case "start":
		return cmdAgentSessionStart(p, args[1:], stdout, stderr)
	case "stop":
		return cmdAgentRevoke(p, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "envy: unknown agent session subcommand %q\n", args[0])
		return 2
	}
}

func cmdAgentSessionStart(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: envy agent session start <identity> [--env name] [--ttl 30m]")
		return 2
	}
	agent := args[0]
	_, vals, positionals, err := parseFlags(args[1:], nil, []string{"env", "ttl"})
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	env := vals["env"]
	if env == "" {
		env = p.ActiveEnvironment()
	}
	ttl := 30 * time.Minute
	if raw, ok := vals["ttl"]; ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			fmt.Fprintf(stderr, "envy: invalid ttl: %v\n", err)
			return 2
		}
		ttl = d
	}
	// Session defaults: metadata + write; secrets and delete remain denied.
	g, err := p.GrantAgent(agent, env, true, false, false, ttl)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Agent session started\n")
	return printGrant(stdout, agent, g)
}

func printGrant(stdout io.Writer, agent string, g project.GrantDisplay) int {
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

func cmdSchema(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "generate" {
		fmt.Fprintln(stderr, "usage: envy schema generate")
		return 2
	}
	_, _, positionals, err := parseFlags(args[1:], nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	n, err := p.GenerateSchema()
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "generated schema for %d keys (no secret values written)\n", n)
	return 0
}

func cmdExample(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "generate" {
		fmt.Fprintln(stderr, "usage: envy example generate")
		return 2
	}
	_, _, positionals, err := parseFlags(args[1:], nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	path, err := p.WriteExampleFile()
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return 0
}

func cmdHooks(p *project.Project, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "install" {
		fmt.Fprintln(stderr, "usage: envy hooks install")
		return 2
	}
	_, _, positionals, err := parseFlags(args[1:], nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 2
	}
	if len(positionals) > 0 {
		fmt.Fprintf(stderr, "envy: unexpected argument %q\n", positionals[0])
		return 2
	}
	path, err := p.InstallHooks()
	if err != nil {
		fmt.Fprintf(stderr, "envy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "installed %s\n", path)
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
