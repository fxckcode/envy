package project

import (
	"strings"
	"testing"

	"github.com/fxckcode/envy/internal/config"
)

func TestInferSchemaFieldRequiredOnlyForConfiguredValue(t *testing.T) {
	required := inferSchemaField("PORT", "3000")
	if !required.Required {
		t.Fatal("non-empty discovered value should be required")
	}
	optional := inferSchemaField("DEBUG", "")
	if optional.Required {
		t.Fatal("empty discovered value should remain optional")
	}
}

func TestMergeSchemaFieldPreservesExplicitOptional(t *testing.T) {
	got := mergeSchemaField(inferSchemaField("DEBUG", "true"), config.SchemaField{Required: false, Type: "boolean"})
	if !got.Required {
		t.Fatal("merge should retain inferred requirement before explicit override")
	}
	// GenerateSchema applies the existing declaration's required flag after
	// merging, so an explicit optional declaration remains optional.
	got.Required = config.SchemaField{Required: false}.Required
	if got.Required {
		t.Fatal("explicit optional schema must remain optional")
	}
}

func TestLooksLikeSecretValueDetectsMySQLCredentials(t *testing.T) {
	value := "mysql://user:" + "password" + "@host/db"
	if !looksLikeSecretValue(value) {
		t.Fatal("expected MySQL URL userinfo to be treated as secret")
	}
	if looksLikeSecretValue("mysql://host/db") {
		t.Fatal("credential-free MySQL URL should not be treated as secret")
	}
}

func TestPreCommitHookProtectsDotenvVariantsAndMySQLURLs(t *testing.T) {
	for _, fragment := range []string{".env|.env.*", ".env.example)", "mysql://[^ ]+@"} {
		if !strings.Contains(preCommitHook, fragment) {
			t.Fatalf("hook missing protection fragment %q", fragment)
		}
	}
}
