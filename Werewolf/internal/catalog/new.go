package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

// New creates only the named context, refusing to overwrite an existing one.
// A Wolfi starter takes a local, reviewed recipe; source discovery is explicit.
func New(root, context, repo, baseline string) (string, error) {
	parts := strings.Split(filepath.ToSlash(context), "/")
	if len(parts) != 2 || !Category(parts[0]) || !Name.MatchString(parts[1]) || parts[0] == "." || parts[1] == "." || parts[0] == ".." || parts[1] == ".." {
		return "", fmt.Errorf("use CATEGORY/NAME")
	}
	if !repository.MatchString(repo) {
		return "", fmt.Errorf("new needs --repo OWNER/PROJECT for its release source")
	}
	path := filepath.Join(root, context)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return "", fmt.Errorf("context already exists: %s", context)
	}
	existing, err := filepath.Glob(filepath.Join(root, "*", parts[1], "package.yaml"))
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return "", fmt.Errorf("package name already exists: %s", parts[1])
	}
	d := Definition{Update: Source{GitHub: &GitHub{Identifier: repo}}, Changes: []Change{{What: "Maintain " + parts[1] + " packaging.", Why: "Describe the package's purpose and reviewed differences.", Evidence: []string{"package.yaml"}}}}
	var fixture []byte
	if baseline != "" {
		fixture, err = os.ReadFile(baseline)
		if err != nil {
			return "", err
		}
		recipe, err := overlay.Parse(fixture)
		if err != nil {
			return "", err
		}
		name := overlay.Value(overlay.Field(recipe, "package"), "name")
		if !Name.MatchString(name) {
			return "", fmt.Errorf("baseline lacks a valid package name")
		}
		checkout, err := overlay.Select(recipe, []string{"pipeline", "uses=git-checkout"})
		if err != nil {
			return "", err
		}
		with := overlay.Field(checkout, "with")
		source := overlay.Value(with, "repository")
		if source == "" {
			return "", fmt.Errorf("baseline needs an explicit source checkout")
		}
		expected := overlay.Clone(checkout)
		if !strings.HasPrefix(overlay.Value(with, "expected-commit"), "${{vars.") {
			overlay.Put(overlay.Field(expected, "with"), "expected-commit", overlay.String("${lock.source_commit}"))
		}
		shape := []string{}
		for _, step := range overlay.Field(recipe, "pipeline").Content {
			key := "runs"
			if n := overlay.Value(step, "name"); n != "" {
				key = "name=" + n
			} else if u := overlay.Value(step, "uses"); u != "" {
				key = "uses=" + u
			}
			shape = append(shape, key)
		}
		d.Wolfi = &Derivation{Recipe: name, Revision: 1, Spec: overlay.Spec{Sources: []string{source}, Checks: []overlay.Edit{{Path: []string{"pipeline", "uses=git-checkout"}, Expect: *expected}, {Path: []string{"pipeline"}, Shape: shape}}}}
	} else {
		recipe, err := overlay.Node(map[string]any{"package": map[string]any{"name": parts[1], "version": "0.0.0", "epoch": 0, "description": "Describe " + parts[1], "copyright": []any{map[string]string{"license": "NOASSERTION"}}}, "pipeline": []any{map[string]string{"name": "Replace with the reviewed source and build steps", "runs": "echo 'Complete the package recipe before building' >&2\nexit 1\n"}}})
		if err != nil {
			return "", err
		}
		d.Melange = *recipe
	}
	data, err := YAML(d)
	if err != nil {
		return "", err
	}
	data = append([]byte("# yaml-language-server: $schema=../../Werewolf/package.schema.json\n"), data...)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.Mkdir(path, 0755); err != nil {
		return "", err
	}
	if len(fixture) > 0 {
		dir := filepath.Join(path, "tests/fixtures")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "upstream.yaml"), fixture, 0644); err != nil {
			return "", err
		}
	}
	output := filepath.Join(path, "package.yaml")
	if err := os.WriteFile(output, data, 0644); err != nil {
		return "", err
	}
	return output, nil
}
