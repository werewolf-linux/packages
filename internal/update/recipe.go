package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type recipe struct {
	path  string
	text  string
	root  *yaml.Node
	edits map[int]string
}

func field(n *yaml.Node, name string) *yaml.Node {
	if n == nil {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == name {
			return n.Content[i+1]
		}
	}
	return nil
}

func value(n *yaml.Node, name string) string {
	if f := field(n, name); f != nil {
		return f.Value
	}
	return ""
}

func readRecipe(path string) (*recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 1 && filepath.Base(path) == "package.yaml" {
		if native := field(doc.Content[0], "melange"); native != nil {
			doc.Content[0] = native
		}
	}
	if len(doc.Content) != 1 || field(doc.Content[0], "package") == nil {
		return nil, fmt.Errorf("missing package in %s", path)
	}
	return &recipe{path: path, text: string(data), root: doc.Content[0], edits: map[int]string{}}, nil
}

func (r *recipe) set(n *yaml.Node, v string) error {
	if n == nil || n.Kind != yaml.ScalarNode || strings.ContainsAny(n.Value, "\r\n") {
		return fmt.Errorf("expected single-line recipe scalar")
	}
	if n.Value == v {
		return nil
	}
	lines := strings.Split(r.text, "\n")
	line := lines[n.Line-1]
	// Only replace the scalar; retain surrounding recipe formatting/comments.
	suffix := ""
	if n.LineComment != "" {
		suffix = " " + n.LineComment
	}
	encoded := strconv.Quote(v)
	if n.Tag == "!!int" {
		encoded = v
	}
	r.edits[n.Line-1] = line[:n.Column-1] + encoded + suffix
	n.Value = v
	return nil
}

func (r *recipe) render() string {
	lines := strings.Split(r.text, "\n")
	for i, text := range r.edits {
		lines[i] = text
	}
	return strings.Join(lines, "\n")
}

func walk(n *yaml.Node, visit func(*yaml.Node) error) error {
	if n == nil {
		return nil
	}
	if err := visit(n); err != nil {
		return err
	}
	for _, child := range n.Content {
		if err := walk(child, visit); err != nil {
			return err
		}
	}
	return nil
}

func (r *recipe) expand(text, version string) (string, error) {
	text = strings.ReplaceAll(text, "${{package.version}}", version)
	vars := field(r.root, "vars")
	if vars != nil {
		for i := 0; i+1 < len(vars.Content); i += 2 {
			text = strings.ReplaceAll(text, "${{vars."+vars.Content[i].Value+"}}", vars.Content[i+1].Value)
		}
	}
	if strings.Contains(text, "${{") {
		return "", fmt.Errorf("unresolved source template: %s", text)
	}
	return text, nil
}

// sources refreshes every version-dependent fetch, including both architectures
// and nested pipelines, and pins git-checkout to the release's actual commit.
func (c *Client) sources(ctx context.Context, r *recipe, p Policy, release Release, primaryChanged bool, variables map[string]string) error {
	return walk(r.root, func(n *yaml.Node) error {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		with := field(n, "with")
		switch value(n, "uses") {
		case "git-checkout":
			if !primaryChanged {
				return nil
			}
			commit := release.Commit
			if commit == "" {
				tag, err := r.expand(value(with, "tag"), release.Version)
				if p.Separator != "" {
					tag = release.Tag
					if err := r.set(field(with, "tag"), tag); err != nil {
						return err
					}
				}
				if err != nil {
					return err
				}
				repo := strings.TrimSuffix(strings.TrimPrefix(value(with, "repository"), "https://github.com/"), ".git")
				if tag == "" || repo == "" {
					return fmt.Errorf("git source needs a tag and repository")
				}
				commit, err = c.commit(ctx, repo, tag)
				if err != nil {
					return err
				}
			}
			return r.set(field(with, "expected-commit"), commit)
		case "fetch":
			uri := field(with, "uri")
			if uri == nil {
				return fmt.Errorf("fetch has no URI")
			}
			refresh := primaryChanged && strings.Contains(uri.Value, "${{package.version}}")
			for name := range variables {
				refresh = refresh || strings.Contains(uri.Value, "${{vars."+name+"}}")
			}
			if !refresh && p.Source != "content" && p.Source != "minecraft" {
				return nil
			}
			address, err := r.expand(uri.Value, release.Version)
			if err != nil {
				return err
			}
			if p.Source == "minecraft" {
				address = release.URL
				if err := r.set(uri, address); err != nil {
					return err
				}
			}
			digest, err := c.Download(ctx, address, io.Discard)
			if err != nil {
				return err
			}
			return r.set(field(with, "expected-sha256"), digest)
		}
		return nil
	})
}

