package packaging

import (
	"os"
	"path/filepath"
	"strings"
)

// sourceName maps framework files when recovery reads a validated source checkout
// from before the Werewolf/ move. Current checkouts always use the new layout;
// generated work and the public CLI have only the new paths.
func sourceName(root, name string) string {
	if !strings.HasPrefix(name, "Werewolf/") {
		return name
	}
	if _, err := os.Stat(filepath.Join(root, "Werewolf/go.mod")); err == nil {
		return name
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return name
	}
	relative := strings.TrimPrefix(name, "Werewolf/")
	switch {
	case strings.HasPrefix(relative, "runner/guest"):
		return strings.Replace(relative, "runner/guest", "tools/guest", 1)
	case relative == "runner/ci-root":
		return "scripts/ci-root"
	case relative == "keys/packages.pub":
		return "release/packages.pub"
	default:
		return relative
	}
}

func sourcePath(root, name string) string { return filepath.Join(root, sourceName(root, name)) }

func moduleDirectory(root string) string {
	if sourceName(root, "Werewolf/go.mod") == "go.mod" {
		return root
	}
	return filepath.Join(root, "Werewolf")
}
