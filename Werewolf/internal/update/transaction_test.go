package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateInstallsRecipeAndLockTogetherAndRefusesStaleInputs(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "package")
	prepared := t.TempDir()
	for _, dir := range []string{original, prepared} {
		mustWrite(t, filepath.Join(dir, "package.yaml"), "old recipe")
		mustWrite(t, filepath.Join(dir, "package-lock.json"), "old lock")
	}
	expected, err := contextDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(prepared, "package.yaml"), "new recipe")
	mustWrite(t, filepath.Join(prepared, "package-lock.json"), "new lock")
	mustWrite(t, filepath.Join(original, "package.yaml"), "maintainer edit")
	if err := installContext(prepared, original, expected); err == nil || !strings.Contains(err.Error(), "changed during update") {
		t.Fatalf("stale update: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(original, "package.yaml"))
	if string(data) != "maintainer edit" {
		t.Fatal("lost a maintainer edit")
	}
	expected, err = contextDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := installContext(prepared, original, expected); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"package.yaml": "new recipe", "package-lock.json": "new lock"} {
		data, err := os.ReadFile(filepath.Join(original, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %s %v", name, data, err)
		}
	}
}
