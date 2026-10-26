package overlay

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Lock is the source identity needed to derive a recipe.
type Lock struct {
	Recipe       string `json:"recipe"`
	Version      string `json:"version"`
	Epoch        int    `json:"epoch"`
	Revision     int    `json:"revision"`
	SourceCommit string `json:"source_commit"`
}

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)
var sourceURL = regexp.MustCompile(`^https://[A-Za-z0-9./_-]+$`)
var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`)
var patchDirectory = regexp.MustCompile(`^[A-Za-z0-9/_-]+$`)

// SourceCommit resolves the recipe's single checkout pin, including a vars pin.
func SourceCommit(recipe *yaml.Node) (string, error) {
	checkout, err := Select(recipe, []string{"pipeline", "uses=git-checkout", "with"})
	if err != nil {
		return "", err
	}
	pin := Value(checkout, "expected-commit")
	if key, ok := strings.CutPrefix(pin, "${{vars."); ok && strings.HasSuffix(key, "}}") {
		pin = Value(Field(recipe, "vars"), strings.TrimSuffix(key, "}}"))
	}
	if !commitID.MatchString(pin) {
		return "", fmt.Errorf("source must name an exact commit: %q", pin)
	}
	return pin, nil
}

// Variables supplies the explicit substitutions used by an overlay.
func Variables(recipe *yaml.Node, lock Lock, maxFailPerMille int, shared string) (map[string]string, error) {
	checkout, err := Select(recipe, []string{"pipeline", "uses=git-checkout", "with"})
	if err != nil {
		return nil, err
	}
	vars := map[string]string{
		"lock.source_commit": lock.SourceCommit, "lock.version": lock.Version,
		"lock.feature":          strings.Split(lock.Version, ".")[0],
		"upstream.name":         Value(Field(recipe, "package"), "name"),
		"upstream.tag":          Value(checkout, "tag"),
		"policy.max_fail_ratio": fmt.Sprintf("%.3f", float64(maxFailPerMille)/1000),
	}
	baseline := strings.TrimSuffix(lock.Recipe, ".yaml")
	if at := strings.LastIndex(baseline, "-"); at >= 0 {
		baseline = baseline[at+1:]
	}
	vars["baseline.feature"] = strings.Split(baseline, ".")[0]
	data, err := os.ReadFile(filepath.Join(shared, "root-probe.sh"))
	if err != nil {
		return nil, err
	}
	for _, runtime := range []string{"java", "node", "php", "python"} {
		command := map[string]string{"java": "java -version", "node": "node --version", "php": "php -v", "python": "python3 -V"}[runtime]
		vars["shared:root-probe-"+runtime] = strings.NewReplacer("@COMMAND@", command, "@RUNTIME@", runtime).Replace(string(data))
	}
	return vars, nil
}

// Derive applies the package's reviewed edits, then adds the common source,
// patch, version, provenance, and update policy.
func Derive(spec *Spec, upstream *yaml.Node, lock Lock, directory string, vars map[string]string) (*yaml.Node, error) {
	if lock.Revision < 1 || lock.Epoch < 0 || lock.Epoch >= 1000 {
		return nil, fmt.Errorf("invalid policy revision or Wolfi epoch")
	}
	multiplier := spec.EpochMultiplier
	if multiplier == 0 {
		multiplier = 1000
	}
	if lock.Revision > (math.MaxInt-lock.Epoch)/multiplier {
		return nil, fmt.Errorf("policy revision overflows epoch")
	}
	if Value(Field(upstream, "package"), "version") != lock.Version {
		return nil, fmt.Errorf("source version differs from lock")
	}
	pin, err := SourceCommit(upstream)
	if err != nil {
		return nil, err
	}
	if pin != lock.SourceCommit {
		return nil, fmt.Errorf("source commit differs from lock")
	}
	checked, err := Apply(upstream, spec.Checks, directory, vars)
	if err != nil {
		return nil, err
	}
	result, err := Apply(checked, spec.Edits, directory, vars)
	if err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(spec.Sources))
	for _, source := range spec.Sources {
		n := String(source)
		if err := Expand(n, directory, vars); err != nil {
			return nil, err
		}
		if !sourceURL.MatchString(n.Value) {
			return nil, fmt.Errorf("unsafe source URL %q", n.Value)
		}
		sources = append(sources, n.Value)
	}
	pipeline := Field(result, "pipeline")
	i, err := position(pipeline, "uses=git-checkout")
	if err != nil || i < 0 {
		return nil, fmt.Errorf("one source checkout is required")
	}
	old := pipeline.Content[i]
	fetchScript := fmt.Sprintf(`git init -q .
for delay in 0 15 45; do
  sleep "$delay"
  for source in %[2]s; do
    if git -c core.hooksPath=/dev/null -c fetch.fsckObjects=true fetch --no-tags --depth=1 "$source" %[1]s; then
      test "$(git rev-parse FETCH_HEAD)" = %[1]s
      git -c core.hooksPath=/dev/null checkout -q --detach %[1]s
      exit 0
    fi
  done
