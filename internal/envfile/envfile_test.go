package envfile

import "testing"

func TestFormatValueEscapesDotenvExampleValues(t *testing.T) {
	for _, value := range []string{"hello world", `say "hi"`, "line1\nline2", `C:\\tmp`} {
		formatted := FormatValue(value)
		parsed, err := ParseString("VALUE=" + formatted + "\n")
		if err != nil {
			t.Fatalf("parse %q: %v", formatted, err)
		}
		got, ok := parsed.Get("VALUE")
		if !ok || got != value {
			t.Fatalf("round trip = %q, %v; want %q", got, ok, value)
		}
	}
}
