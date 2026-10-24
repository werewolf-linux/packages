package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// ContextPath resolves the stable context identity without exposing categories to callers.
func ContextPath(root, name string) (string, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`).MatchString(name) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid context: %s", name)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", name, "metadata.json"))
	if err != nil {
		return "", err
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0]), nil
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("duplicate context: %s", name)
	}
	if _, err := os.Stat(filepath.Join(root, "wolfi")); err == nil {
		return "", fmt.Errorf("missing metadata: %s", name)
	}
	return filepath.Join(root, name), nil
}

func catalogPolicies(root string) (map[string]Policy, []string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "metadata.json"))
	if err != nil {
		return nil, nil, err
	}
	policies := map[string]Policy{}
	recipes := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		if filepath.Base(filepath.Dir(filepath.Dir(path))) == "node_modules" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var meta struct {
			Update json.RawMessage `json:"update"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, nil, err
		}
		name := filepath.Base(filepath.Dir(path))
		if seen[name] {
			return nil, nil, fmt.Errorf("invalid or duplicate context: %s", path)
		}
		seen[name] = true
		var p Policy
		var source struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(meta.Update, &source); err != nil {
			return nil, nil, err
		}
		if source.Source == "wolfi" {
			continue
		}

		if err := json.Unmarshal(meta.Update, &p); err != nil {
			return nil, nil, err
		}
		found, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.yaml"))
		if err != nil || len(found) != 1 {
			return nil, nil, fmt.Errorf("%s needs exactly one recipe", name)
		}
		policies[name] = p
		recipes = append(recipes, found[0])
	}
	// A recipe or hook without metadata must never disappear from automation.
	for _, pattern := range []string{"*.yaml", "package.py"} {
		found, _ := filepath.Glob(filepath.Join(root, "*", "*", pattern))
		for _, path := range found {
			category := filepath.Base(filepath.Dir(filepath.Dir(path)))
			switch category {
			case "scripts", "tests", "wolfi", "build", ".github":
				continue
			}
			if !seen[filepath.Base(filepath.Dir(path))] {
				return nil, nil, fmt.Errorf("missing package metadata: %s", path)
			}
		}
	}
	if len(seen) == 0 {
		return nil, nil, fmt.Errorf("empty package catalog")
	}
	return policies, recipes, nil
}
