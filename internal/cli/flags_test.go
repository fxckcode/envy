package cli

import "testing"

func TestParseFlagsRejectsUnknownAndDangling(t *testing.T) {
	_, _, _, err := parseFlags([]string{"--bogus"}, nil, []string{"env"})
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	_, _, _, err = parseFlags([]string{"--env"}, nil, []string{"env"})
	if err == nil {
		t.Fatal("expected dangling value flag error")
	}
	bools, vals, pos, err := parseFlags([]string{"--ci", "--env", "production", "extra"}, []string{"ci"}, []string{"env"})
	if err != nil {
		t.Fatal(err)
	}
	if !bools["ci"] || vals["env"] != "production" || len(pos) != 1 || pos[0] != "extra" {
		t.Fatalf("unexpected parse: bools=%v vals=%v pos=%v", bools, vals, pos)
	}
}
