package main

import (
	"fmt"
	"os"

	"github.com/fxckcode/envy/internal/cli"
	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/tui"
)

func main() {
	args := os.Args[1:]

	// Global --dir before subcommand detection.
	dir := "."
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--dir" && i+1 < len(args) {
			dir = args[i+1]
			i++
			continue
		}
		if len(a) > 6 && a[:6] == "--dir=" {
			dir = a[6:]
			continue
		}
		rest = append(rest, a)
	}

	// No subcommand → launch TUI (legacy path also accepts bare project path).
	if len(rest) == 0 {
		runTUI(dir)
		return
	}
	if len(rest) == 1 && !isCommand(rest[0]) {
		runTUI(rest[0])
		return
	}

	code := cli.Execute(append([]string{"--dir", dir}, rest...), os.Stdout, os.Stderr)
	os.Exit(code)
}

func isCommand(s string) bool {
	switch s {
	case "list", "check", "doctor", "diff", "run", "get", "set", "delete", "import", "export", "agent", "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func runTUI(root string) {
	p, err := project.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envy: %v\n", err)
		os.Exit(1)
	}
	if err := tui.Run(p); err != nil {
		fmt.Fprintf(os.Stderr, "envy: %v\n", err)
		os.Exit(1)
	}
}
