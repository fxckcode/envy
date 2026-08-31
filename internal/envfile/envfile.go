// Package envfile reads and writes dotenv-style environment files.
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Vars is an ordered map of environment variable bindings.
type Vars struct {
	order  []string
	values map[string]string
}

// New returns an empty Vars.
func New() *Vars {
	return &Vars{values: map[string]string{}}
}

// ParseFile loads a .env file. Missing files yield an empty Vars.
func ParseFile(path string) (*Vars, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, err
	}
	defer f.Close()

	v := New()
	sc := bufio.NewScanner(f)
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

func splitKV(line string) (string, string, bool) {
	if i := strings.IndexByte(line, '='); i > 0 {
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		val = unquote(val)
		if key == "" {
			return "", "", false
		}
		return key, val, true
	}
	return "", "", false
}

func unquote(s string) string {
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			if unquoted, err := strconv.Unquote(s); err == nil {
				return unquoted
			}
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// Get returns a value and whether it exists.
func (v *Vars) Get(key string) (string, bool) {
	val, ok := v.values[key]
	return val, ok
}

// Set adds or updates a key, preserving order for new keys.
func (v *Vars) Set(key, value string) {
	if _, exists := v.values[key]; !exists {
		v.order = append(v.order, key)
	}
	v.values[key] = value
}

// Delete removes a key.
func (v *Vars) Delete(key string) {
	if _, ok := v.values[key]; !ok {
		return
	}
	delete(v.values, key)
	out := v.order[:0]
	for _, k := range v.order {
		if k != key {
			out = append(out, k)
		}
	}
	v.order = out
}

// Keys returns keys in file order.
func (v *Vars) Keys() []string {
	out := make([]string, len(v.order))
	copy(out, v.order)
	return out
}

// Len returns the number of bindings.
func (v *Vars) Len() int {
	return len(v.values)
}

// Clone returns a deep copy.
func (v *Vars) Clone() *Vars {
	c := New()
	for _, k := range v.order {
		c.Set(k, v.values[k])
	}
	return c
}

// WriteFile persists vars to path.
func (v *Vars) WriteFile(path string) error {
	var b strings.Builder
	for _, k := range v.order {
		val := v.values[k]
		b.WriteString(fmt.Sprintf("%s=%s\n", k, FormatValue(val)))
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func needsQuote(s string) bool {
	return strings.ContainsAny(s, " 	#\"'\\\r\n")
}

// FormatValue returns a dotenv-safe value, escaping quoted content as needed.
func FormatValue(s string) string {
	if needsQuote(s) {
		return strconv.Quote(s)
	}
	return s
}
