package update

import (
	"context"
	"testing"
)

func TestMkshLetterPatchReleases(t *testing.T) {
	policy := Policy{Source: "github-tags", Repo: "MirBSD/mksh", Prefix: "mksh-R", VersionPattern: `^[0-9]+[a-z]?$`, VersionReplace: "$0"}
	client := mockClient(t, nil)
	client.Tags = func(_ context.Context, address string) (map[string]string, error) {
		if address != "https://github.com/MirBSD/mksh" {
			t.Fatal("tags read from", address)
		}
		return map[string]string{
			"mksh-R59": commitA, "mksh-R59c": commitB, "mksh-R59b": commitA, "mksh-R60-rc1": commitB,
		}, nil
	}
	release, err := client.Discover(context.Background(), policy, "59b")
	if err != nil || release.Version != "59c" || release.Tag != "mksh-R59c" || release.Commit != commitB {
		t.Fatalf("%+v %v", release, err)
	}
	for _, pair := range [][2]string{{"59c", "59b"}, {"59a", "59"}, {"60", "59z"}, {"10", "9z"}, {"59.1", "59z"}} {
		if Compare(pair[0], pair[1]) <= 0 || Compare(pair[1], pair[0]) >= 0 {
			t.Fatal("wrong letter patch order", pair)
		}
	}
	if _, ok := candidate("v1.0b", Policy{Prefix: "v"}); ok {
		t.Fatal("letter suffix accepted without explicit tag-format policy")
	}
	for _, tag := range []string{"mksh-R60-rc1", "mksh-R60beta", "mksh-R60.1"} {
		if _, ok := candidate(tag, policy); ok {
			t.Fatal("non-release accepted", tag)
		}
	}
}
