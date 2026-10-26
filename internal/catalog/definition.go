package catalog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/werewolf-linux/packages/internal/overlay"
	"github.com/werewolf-linux/packages/internal/transform"
	"gopkg.in/yaml.v3"
)

// Source uses Melange's GitHub vocabulary and explicit adapters for other sources.
type Source struct {
	Policy `yaml:",inline"`
	GitHub *GitHub `yaml:"github,omitempty"`
}

type GitHub struct {
	Identifier  string `yaml:"identifier"`
	StripPrefix string `yaml:"strip-prefix,omitempty"`
	StripSuffix string `yaml:"strip-suffix,omitempty"`
	UseTag      bool   `yaml:"use-tag,omitempty"`
}

func (s Source) policy() (Policy, error) {
	p := s.Policy
	if s.GitHub != nil {
		if p.Source != "" || p.Repo != "" || p.Prefix != "" || p.Suffix != "" {
			return p, fmt.Errorf("github and source/repo/prefix/suffix are mutually exclusive")
		}
		p.Source, p.Repo, p.Prefix, p.Suffix = "github", s.GitHub.Identifier, s.GitHub.StripPrefix, s.GitHub.StripSuffix
		if s.GitHub.UseTag {
			p.Source = "github-tags"
		}
	}
	return p, nil
}

type Derivation struct {
	Recipe          string `yaml:"recipe,omitempty"`
	Family          string `yaml:"family,omitempty"`
	Revision        int    `yaml:"revision"`
	MaxFailPerMille int    `yaml:"max-fail-per-mille,omitempty"`
	overlay.Spec    `yaml:",inline"`
}

type Change struct {
	What     string   `yaml:"what"`
	Why      string   `yaml:"why"`
	Evidence []string `yaml:"evidence"`
	Claims   []string `yaml:"claims,omitempty"`
}

// Test contains harness controls outside native Melange's test configuration.
type Test struct {
	Confined       bool     `yaml:"confined,omitempty"`
	Allow          []string `yaml:"allow,omitempty"`
	PreserveMtimes []string `yaml:"preserve-mtimes,omitempty"`
}

type Definition struct {
	Update   Source                      `yaml:"update"`
	Melange  yaml.Node                   `yaml:"melange,omitempty"`
	Wolfi    *Derivation                 `yaml:"wolfi,omitempty"`
	Changes  []Change                    `yaml:"changes"`
	Replaces []string                    `yaml:"replaces,omitempty"`
	DropWhen string                      `yaml:"drop-when,omitempty"`
	Actions  map[string]transform.Action `yaml:"actions,omitempty"`
	Test     Test                        `yaml:"test,omitempty"`
}

// Policy adapts the definition to the release engines. Baseline and release
// selection have separate owners in the maintained document.
func (d Definition) Policy() Policy {
	p, _ := d.Update.policy() // Read validates before callers receive a definition.
	if d.Wolfi != nil {
		return Policy{Source: "wolfi", Recipe: d.Wolfi.Recipe, Family: d.Wolfi.Family, Upstream: &p, Hold: p.Hold}
	}
	return p
}

// Read validates structure without requiring source files to exist in build guests.
// Load additionally validates evidence and repository maintenance constraints.
func Read(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Definition
	if _, err := overlay.Parse(data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := Decode(data, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := d.Update.policy(); err != nil {
		return nil, fmt.Errorf("%s: update: %w", path, err)
	}
	if (d.Wolfi == nil) == (d.Melange.Kind == 0) {
		return nil, fmt.Errorf("%s: choose exactly one of melange and wolfi", path)
	}
	if d.Wolfi != nil {
		w := d.Wolfi
		if (w.Recipe == "") == (w.Family == "") {
			return nil, fmt.Errorf("%s: choose one Wolfi recipe or family", path)
		}
		if w.Revision < 1 || w.MaxFailPerMille < 0 || w.MaxFailPerMille > 55 {
			return nil, fmt.Errorf("%s: invalid revision or failure ceiling", path)
		}
		if w.Title == "" {
			w.Title = filepath.Base(filepath.Dir(path))
		}
		if err := w.Spec.Validate(); err != nil {
			return nil, fmt.Errorf("%s: wolfi: %w", path, err)
		}
	} else {
		if d.Melange.Kind != yaml.MappingNode || overlay.Field(&d.Melange, "package") == nil {
			return nil, fmt.Errorf("%s: melange needs a native package recipe", path)
		}
		if overlay.Field(&d.Melange, "update") != nil {
			return nil, fmt.Errorf("%s: declare update only at the document root", path)
		}
	}
	if d.Actions != nil {
		if err := (transform.Plan{Actions: d.Actions}).Validate(); err != nil {
			return nil, fmt.Errorf("%s: actions: %w", path, err)
		}
	}
	return &d, nil
}

// Recipe returns a standalone native Melange document. Release discovery is owned
// by the wrapper, so an exported recipe cannot invoke a competing update policy.
func (d Definition) Recipe() ([]byte, error) {
	if d.Melange.Kind == 0 {
		return nil, fmt.Errorf("Wolfi recipe requires captured inputs")
	}
	return YAML(&d.Melange)
}

func YAML(value any) ([]byte, error) {
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
