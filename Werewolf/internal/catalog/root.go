package catalog

import (
	"os"
	"path/filepath"
)

// RepositoryRoot accepts the repository root or its maintenance module. Explicit
// package roots used by source preparation are otherwise kept as supplied.
func RepositoryRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Base(absolute) == "Werewolf" {
		if info, err := os.Stat(filepath.Join(absolute, "go.mod")); err == nil && info.Mode().IsRegular() {
			return filepath.Dir(absolute), nil
		}
	}
	return absolute, nil
}
