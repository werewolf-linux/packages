package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandsAcceptTheRepositoryRootAndMaintenanceModule(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "Werewolf")
	if err := os.Mkdir(module, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/Werewolf\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, module} {
		got, err := RepositoryRoot(path)
		if err != nil || got != root {
			t.Fatalf("%s: %s %v", path, got, err)
		}
	}
	scratch := t.TempDir()
	got, err := RepositoryRoot(scratch)
	if err != nil || got != scratch {
		t.Fatalf("explicit preparation root changed: %s %v", got, err)
	}
}
