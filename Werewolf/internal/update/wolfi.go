package update

import (
	"fmt"
	"regexp"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
	"gopkg.in/yaml.v3"
)

// WolfiPolicy reads release discovery from the captured packaging baseline.
// Unsupported settings require an explicit package update policy; never ignore
// a stream restriction or silently turn branch snapshots into tagged releases.
func WolfiPolicy(recipe *yaml.Node) (Policy, error) {
	stanza := overlay.Field(recipe, "update")
	if stanza == nil {
		return Policy{}, fmt.Errorf("Wolfi recipe has no update stanza; declare update explicitly")
	}
	type source struct {
		Identifier  string `yaml:"identifier,omitempty"`
		StripPrefix string `yaml:"strip-prefix,omitempty"`
		StripSuffix string `yaml:"strip-suffix,omitempty"`
		UseTag      bool   `yaml:"use-tag,omitempty"`
		TagPrefix   string `yaml:"tag-filter-prefix,omitempty"`
	}
	var spec struct {
		Enabled   bool     `yaml:"enabled"`
		GitHub    *source  `yaml:"github,omitempty"`
		Git       *source  `yaml:"git,omitempty"`
		Separator string   `yaml:"version-separator,omitempty"`
		Ignore    []string `yaml:"ignore-regex-patterns,omitempty"`
	}
	data, err := catalog.YAML(stanza)
	if err != nil {
		return Policy{}, err
	}
	if err := catalog.Decode(data, &spec); err != nil {
		return Policy{}, fmt.Errorf("unsupported Wolfi update stanza; declare update explicitly: %w", err)
	}
	if !spec.Enabled || (spec.GitHub == nil) == (spec.Git == nil) {
		return Policy{}, fmt.Errorf("Wolfi update must enable exactly one GitHub or git source; declare update explicitly")
	}
	s := spec.GitHub
	p := Policy{Source: "github", Separator: spec.Separator, IgnorePatterns: spec.Ignore}
	if spec.Git != nil {
		s = spec.Git
		checkout, err := overlay.Select(recipe, []string{"pipeline", "uses=git-checkout", "with"})
		if err != nil {
			return Policy{}, err
		}
		p.Source, p.URL = "git-tags", overlay.Value(checkout, "repository")
		if !gitURL.MatchString(p.URL) || s.Identifier != "" || s.UseTag {
			return Policy{}, fmt.Errorf("unsupported Wolfi git source; declare update explicitly")
		}
	} else {
		p.Repo = s.Identifier
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(p.Repo) {
			return Policy{}, fmt.Errorf("unsupported Wolfi GitHub identifier; declare update explicitly")
		}
		if s.UseTag {
			p.Source = "github-tags"
		}
	}
	p.Prefix, p.Suffix = s.StripPrefix, s.StripSuffix
	if s.TagPrefix != "" && s.TagPrefix != p.Prefix {
		return Policy{}, fmt.Errorf("Wolfi tag filter %q can restrict release streams; declare update explicitly", s.TagPrefix)
	}
	if p.Separator != "" && p.Separator != "_" {
		return Policy{}, fmt.Errorf("unsupported Wolfi version separator %q", p.Separator)
	}
	for _, pattern := range p.IgnorePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return Policy{}, fmt.Errorf("Wolfi ignore pattern: %w", err)
		}
	}
	return p, nil
}
