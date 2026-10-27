// Package catalog discovers package contexts and validates their maintenance contracts.
package catalog

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Policy describes an automatic release source. Wolfi recipes independently
// track an upstream policy while retaining a captured packaging baseline.
type Policy struct {
	IgnorePatterns []string          `json:"ignore_regex_patterns,omitempty" yaml:"ignore-regex-patterns,omitempty"`
	Source         string            `json:"source" yaml:"source,omitempty"`
	Separator      string            `json:"separator,omitempty" yaml:"separator,omitempty"`
	Repo           string            `json:"repo,omitempty" yaml:"repo,omitempty"`
	URL            string            `json:"url,omitempty" yaml:"url,omitempty"`
	Project        string            `json:"project,omitempty" yaml:"project,omitempty"`
	Prefix         string            `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Pattern        string            `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Suffix         string            `json:"suffix,omitempty" yaml:"suffix,omitempty"`
	VersionPattern string            `json:"version-pattern,omitempty" yaml:"version-pattern,omitempty"`
	VersionReplace string            `json:"version-replace,omitempty" yaml:"version-replace,omitempty"`
	Series         string            `json:"series,omitempty" yaml:"series,omitempty"`
	Branch         string            `json:"branch,omitempty" yaml:"branch,omitempty"`
	Lock           string            `json:"lock,omitempty" yaml:"lock,omitempty"`
	Reason         string            `json:"reason,omitempty" yaml:"reason,omitempty"`
	Hold           string            `json:"hold,omitempty" yaml:"hold,omitempty"`
	Components     map[string]Policy `json:"components,omitempty" yaml:"components,omitempty"`
	CommitVar      string            `json:"commit-var,omitempty" yaml:"commit-var,omitempty"`
	Recipe         string            `json:"recipe,omitempty" yaml:"-"`
	Family         string            `json:"family,omitempty" yaml:"-"`
	Upstream       *Policy           `json:"upstream,omitempty" yaml:"-"`
}

// Package is a stable identity and its repository-relative build context.
type Package struct {
	Name   string      `json:"name" yaml:"name"`
	Path   string      `json:"path" yaml:"path"`
	Engine string      `json:"engine" yaml:"engine"`
	Update Policy      `json:"update" yaml:"update"`
	Config *Definition `json:"-" yaml:"-"`
}

var Name = regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`)
var repository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var gitURL = regexp.MustCompile(`^https://[A-Za-z0-9./_-]+$`)
var project = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Category accepts descriptive package categories without a central category registry.
func Category(name string) bool {
	return Name.MatchString(name) && name != "." && name != ".." && !slices.Contains([]string{"cmd", "internal", "scripts", "tests", "docs", "build", "out", "pipelines", "tools", "release", "wolfi"}, name)
}

// Decode rejects unknown fields and multiple YAML documents.
func Decode(data []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one YAML document")
	}
	return nil
}

// Load validates every context, including orphan recipes that would otherwise
// disappear silently from update and publication jobs.
func Load(root string) ([]Package, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "package.yaml"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var packages []Package
	for _, path := range paths {
		directory := filepath.Dir(path)
		name := filepath.Base(directory)
		if strings.HasPrefix(name, ".") {
			continue
		}
		category := filepath.Base(filepath.Dir(directory))
		if !Category(category) {
			return nil, fmt.Errorf("unsupported category: %s", category)
		}
		if !Name.MatchString(name) || name == "." || name == ".." || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate package: %s", name)
		}
		for _, p := range []string{directory, filepath.Dir(directory)} {
			info, err := os.Lstat(p)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("symlinked context: %s", p)
			}
		}
		meta, err := Read(path)
		if err != nil {
			return nil, err
		}
		if err := validate(directory, *meta); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, err
		}
		engine := "upstream"
		if meta.Wolfi != nil {
			engine = "wolfi"
		}
		packages = append(packages, Package{Name: name, Path: relative, Engine: engine, Update: meta.Policy(), Config: meta})
		seen[name] = true
	}
	for _, pattern := range []string{"*.yaml"} {
		candidates, err := filepath.Glob(filepath.Join(root, "*", "*", pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range candidates {
			category := filepath.Base(filepath.Dir(filepath.Dir(path)))
			if !Category(category) {
				continue
			}
			if !seen[filepath.Base(filepath.Dir(path))] {
				return nil, fmt.Errorf("missing package.yaml: %s", path)
			}
		}
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("empty package catalog")
	}
	slices.SortFunc(packages, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })
	return packages, nil
}

