package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KnownCommand reports whether s is a first-class envy CLI subcommand.
func KnownCommand(s string) bool {
	switch s {
	case "list", "check", "doctor", "diff", "run", "get", "set", "delete",
		"import", "export", "agent", "schema", "example", "hooks",
		"help", "-h", "--help":
		return true
	default:
		return false
	}
}

// LooksLikeProjectPath reports whether s should be treated as a TUI project root
// rather than an unknown command token.
func LooksLikeProjectPath(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	if strings.Contains(s, string(filepath.Separator)) || strings.HasPrefix(s, ".") {
		return true
	}
	info, err := os.Stat(s)
	return err == nil && info.IsDir()
}

// parseFlags extracts known boolean and value flags from args.
// Unknown flags or dangling value flags return an error.
func parseFlags(args []string, boolFlags, valueFlags []string) (bools map[string]bool, vals map[string]string, positionals []string, err error) {
	boolSet := map[string]struct{}{}
	for _, f := range boolFlags {
		boolSet[f] = struct{}{}
	}
	valueSet := map[string]struct{}{}
	for _, f := range valueFlags {
		valueSet[f] = struct{}{}
	}
	bools = map[string]bool{}
	vals = map[string]string{}
	positionals = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
			continue
		}
		name, inline, hasInline := splitFlag(a)
		if name == "" {
			return nil, nil, nil, fmt.Errorf("unknown flag %q", a)
		}
		if _, ok := boolSet[name]; ok {
			if hasInline {
				return nil, nil, nil, fmt.Errorf("flag %s does not take a value", name)
			}
			bools[name] = true
			continue
		}
		if _, ok := valueSet[name]; ok {
			if hasInline {
				vals[name] = inline
				continue
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, nil, nil, fmt.Errorf("flag %s requires a value", name)
			}
			vals[name] = args[i+1]
			i++
			continue
		}
		return nil, nil, nil, fmt.Errorf("unknown flag %q", a)
	}
	return bools, vals, positionals, nil
}

func splitFlag(a string) (name, value string, hasValue bool) {
	if strings.HasPrefix(a, "--") {
		body := strings.TrimPrefix(a, "--")
		if body == "" {
			return "", "", false
		}
		if i := strings.IndexByte(body, '='); i >= 0 {
			return body[:i], body[i+1:], true
		}
		return body, "", false
	}
	if strings.HasPrefix(a, "-") && len(a) > 1 && !strings.HasPrefix(a, "--") {
		// Short flags are not used; treat as unknown unless exact known forms.
		return "", "", false
	}
	return "", "", false
}
