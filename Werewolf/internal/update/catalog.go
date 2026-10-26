package update

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
)

// ContextPath resolves a stable context identity independently of its category.
func ContextPath(root, name string) (string, error) {
	if !catalog.Name.MatchString(name) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid context: %s", name)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", name, "package.yaml"))
	if err != nil {
		return "", err
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0]), nil
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("duplicate context: %s", name)
	}
	if _, err := os.Stat(filepath.Join(root, "Werewolf")); err == nil {
		return "", fmt.Errorf("missing package.yaml: %s", name)
	}
	return filepath.Join(root, name), nil
}

func catalogPolicies(root string) (map[string]Policy, []string, error) {
	packages, err := catalog.Load(root)
	if err != nil {
		return nil, nil, err
	}
	policies := map[string]Policy{}
	var recipes []string
	for _, pkg := range packages {
		if pkg.Engine == "wolfi" {
			continue
		}
		found, err := filepath.Glob(filepath.Join(root, pkg.Path, "*.yaml"))
		if err != nil || len(found) != 1 {
			return nil, nil, fmt.Errorf("%s needs exactly one recipe", pkg.Name)
		}
		policies[pkg.Name] = pkg.Update
		recipes = append(recipes, found[0])
	}
	return policies, recipes, nil
}
