package update

import "testing"

const (
	tagObject = "1111111111111111111111111111111111111111"
	commitA   = "2222222222222222222222222222222222222222"
	commitB   = "3333333333333333333333333333333333333333"
)

func TestParseTagsPrefersPeeledCommits(t *testing.T) {
	out := commitA + "\tHEAD\n" +
		commitB + "\trefs/tags/v1.2.78^{}\n" + // peeled line before its tag object
		tagObject + "\trefs/tags/v1.2.78\n" +
		commitA + "\trefs/tags/v1.2.77\n" + // lightweight
		commitA + "\trefs/heads/master\n"
	tags, err := parseTags(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags["v1.2.78"] != commitB || tags["v1.2.77"] != commitA {
		t.Fatalf("tags = %v", tags)
	}
	for _, bad := range []string{"not-a-sha\trefs/tags/v1\n", commitA + " refs/tags/v1\n"} {
		if _, err := parseTags(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNewestTagFollowsPolicy(t *testing.T) {
	p := Policy{Source: "git-tags", URL: "https://git.kernel.org/pub/scm/libs/libcap/libcap.git", Prefix: "v1."}
	tags := map[string]string{
		"v1.2.9":     commitA,
		"v1.2.78":    commitB,
		"v1.2.79-rc": tagObject, // not stable
		"libcap-2.9": tagObject, // other prefix
	}
	r, err := newestTag(tags, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "2.78" || r.Tag != "v1.2.78" || r.Commit != commitB || r.Repository != p.URL {
		t.Fatalf("release = %+v", r)
	}
	if _, err := newestTag(map[string]string{"v2.0-beta": commitA}, p); err == nil {
		t.Fatal("found a release among unstable tags")
	}
}
