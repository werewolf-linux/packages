package update

import (
	"context"
	"strings"
	"testing"
)

func TestReleaseVersionEncodings(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		p    Policy
		want string
	}{
		{"7.1.2-34", Policy{VersionPattern: `^([0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)$`, VersionReplace: "${1}.${2}"}, "7.1.2.34"},
		{"V_10_6_P1", Policy{VersionPattern: `^V_([0-9]+)_([0-9]+)_P([0-9]+)$`, VersionReplace: "${1}.${2}_p${3}"}, "10.6_p1"},
		{"jdk-27.0.1.0.3-ga", Policy{Prefix: "jdk-", Suffix: "-ga"}, "27.0.1.0.3"},
	} {
		got, ok := candidate(tc.tag, tc.p)
		if !ok || got.Version != tc.want {
			t.Fatalf("%s: %+v %v", tc.tag, got, ok)
		}
	}
	for _, tag := range []string{"jdk-28+21", "jdk-28-ea", "jdk-28-rc1-ga"} {
		if _, ok := candidate(tag, Policy{Prefix: "jdk-", Suffix: "-ga"}); ok {
			t.Fatal("accepted preview", tag)
		}
	}
}

func TestOpenJDKSelectsLatestReleasedFeatureAndItsGAUpdate(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/openjdk/jdk/tags?per_page=100&page=1":     `[{"name":"jdk-28+21"},{"name":"jdk-27-ga"},{"name":"jdk-26-ga"}]`,
		"https://api.github.com/repos/openjdk/jdk27u/tags?per_page=100&page=1":  `[{"name":"jdk-27.0.2+1"},{"name":"jdk-27.0.1.0.3-ga"},{"name":"jdk-27.0.1.0.2-ga"}]`,
		"https://api.github.com/repos/openjdk/jdk27u/commits/jdk-27.0.1.0.3-ga": `{"sha":"` + strings.Repeat("b", 40) + `","commit":{"committer":{"date":"2026-10-22T12:00:00Z"}}}`,
	})
	r, err := c.ResolveRelease(context.Background(), Policy{Source: "openjdk"})
	if err != nil || r.Version != "27.0.1.0.3" || r.Repository != "openjdk/jdk27u" || r.Commit != strings.Repeat("b", 40) || r.Timestamp == 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestReleaseResolutionRequiresStableUnrestrictedPolicyAndImmutableCommit(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/releases/latest": `{"tag_name":"v2.0.0"}`,
		"https://api.github.com/repos/a/b/commits/v2.0.0":  `{"sha":"main"}`,
	})
	for _, p := range []Policy{
		{Source: "github", Repo: "a/b", Prefix: "v"},
		{Source: "github-tags", Repo: "a/b", Series: "1"},
		{Source: "git", Repo: "a/b", Branch: "main"},
		{Source: "github", Repo: "a/b", VersionPattern: "[", VersionReplace: "x"},
	} {
		if _, err := c.ResolveRelease(context.Background(), p); err == nil {
			t.Fatal("invalid release policy or commit accepted", p)
		}
	}
}
