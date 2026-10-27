package update

import (
	"context"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

func TestWolfiStanzaDiscoversAheadOfRecipe(t *testing.T) {
	recipe, err := overlay.Parse([]byte(`package: {name: example, version: '1.0'}
update:
  enabled: true
  version-separator: _
  ignore-regex-patterns: ['^v9_0$']
  github:
    identifier: example/project
    strip-prefix: v
    tag-filter-prefix: v
    use-tag: true
`))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := WolfiPolicy(recipe)
	if err != nil {
		t.Fatal(err)
	}
	c := mockClient(t, nil)
	c.Tags = func(_ context.Context, address string) (map[string]string, error) {
		if address != "https://github.com/example/project" {
			t.Fatal(address)
		}
		return map[string]string{"v1_0": strings.Repeat("a", 40), "v2_0": strings.Repeat("b", 40), "v9_0": strings.Repeat("c", 40), "v3_0-rc1": strings.Repeat("d", 40)}, nil
	}
	c.CommitTime = func(_ context.Context, address, tag, commit string) (int64, error) {
		if tag != "v2_0" || commit != strings.Repeat("b", 40) {
			t.Fatalf("%s %s", tag, commit)
		}
		return 1792670400, nil
	}
	release, err := c.ResolveRelease(context.Background(), policy)
	if err != nil || release.Version != "2.0" || release.Commit != strings.Repeat("b", 40) {
		t.Fatalf("%+v: %v", release, err)
	}
	if overlay.Value(overlay.Field(recipe, "package"), "version") != "1.0" {
		t.Fatal("changed baseline")
	}
}

func TestWolfiStanzaRequiresOverrideForUnsupportedPolicy(t *testing.T) {
	for _, stanza := range []string{
		"", "update: {enabled: false, github: {identifier: a/b}}",
		"update: {enabled: true}",
		"update: {enabled: true, github: {identifier: a/b}, git: {}}",
		"update: {enabled: true, release-monitor: {identifier: 1}}",
		"update: {enabled: true, git: {}, schedule: {period: weekly}}",
		"update: {enabled: true, github: {identifier: a/b, strip-prefix: v, tag-filter-prefix: v1.}}",
		"update: {enabled: true, github: {identifier: a/b, tag-filter: v1}}",
		"update: {enabled: true, github: {identifier: 'a/${{vars.repo}}'}}",
		"update: {enabled: true, github: {identifier: a/b, strp-prefix: v}}",
		"update: {enabled: true, github: {identifier: a/b}, ignore-regex-patterns: ['[']}",
		"update: {enabled: true, github: {identifier: a/b}, version-transform: [{match: x, replace: y}]}",
	} {
		recipe, err := overlay.Parse([]byte("package: {name: example}\n" + stanza))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := WolfiPolicy(recipe); err == nil {
			t.Errorf("accepted %s", stanza)
		}
	}
}

func TestWolfiGitUsesCheckoutRepository(t *testing.T) {
	recipe, err := overlay.Parse([]byte(`pipeline:
- uses: git-checkout
  with: {repository: https://example.org/project.git}
update:
  enabled: true
  git: {strip-prefix: v, tag-filter-prefix: v}
`))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := WolfiPolicy(recipe)
	if err != nil || policy.Source != "git-tags" || policy.URL != "https://example.org/project.git" || policy.Prefix != "v" {
		t.Fatalf("%+v: %v", policy, err)
	}
}
