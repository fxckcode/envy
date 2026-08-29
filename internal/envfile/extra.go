package envfile

import (
	"bufio"
	"os"
	"strings"
)

// DuplicateKey is a key that appeared more than once in a file.
type DuplicateKey struct {
	Key   string
	Count int
}

// ScanDuplicates reports keys that appear more than once in a dotenv file.
// Missing files yield an empty result.
func ScanDuplicates(path string) ([]DuplicateKey, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	counts := map[string]int{}
	order := []string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := splitKV(line)
		if !ok {
			continue
		}
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var dups []DuplicateKey
	for _, k := range order {
		if counts[k] > 1 {
			dups = append(dups, DuplicateKey{Key: k, Count: counts[k]})
		}
	}
	return dups, nil
}

// Merge overlays keys from other into v (other wins on conflict).
func (v *Vars) Merge(other *Vars) {
	for _, k := range other.Keys() {
		val, _ := other.Get(k)
		v.Set(k, val)
	}
}

// ExportLines returns KEY=value lines in file order.
func (v *Vars) ExportLines(format func(key, value string) string) []string {
	out := make([]string, 0, len(v.order))
	for _, k := range v.order {
		out = append(out, format(k, v.values[k]))
	}
	return out
}

// ParseString parses dotenv content from a string.
func ParseString(content string) (*Vars, error) {
	v := New()
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		v.Set(key, val)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return v, nil
}
