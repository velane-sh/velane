package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRecipe(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipe.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadStrictYAMLNormalizesDocument(t *testing.T) {
	document, err := readStrictYAML(writeRecipe(t, "schema_version: \"1\"\nprofiles:\n  - profile-1\n"))
	if err != nil {
		t.Fatalf("readStrictYAML() error = %v", err)
	}
	value := document.(map[string]any)
	if value["schema_version"] != "1" {
		t.Fatalf("schema_version = %#v", value["schema_version"])
	}
}

func TestReadStrictYAMLRejectsDuplicateKeysAndAliases(t *testing.T) {
	for _, contents := range []string{
		"schema_version: \"1\"\nschema_version: \"2\"\n",
		"defaults: &defaults\n  schema_version: \"1\"\nrecipe: *defaults\n",
	} {
		if _, err := readStrictYAML(writeRecipe(t, contents)); err == nil {
			t.Errorf("readStrictYAML(%q) succeeded", contents)
		}
	}
}

func TestReadStrictYAMLRejectsMultipleDocuments(t *testing.T) {
	if _, err := readStrictYAML(writeRecipe(t, "schema_version: \"1\"\n---\nschema_version: \"1\"\n")); err == nil {
		t.Fatal("readStrictYAML() accepted multiple documents")
	}
}