done
echo "%[1]s: every source failed, three times" >&2
exit 1
`, lock.SourceCommit, strings.Join(sources, " "))
	fetch, err := Node(map[string]any{"name": "Fetch the locked source commit", "needs": map[string]any{"packages": []string{"git"}}, "runs": fetchScript})
	if err != nil {
		return nil, err
	}
	if working := Field(old, "working-directory"); working != nil {
		Put(fetch, "working-directory", Clone(working))
	}
	pipeline.Content[i] = fetch
	if spec.Patches != "" {
		if !filepath.IsLocal(spec.Patches) || !patchDirectory.MatchString(spec.Patches) {
			return nil, fmt.Errorf("unsafe patch directory")
		}
		last := i
		for j, step := range pipeline.Content {
			if Value(step, "uses") == "patch" {
				if j < i {
					return nil, fmt.Errorf("patch precedes checkout")
				}
				last = j
			}
		}
		step, err := Node(map[string]any{"name": "Apply Werewolf policy patches", "needs": map[string]any{"packages": []string{"patch"}}, "runs": "while IFS= read -r name; do\n  patch -p1 --forward --batch --fuzz=2 --no-backup-if-mismatch -i \"" + spec.Patches + "/$name\" || exit 1\ndone < " + spec.Patches + "/series\n"})
		if err != nil {
			return nil, err
		}
		pipeline.Content = slices.Insert(pipeline.Content, last+1, step)
	}
	pkg := Field(result, "package")
	Put(pkg, "epoch", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(lock.Revision*multiplier + lock.Epoch)})
	Put(pkg, "description", String(Value(pkg, "description")+spec.DescriptionSuffix))
	sections := []*yaml.Node{pkg}
	if !spec.OriginMainOnly {
		if subs := Field(result, "subpackages"); subs != nil {
			sections = append(sections, subs.Content...)
		}
	}
	origin := "upstream-origin:" + Value(Field(upstream, "package"), "name") + ":" + lock.Version + "-r" + strconv.Itoa(lock.Epoch)
	for _, section := range sections {
		deps := Field(section, "dependencies")
		if deps == nil {
			deps = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			Put(section, "dependencies", deps)
		}
		provides := Field(deps, "provides")
		if provides == nil {
			provides = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			Put(deps, "provides", provides)
		}
		provides.Content = append(provides.Content, String(origin))
	}
	update, err := Node(map[string]bool{"enabled": false})
	if err != nil {
		return nil, err
	}
	Put(result, "update", update)
	if err := setupTests(result, &spec.TestSetup, directory, vars); err != nil {
		return nil, err
	}
	return result, nil
}

// TestRecipe adds build-only permissions to package tests, never to shipped APKs.
func TestRecipe(spec *Spec, recipe *yaml.Node, directory string, vars map[string]string) (*yaml.Node, error) {
	result, err := Apply(recipe, spec.TestEdits, directory, vars)
	if err != nil {
		return nil, err
	}
	if err := setupTests(result, &spec.SuiteSetup, directory, vars); err != nil {
		return nil, err
	}
	return result, nil
}

func setupTests(recipe, setup *yaml.Node, directory string, vars map[string]string) error {
	if setup.Kind == 0 {
		return nil
	}
	step := Clone(setup)
	if err := Expand(step, directory, vars); err != nil {
		return err
	}
	sections := []*yaml.Node{recipe}
	if subs := Field(recipe, "subpackages"); subs != nil {
		sections = append(sections, subs.Content...)
	}
	for _, section := range sections {
		pipeline := Field(Field(section, "test"), "pipeline")
		if pipeline != nil && len(pipeline.Content) > 0 {
			pipeline.Content = slices.Insert(pipeline.Content, 0, Clone(step))
		}
	}
	return nil
}

// OutputNames expands Melange's package, vars and range variables for publication.
func OutputNames(recipe *yaml.Node) ([]string, error) {
	pkg := Field(recipe, "package")
	name, version := Value(pkg, "name"), Value(pkg, "version")
	if name == "" {
		return nil, fmt.Errorf("missing package name")
	}
	names := map[string]bool{name: true}
	// Melange substitutes the recipe's own vars in names too: Samba's Python
	// bindings are py${{vars.python-version}}-samba.
	known := []string{"${{package.name}}", name, "${{package.version}}", version}
	if vars := Field(recipe, "vars"); vars != nil {
		for i := 0; i < len(vars.Content); i += 2 {
			if value := vars.Content[i+1]; value.Kind == yaml.ScalarNode {
				known = append(known, "${{vars."+vars.Content[i].Value+"}}", value.Value)
			}
		}
	}
	ranges := map[string][]string{}
	if data := Field(recipe, "data"); data != nil {
		for _, item := range data.Content {
			values := Field(item, "items")
			if values == nil {
				return nil, fmt.Errorf("range has no items")
			}
			for j := 0; j < len(values.Content); j += 2 {
				ranges[Value(item, "name")] = append(ranges[Value(item, "name")], values.Content[j].Value)
			}
		}
	}
	if subs := Field(recipe, "subpackages"); subs != nil {
		for _, sub := range subs.Content {
			if Field(sub, "if") != nil {
				return nil, fmt.Errorf("review conditional subpackage publication")
			}
			keys := []string{""}
			if r := Value(sub, "range"); r != "" {
				var ok bool
				keys, ok = ranges[r]
				if !ok {
					return nil, fmt.Errorf("unknown range %q", r)
				}
			}
			for _, key := range keys {
				n := strings.NewReplacer(slices.Concat(known, []string{"${{range.key}}", key})...).Replace(Value(sub, "name"))
				if strings.Contains(n, "${{") || !packageName.MatchString(n) {
					return nil, fmt.Errorf("unresolved or unsafe package name %q", n)
				}
				if names[n] {
					return nil, fmt.Errorf("duplicate package name %q", n)
				}
				names[n] = true
			}
		}
	}
	result := make([]string, 0, len(names))
	for n := range names {
		result = append(result, n)
	}
	slices.Sort(result)
	return result, nil
}
