// Package redact masks secret values for safe display across TUI, CLI, and audit.
package redact

const (
	// MaskGlyph is the character used for secret list display.
	MaskGlyph = "●"
	// MaskLength is the number of glyphs shown for a masked secret.
	MaskLength = 8
	// Placeholder is the textual redaction used in modals and audit.
	Placeholder = "********"
)

// Mask returns the visual mask for secret list cells.
func Mask() string {
	out := make([]rune, MaskLength)
	g := []rune(MaskGlyph)[0]
	for i := range out {
		out[i] = g
	}
	return string(out)
}

// Display returns a safe display string for a value.
// Secrets always mask; non-secrets return the raw value (may be truncated by callers).
func Display(value string, secret bool) string {
	if secret {
		return Mask()
	}
	return value
}

// ModalValue returns the policy-aware display for approval modals.
// Old secret values are never cleartext. New values show cleartext only when not secret.
func ModalValue(value string, secret bool) string {
	if secret {
		return Placeholder
	}
	if value == "" {
		return "—"
	}
	return value
}
