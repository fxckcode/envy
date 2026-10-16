package project_test

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxckcode/envy/internal/project"
)

func TestAuditPersistenceFailureIsLoggedAndNonBlocking(t *testing.T) {
	root := sampleRoot(t)
	p, err := project.Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".envy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".envy", "audit.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	if err := p.SetInEnv("development", "PORT", "8787", "agent"); err != nil {
		t.Fatalf("mutation failed after audit persistence error: %v", err)
	}
	if value, ok := p.RawValue("development", "PORT"); !ok || value != "8787" {
		t.Fatalf("mutation was not applied despite audit persistence error: value=%q present=%v", value, ok)
	}

	if !strings.Contains(output.String(), "audit persistence failed") {
		t.Fatalf("audit persistence failure was not logged: %q", output.String())
	}
	entries := p.AuditLog()
	if len(entries) == 0 || entries[len(entries)-1].Action != "env_set" {
		t.Fatalf("in-memory audit entry missing after persistence failure: %+v", entries)
	}
}