// LoadPolicies requires every context to declare a policy. Adding a package
// without tracking it, or disabling a production package, fails the offline CI.
func LoadPolicies(root string) (map[string]Policy, error) {
	var data []byte
	var err error
	var paths []string
	policies := map[string]Policy{}
	definitions, _ := filepath.Glob(filepath.Join(root, "*/*/package.yaml"))
	if _, statErr := os.Stat(filepath.Join(root, "pipelines")); statErr == nil || len(definitions) > 0 {
		policies, paths, err = catalogPolicies(root)
		if err != nil {
			return nil, err
		}
	} else {
		data, err = os.ReadFile(filepath.Join(root, "updates.json"))
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policies); err != nil {
			return nil, err
		}
		paths, err = filepath.Glob(filepath.Join(root, "*", "*.yaml"))
		if err != nil {
			return nil, err
		}
	}
	contexts := map[string]int{}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		if strings.HasPrefix(name, ".") {
			continue
		}
		contexts[name]++
		if _, ok := policies[name]; !ok {
			return nil, fmt.Errorf("%s has no update policy", name)
		}
	}
	for name, p := range policies {
		if contexts[name] != 1 {
			return nil, fmt.Errorf("%s needs exactly one recipe", name)
		}
		directory, err := ContextPath(root, name)
		if err != nil {
			return nil, err
		}
		recipePaths, _ := filepath.Glob(filepath.Join(directory, "*.yaml"))
		r, err := readRecipe(recipePaths[0])
		if err != nil {
			return nil, err
		}
		for variable, component := range p.Components {
			if field(field(r.root, "vars"), variable) == nil || (component.CommitVar != "" && field(field(r.root, "vars"), component.CommitVar) == nil) {
				return nil, fmt.Errorf("%s: missing component variable %s", name, variable)
			}
			if component.Source != "github" || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(component.Repo) || len(component.Components) != 0 || component.Lock != "" {
				return nil, fmt.Errorf("%s: component %s needs a stable GitHub release source", name, variable)
			}
		}
		if strings.HasSuffix(name, "-vuln-demo") != (p.Source == "pinned" || p.Hold != "") {
			return nil, fmt.Errorf("only vuln-demo contexts may be pinned: %s", name)
		}
		switch p.Source {
		case "github", "github-releases", "github-tags", "git":
			if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(p.Repo) {
				return nil, fmt.Errorf("invalid GitHub repository: %s", name)
			}
		case "pypi", "npm":
			if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(p.Project) {
				return nil, fmt.Errorf("invalid registry project: %s", name)
			}
		case "pinned", "content":
			if p.Reason == "" {
				return nil, fmt.Errorf("%s needs an explanation", name)
			}
		case "minecraft", "plex":
		default:
			return nil, fmt.Errorf("unknown source for %s", name)
		}
		if p.Separator != "" && (p.Separator != "_" || (p.Source != "github" && p.Source != "github-releases" && p.Source != "github-tags")) {
			return nil, fmt.Errorf("unsupported version separator: %s", name)
		}
		if p.Pattern != "" {
			re, err := regexp.Compile(p.Pattern)
			if err != nil || re.NumSubexp() != 2 {
				return nil, fmt.Errorf("invalid release pattern: %s", name)
			}
		}
		if p.Series != "" && (!stable.MatchString(p.Series) || p.Source != "github-tags" || p.Reason == "") {
			return nil, fmt.Errorf("version stream needs a numeric series, tag source and explanation: %s", name)
		}
		if p.Source == "git" && (p.Branch == "" || p.Reason == "") {
			return nil, fmt.Errorf("snapshot policy needs a branch and explanation")
		}
		switch p.Lock {
		case "", "npm", "bugsink", "langflow":
		default:
			return nil, fmt.Errorf("unknown lock handler: %s", p.Lock)
		}
	}
	return policies, nil
}

type Result struct {
	Package string `json:"package"`
	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// Runner is injectable so tests exercise resolver failures without executing npm
// or uv. Commands execute in scratch contexts with a small, explicit environment.
type Runner func(context.Context, string, string, ...string) error

func RunCommand(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	for _, k := range []string{"PATH", "HOME", "LANG", "SSL_CERT_FILE", "TMPDIR", "QEMU_KERNEL_IMAGE", "QEMU_BASE_INITRAMFS", "QEMU_DISKS_PATH"} {
		if v, ok := os.LookupEnv(k); ok {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Env = append(cmd.Env, "UV_PYTHON_DOWNLOADS=never", "UV_NO_CACHE=1", "npm_config_ignore_scripts=true")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func copyContext(source, dest string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("context symlinks are forbidden: %s", path)
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dest, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), data, info.Mode().Perm())
	})
}

