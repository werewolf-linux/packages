// Package catalog discovers package contexts and validates their maintenance contracts.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	Source         string            `json:"source"`
	Separator      string            `json:"separator,omitempty"`
	Repo           string            `json:"repo,omitempty"`
	URL            string            `json:"url,omitempty"`
	Project        string            `json:"project,omitempty"`
	Prefix         string            `json:"prefix,omitempty"`
	Pattern        string            `json:"pattern,omitempty"`
	Suffix         string            `json:"suffix,omitempty"`
	VersionPattern string            `json:"version-pattern,omitempty"`
	VersionReplace string            `json:"version-replace,omitempty"`
	Series         string            `json:"series,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	Lock           string            `json:"lock,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Hold           string            `json:"hold,omitempty"`
	Components     map[string]Policy `json:"components,omitempty"`
	CommitVar      string            `json:"commit-var,omitempty"`
	Recipe         string            `json:"recipe,omitempty"`
	Family         string            `json:"family,omitempty"`
	Upstream       *Policy           `json:"upstream,omitempty"`
}

// Metadata records why a package exists and the evidence supporting its policy.
type Metadata struct {
	Update    Policy `json:"update"`
	Hardening struct {
		Minimized       *bool    `json:"minimized"`
		Pledge          *bool    `json:"pledge"`
		MinimizedReason string   `json:"minimized_reason"`
		PledgeReason    string   `json:"pledge_reason"`
		Evidence        []string `json:"evidence"`
	} `json:"hardening"`
	Wolfi struct {
		Relationship string   `json:"relationship"`
		Recipes      []string `json:"recipes"`
		Changes      []struct {
			What     string   `json:"what"`
			Why      string   `json:"why"`
			Evidence []string `json:"evidence"`
		} `json:"changes"`
		RemovalCondition string `json:"removal_condition"`
	} `json:"wolfi"`
}

// Package is a stable package identity and its repository-relative context.
type Package struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Engine string `json:"engine"`
	Metadata
}

var Name = regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`)
var repository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var gitURL = regexp.MustCompile(`^https://[A-Za-z0-9./_-]+$`)
var project = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Decode rejects unknown fields, trailing JSON, and missing input.
func Decode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

// Load validates every context, including orphan recipes that would otherwise
// disappear silently from update and publication jobs.
func Load(root string) ([]Package, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "metadata.json"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var packages []Package
	for _, path := range paths {
		directory := filepath.Dir(path)
		name := filepath.Base(directory)
		category := filepath.Base(filepath.Dir(directory))
		if !slices.Contains([]string{"databases", "devel", "games", "graphics", "lang", "mail", "math", "multimedia", "net", "net-im", "security", "www"}, category) {
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var meta Metadata
		if err := Decode(data, &meta); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validate(directory, meta); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, err
		}
		engine := "upstream"
		if meta.Update.Source == "wolfi" {
			engine = "wolfi"
		}
		packages = append(packages, Package{Name: name, Path: relative, Engine: engine, Metadata: meta})
		seen[name] = true
	}
	for _, pattern := range []string{"*.yaml"} {
		candidates, err := filepath.Glob(filepath.Join(root, "*", "*", pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range candidates {
			category := filepath.Base(filepath.Dir(filepath.Dir(path)))
			if slices.Contains([]string{"scripts", "tests", "wolfi", "build", ".github", "internal"}, category) {
				continue
			}
			if !seen[filepath.Base(filepath.Dir(path))] {
				return nil, fmt.Errorf("missing package metadata: %s", path)
			}
		}
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("empty package catalog")
	}
	slices.SortFunc(packages, func(a, b Package) int { return strings.Compare(a.Name, b.Name) })
	return packages, nil
}

func validate(directory string, m Metadata) error {
	name := filepath.Base(directory)
	p := m.Update
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
	recipes, err := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if err != nil {
		return err
	}
	if p.Source == "wolfi" {
		if (p.Recipe == "") == (p.Family == "") {
			return fmt.Errorf("choose exactly one Wolfi recipe or family")
		}
		if p.Upstream == nil || !slices.Contains([]string{"github", "github-tags", "openjdk", "git-tags"}, p.Upstream.Source) {
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
		if len(recipes) != 1 || filepath.Base(recipes[0]) != "overlay.yaml" {
			return fmt.Errorf("Wolfi context needs overlay.yaml and no maintained recipe")
		}
		if _, err := os.Stat(filepath.Join(directory, "policy.json")); err != nil {
			return err
		}
	} else if len(recipes) != 1 || filepath.Base(recipes[0]) == "overlay.yaml" {
		return fmt.Errorf("upstream context needs exactly one maintained recipe")
	}
	h := m.Hardening
	if h.Minimized == nil || h.Pledge == nil || strings.TrimSpace(h.MinimizedReason) == "" || strings.TrimSpace(h.PledgeReason) == "" {
		return fmt.Errorf("hardening flags need booleans and reasons")
	}
	if err := evidence(directory, h.Evidence); err != nil {
		return err
	}
	w := m.Wolfi
	if !slices.Contains([]string{"derived", "replacement", "independent"}, w.Relationship) || (p.Source == "wolfi") != (w.Relationship == "derived") {
		return fmt.Errorf("invalid Wolfi relationship")
	}
	if w.Relationship != "independent" && len(w.Recipes) == 0 {
		return fmt.Errorf("Wolfi baseline is required")
	}
	if strings.TrimSpace(w.RemovalCondition) == "" || len(w.Changes) == 0 {
		return fmt.Errorf("explain divergence and its removal condition")
	}
	for _, change := range w.Changes {
		if strings.TrimSpace(change.What) == "" || strings.TrimSpace(change.Why) == "" {
			return fmt.Errorf("divergence needs what and why")
		}
		if err := evidence(directory, change.Evidence); err != nil {
			return err
		}
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
