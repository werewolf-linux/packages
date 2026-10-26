package update

import (
	"context"
	"testing"
)

// MariaDB publishes several maintained lines at once, marks release
// candidates only as GitHub prereleases, and its newest release by date need
// not be its newest version: only github-releases sees all three.
func TestGitHubReleasesSkipPrereleasesAndCompareVersions(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/MariaDB/server/releases?per_page=100&page=1": `[
			{"tag_name":"mariadb-10.6.28","prerelease":false},
			{"tag_name":"mariadb-13.1.1","prerelease":true},
			{"tag_name":"mariadb-13.0.2","prerelease":false},
			{"tag_name":"mariadb-13.2.0","draft":true},
			{"tag_name":"mariadb-12.3.3","prerelease":false}]`,
	})
	r, err := c.Discover(context.Background(), Policy{Source: "github-releases", Repo: "MariaDB/server", Prefix: "mariadb-"}, "12.3.3")
	if err != nil || r.Version != "13.0.2" || r.Tag != "mariadb-13.0.2" {
		t.Fatalf("%+v %v", r, err)
	}
}