func validate(directory string, m Definition) error {
	name := filepath.Base(directory)
	p := m.Policy()
	if !slices.Contains([]string{"wolfi", "github", "github-releases", "github-tags", "pypi", "npm", "minecraft", "git", "content", "plex"}, p.Source) {
		return fmt.Errorf("unsupported update source %q", p.Source)
	}
	vulnerable := strings.HasSuffix(name, "-vuln-demo")
	if (vulnerable && strings.TrimSpace(p.Hold) == "") || (!vulnerable && p.Hold != "") {
		return fmt.Errorf("only vulnerability demonstrations must hold updates with a reason")
	}
	if p.Separator != "" && p.Separator != "_" {
		return fmt.Errorf("unsupported version separator")
	}
	if slices.Contains([]string{"github", "github-releases", "github-tags", "git"}, p.Source) && !repository.MatchString(p.Repo) {
		return fmt.Errorf("invalid update repository")
	}
	if p.URL != "" {
		return fmt.Errorf("only a Wolfi package's git-tags upstream names a URL")
	}
	if slices.Contains([]string{"npm", "pypi"}, p.Source) && !project.MatchString(p.Project) {
		return fmt.Errorf("invalid registry project")
	}
	if slices.Contains([]string{"git", "content", "plex"}, p.Source) && strings.TrimSpace(p.Reason) == "" {
		return fmt.Errorf("update source needs a reason")
	}
	if p.Source == "git" && p.Branch == "" {
		return fmt.Errorf("snapshot source needs a branch")
	}
	if p.Source == "wolfi" && (p.Recipe == "") == (p.Family == "") {
		return fmt.Errorf("choose exactly one Wolfi recipe or family")
	}
	if p.Source == "wolfi" && p.Upstream != nil {
		if !slices.Contains([]string{"github", "github-tags", "openjdk", "git-tags"}, p.Upstream.Source) {
			return fmt.Errorf("Wolfi package needs an independent stable release source")
		}
		// A git-tags upstream, for a project without a GitHub mirror, names the
		// HTTPS URL git reads its tags from instead of a repository.
		if p.Upstream.Source == "git-tags" {
			if !gitURL.MatchString(p.Upstream.URL) || p.Upstream.Repo != "" {
				return fmt.Errorf("invalid upstream git URL")
			}
		} else if p.Upstream.URL != "" || p.Upstream.Source != "openjdk" && !repository.MatchString(p.Upstream.Repo) {
			return fmt.Errorf("invalid upstream repository")
		}
		if p.Upstream.Separator != "" && p.Upstream.Separator != "_" {
			return fmt.Errorf("unsupported upstream separator")
		}
		if (p.Upstream.VersionPattern == "") != (p.Upstream.VersionReplace == "") {
			return fmt.Errorf("version pattern and replacement must be paired")
		}
		if p.Upstream.VersionPattern != "" {
			if _, err := regexp.Compile(p.Upstream.VersionPattern); err != nil {
				return err
			}
		}
	}
	for _, change := range m.Changes {
		if strings.TrimSpace(change.What) == "" || strings.TrimSpace(change.Why) == "" {
			return fmt.Errorf("changes need what and why")
		}
		if err := evidence(directory, change.Evidence); err != nil {
			return err
		}
		for _, claim := range change.Claims {
			if claim != "minimized" && claim != "pledge" {
				return fmt.Errorf("unknown hardening claim %q", claim)
			}
		}
	}
	if len(m.Changes) == 0 {
		return fmt.Errorf("explain why this package is maintained")
	}

	return nil
}

func evidence(directory string, paths []string) error {
	var err error
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("hardening evidence is required")
	}
	for _, path := range paths {
		if !filepath.IsLocal(path) {
			return fmt.Errorf("unsafe evidence: %s", path)
		}
		full := filepath.Join(directory, path)
		info, err := os.Lstat(full)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("evidence must be a regular file: %s", path)
		}
		resolved, err := filepath.EvalSymlinks(full)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, resolved)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("evidence escapes context: %s", path)
		}
	}
	return nil
}
