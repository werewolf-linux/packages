package update

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// ResolveRelease returns a stable version and the exact commit it names. Wolfi
// derivations use this without writing an application recipe or running tools.
func (c *Client) ResolveRelease(ctx context.Context, p Policy) (Release, error) {
	if p.Source != "github" && p.Source != "github-tags" && p.Source != "openjdk" {
		return Release{}, fmt.Errorf("source %q cannot pin a release commit", p.Source)
	}
	if p.Source != "openjdk" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(p.Repo) {
		return Release{}, fmt.Errorf("invalid release repository")
	}
	if p.Series != "" || p.Hold != "" || p.Branch != "" || p.Lock != "" || len(p.Components) != 0 {
		return Release{}, fmt.Errorf("upstream release discovery must follow the latest stable version")
	}
	if (p.VersionPattern == "") != (p.VersionReplace == "") {
		return Release{}, fmt.Errorf("version pattern and replacement must be paired")
	}
	if p.VersionPattern != "" {
		if _, err := regexp.Compile(p.VersionPattern); err != nil {
			return Release{}, err
		}
	}
	r, err := c.Discover(ctx, p, "0")
	if err != nil {
		return Release{}, err
	}
	if r.Repository == "" {
		r.Repository = p.Repo
	}
	var commit struct {
		SHA    string
		Commit struct{ Committer struct{ Date time.Time } }
	}
	if err := c.json(ctx, "https://api.github.com/repos/"+r.Repository+"/commits/"+url.PathEscape(r.Tag), &commit); err != nil {
		return Release{}, err
	}
	if !commitID.MatchString(commit.SHA) || commit.Commit.Committer.Date.IsZero() {
		return Release{}, fmt.Errorf("invalid release commit")
	}
	r.Commit, r.Timestamp = commit.SHA, commit.Commit.Committer.Date.Unix()
	return r, nil
}
