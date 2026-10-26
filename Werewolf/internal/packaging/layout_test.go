package packaging

import (
	"path/filepath"
	"testing"
)

func TestRecoveryFindsFrameworkFilesInBothSourceLayouts(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "go.mod"), []byte("module github.com/werewolf-linux/packages\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Werewolf/go.mod":                          "go.mod",
		"Werewolf/internal/packaging/build.go":     "internal/packaging/build.go",
		"Werewolf/keys/packages.pub":               "release/packages.pub",
		"Werewolf/runner/guest/melange-guest.yaml": "tools/guest/melange-guest.yaml",
		"Werewolf/runner/ci-root":                  "scripts/ci-root",
		".github/workflows/build.yml":              ".github/workflows/build.yml",
	}
	for current, previous := range files {
		if got := sourceName(root, current); got != previous {
			t.Fatalf("historical %s: %s", current, got)
		}
	}
	if err := writeFile(filepath.Join(root, "Werewolf/go.mod"), []byte("module github.com/werewolf-linux/packages/Werewolf\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for current := range files {
		if got := sourceName(root, current); got != current {
			t.Fatalf("current %s: %s", current, got)
		}
	}
}