func (c *Client) Update(ctx context.Context, root, name string, p Policy, write bool, run Runner) (result Result) {
	result = Result{Package: name, Status: "failed"}
	fail := func(err error) Result { result.Status = "failed"; result.Error = err.Error(); return result }
	if p.Source == "pinned" || p.Hold != "" {
		result.Status = "pinned"
		return result
	}
	directory, err := ContextPath(root, name)
	if err != nil {
		return fail(err)
	}
	expected, err := contextDigest(directory)
	if err != nil {
		return fail(err)
	}
	paths, err := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if err != nil || len(paths) != 1 {
		return fail(fmt.Errorf("expected one recipe"))
	}
	original, err := readRecipe(paths[0])
	if err != nil {
		return fail(err)
	}
	result.Current = value(field(original.root, "package"), "version")
	if p.Source == "plex" {
		release, err := c.Discover(ctx, p, result.Current)
		if err != nil {
			return fail(err)
		}
		result.Latest = release.Version
		data, err := os.ReadFile(filepath.Join(directory, "sources.json"))
		if err != nil {
			return fail(err)
		}
		var captured struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &captured); err != nil {
			return fail(err)
		}
		if Compare(release.Version, result.Current) < 0 {
			return fail(fmt.Errorf("vendor release would downgrade Plex"))
		}
		if captured.Version == release.Build {
			result.Status = "current"
			return result
		}
		result.Status = "available"
		if !write {
			return result
		}
		scratch, err := os.MkdirTemp("", "plex-update-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(scratch)
		if err := copyContext(directory, scratch); err != nil {
			return fail(err)
		}
		if err := run(ctx, scratch, "go", "-C", root, "run", "./cmd/package", "plex-update", "--root", scratch, "--expected-version", release.Build); err != nil {
			return fail(err)
		}
		prepared, err := readRecipe(filepath.Join(scratch, filepath.Base(paths[0])))
		if err != nil {
			return fail(err)
		}
		if value(field(prepared.root, "package"), "version") != release.Version {
			return fail(fmt.Errorf("Plex update did not prepare the discovered release"))
		}
		if err := queryRecipe(ctx, scratch, prepared.path, run); err != nil {
			return fail(err)
		}
		if err := installContext(scratch, directory, expected); err != nil {
			return fail(err)
		}
		result.Status = "updated"
		return result
	}

	release, err := c.Discover(ctx, p, result.Current)
	if err != nil {
		return fail(err)
	}
	result.Latest = release.Version
	result.Status = "current"
	// Never downgrade a stable pin when an upstream API lags or omits it.
	if stable.MatchString(result.Current) && Compare(release.Version, result.Current) < 0 {
		return fail(fmt.Errorf("upstream %s is older than pinned %s", release.Version, result.Current))
	}
	changed := Compare(release.Version, result.Current) > 0
	if p.Source == "git" {
		changed = release.Version != result.Current
		_ = walk(original.root, func(n *yaml.Node) error {
			if value(n, "uses") == "git-checkout" && value(field(n, "with"), "expected-commit") != release.Commit {
				changed = true
			}
			return nil
		})
	}
	if release.Build != "" {
		changed = changed || value(field(original.root, "vars"), "build") != release.Build
	}
	primaryChanged := changed
	variables := map[string]string{}
	for _, variable := range Names(p.Components) {
		component := p.Components[variable]
		current := value(field(original.root, "vars"), variable)
		latest, err := c.Discover(ctx, component, current)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", variable, err))
		}
		if Compare(latest.Version, current) < 0 {
			return fail(fmt.Errorf("%s: upstream %s is older than pinned %s", variable, latest.Version, current))
		}
		if Compare(latest.Version, current) == 0 {
			continue
		}
		variables[variable] = latest.Version
		if component.CommitVar != "" {
			commit, err := c.commit(ctx, component.Repo, latest.Tag)
			if err != nil {
				return fail(err)
			}
			variables[component.CommitVar] = commit
		}
	}
	changed = changed || len(variables) != 0
	if !changed && p.Source != "content" {
		return result
	}
	result.Status = "available"
	if !write && p.Source != "content" {
		return result
	}
	result.Status = "failed"
	scratch, err := os.MkdirTemp("", "package-update-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(scratch)
	if err := copyContext(directory, scratch); err != nil {
		return fail(err)
	}
	r, err := readRecipe(filepath.Join(scratch, filepath.Base(paths[0])))
	if err != nil {
		return fail(err)
	}
	for variable, version := range variables {
		if err := r.set(field(field(r.root, "vars"), variable), version); err != nil {
			return fail(err)
		}
	}
	if release.Build != "" {
		if err := r.set(field(field(r.root, "vars"), "build"), release.Build); err != nil {
			return fail(err)
		}
	}
	// These release download paths contain the major/minor series separately.
	parts := strings.Split(release.Version, ".")
	if len(parts) >= 2 && (name == "mediawiki" || name == "moodle") {
		err := walk(r.root, func(n *yaml.Node) error {
			if n.Kind != yaml.MappingNode {
				return nil
			}
			uri := field(n, "uri")
			if uri == nil {
				return nil
			}
			v := uri.Value
			if name == "mediawiki" {
				v = regexp.MustCompile(`/mediawiki/[0-9.]+/`).ReplaceAllString(v, "/mediawiki/"+strings.Join(parts[:2], ".")+"/")
			}
			if name == "moodle" {
				minor, _ := strconv.Atoi(parts[1])
				v = regexp.MustCompile(`/stable[0-9]+/`).ReplaceAllString(v, fmt.Sprintf("/stable%s%02d/", parts[0], minor))
			}
			return r.set(uri, v)
		})
		if err != nil {
			return fail(err)
		}
	}
	if err := c.sources(ctx, r, p, release, primaryChanged, variables); err != nil {
		return fail(err)
	}
	if p.Source == "content" {
		if r.render() == r.text {
			result.Status = "current"
			return result
		}
		if !write {
			result.Status = "available"
			return result
		}
	}
	pkg := field(r.root, "package")
	epoch := "0"
	if release.Version == result.Current {
		epoch, err = numericEpoch(value(pkg, "epoch"))
		if err != nil {
			return fail(err)
		}
	}
	if err := r.set(field(pkg, "version"), release.Version); err != nil {
		return fail(err)
	}
	if err := r.set(field(pkg, "epoch"), epoch); err != nil {
		return fail(err)
	}
	text := r.render()
	if name == "open-webui" {
		text = regexp.MustCompile(`--exclude-newer [0-9TZ:+.-]+`).ReplaceAllString(text, "--exclude-newer "+time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}
	if err := os.WriteFile(r.path, []byte(text), 0o644); err != nil {
		return fail(err)
	}
	if primaryChanged {
		if err := c.locks(ctx, scratch, p, release, run); err != nil {
			return fail(err)
		}
	}
	if err := queryRecipe(ctx, scratch, r.path, run); err != nil {
		return fail(err)
	}
	// Nothing in the real context is touched until downloads, locks and parsing
	// all succeed. Resolver failures leave even the recipe's bytes unchanged.
	if err := installContext(scratch, directory, expected); err != nil {
		return fail(err)
	}
	result.Status = "updated"
	return result
}

// Install a complete staged directory, checking that discovery did not race a
// maintainer edit. Directory renames keep recipe and dependency locks together.
func installContext(source, dest, expected string) error {
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".package-update-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(stage)
		}
	}()
	next := filepath.Join(stage, "next")
	if err := copyContext(source, next); err != nil {
		return err
	}
	current, err := contextDigest(dest)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("package changed during update; retry from the new inputs")
	}
	previous := filepath.Join(stage, "previous")
	if err := os.Rename(dest, previous); err != nil {
		return err
	}
	// Check again after taking ownership of the old directory, before promotion.
	current, err = contextDigest(previous)
	if err == nil && current != expected {
		err = fmt.Errorf("package changed during update; retry from the new inputs")
	}
	if err == nil {
		err = os.Rename(next, dest)
	}
	if err != nil {
		if rollback := os.Rename(previous, dest); rollback != nil {
			cleanup = false
			return fmt.Errorf("update: %w; rollback: %v; preserved prior context at %s", err, rollback, previous)
		}
		return err
	}
	return nil
}

func contextDigest(directory string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular package input: %s", path)
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%o\x00%d\x00", filepath.ToSlash(relative), info.Mode().Perm(), len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func Names(policies map[string]Policy) []string {
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate an extracted native recipe outside the update transaction directory.
func queryRecipe(ctx context.Context, directory, path string, run Runner) error {
	r, err := readRecipe(path)
	if err != nil {
		return err
	}
	if filepath.Base(path) != "package.yaml" {
		return run(ctx, directory, "melange", "query", path, "{{.Package.Version}}{{println}}")
	}
	data, err := yaml.Marshal(r.root)
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "package-query-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	native := filepath.Join(temp, "recipe.yaml")
	if err := os.WriteFile(native, data, 0644); err != nil {
		return err
	}
	return run(ctx, directory, "melange", "query", native, "{{.Package.Version}}{{println}}")
}
