package project

import (
	"fmt"
	"sort"

	"github.com/fxckcode/envy/internal/redact"
)

// Compare builds a presence/diff matrix between two environments.
func (p *Project) Compare(left, right string) (CompareResult, error) {
	return p.CompareAll(left, right)
}

// CompareAll builds a presence/diff matrix across one or more environments.
// Secrets never appear as cleartext. Non-secret divergent values are shown.
func (p *Project) CompareAll(envs ...string) (CompareResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(envs) == 0 {
		envs = append([]string{}, p.cfg.EnvironmentNames()...)
	}
	if len(envs) < 2 {
		return CompareResult{}, fmt.Errorf("need at least two environments to compare")
	}
	for _, e := range envs {
		if _, ok := p.envs[e]; !ok {
			return CompareResult{}, fmt.Errorf("unknown environment %q", e)
		}
	}

	keySet := map[string]struct{}{}
	for _, e := range envs {
		for _, k := range p.envs[e].Keys() {
			keySet[k] = struct{}{}
		}
	}
	for k := range p.cfg.Schema {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cells := map[string]map[string]CompareCell{}
	var warnings []string
	for _, k := range keys {
		secret := p.isSecretLocked(k)
		row := map[string]CompareCell{}
		present := make([]string, 0, len(envs))
		absent := make([]string, 0, len(envs))
		values := map[string]string{}
		for _, e := range envs {
			v, ok := p.envs[e].Get(k)
			if ok {
				present = append(present, e)
				values[e] = v
			} else {
				absent = append(absent, e)
			}
		}

		divergent := valuesDiverge(present, values)
		for _, e := range envs {
			row[e] = compareCellFor(e, values, secret, divergent, len(absent) > 0)
		}
		cells[k] = row
		warnings = append(warnings, compareWarnings(k, present, absent, divergent)...)
	}

	left, right := envs[0], envs[1]
	return CompareResult{
		Envs:     append([]string{}, envs...),
		LeftEnv:  left,
		RightEnv: right,
		Keys:     keys,
		Cells:    cells,
		Warnings: warnings,
	}, nil
}

func valuesDiverge(present []string, values map[string]string) bool {
	var first string
	var firstSet bool
	for _, e := range present {
		if !firstSet {
			first = values[e]
			firstSet = true
			continue
		}
		if values[e] != first {
			return true
		}
	}
	return false
}

func safeCellValue(raw string, secret bool) string {
	if secret {
		return redact.MCPPlaceholder
	}
	return raw
}

func compareCellFor(env string, values map[string]string, secret, divergent, partial bool) CompareCell {
	v, ok := values[env]
	switch {
	case !ok:
		return CompareCell{Kind: CellAbsent, Display: "✗"}
	case divergent:
		if secret {
			return CompareCell{Kind: CellDiff, Display: "≠", Value: safeCellValue(v, true)}
		}
		return CompareCell{Kind: CellDiff, Display: v, Value: v}
	case partial:
		return CompareCell{Kind: CellOnly, Display: "◇", Value: safeCellValue(v, secret)}
	default:
		return CompareCell{Kind: CellPresent, Display: "✓", Value: safeCellValue(v, secret)}
	}
}

func compareWarnings(key string, present, absent []string, divergent bool) []string {
	var warnings []string
	for _, e := range absent {
		if len(present) > 0 {
			warnings = append(warnings, fmt.Sprintf("⚠ %s is missing %s", e, key))
		}
	}
	if len(present) == 1 && len(absent) > 0 {
		warnings = append(warnings, fmt.Sprintf("⚠ %s exists only in %s", key, present[0]))
	}
	if divergent {
		warnings = append(warnings, fmt.Sprintf("⚠ %s differs across environments", key))
	}
	return warnings
}
