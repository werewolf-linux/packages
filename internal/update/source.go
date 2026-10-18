// Package update discovers stable upstream releases and prepares pinned recipes.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Policy struct {
	Source  string `json:"source"`
	Repo    string `json:"repo,omitempty"`
	Project string `json:"project,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Lock    string `json:"lock,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type Release struct {
	Version string `json:"version"`
	Tag     string `json:"tag,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Build   string `json:"build,omitempty"`
	URL     string `json:"url,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

// Client only sends the GitHub token to api.github.com. Downloads, redirects,
// dependency resolvers and build scripts never receive it.
type Client struct {
	HTTP  *http.Client
	Token string
}

func NewClient(token string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS redirect")
		}
		if req.URL.Host != "api.github.com" {
			req.Header.Del("Authorization")
		}
		return nil
	}}, Token: token}
}

func (c *Client) get(ctx context.Context, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("invalid HTTPS source: %s", address)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "werewolf-package-update")
	if u.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", address, resp.Status)
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, address string, value any) error {
	r, err := c.get(ctx, address)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("oversized metadata: %s", address)
	}
	return json.Unmarshal(data, value)
}

// Download streams archives, including large model/server payloads.
func (c *Client) Download(ctx context.Context, address string, out io.Writer) (string, error) {
	r, err := c.get(ctx, address)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), r.Body)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var stable = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*(?:\+[0-9]+)?$`)
var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Compare compares numeric release components, so 1.10 is newer than 1.9.
// Missing patch components compare as zero. Prereleases never reach this path.
func Compare(a, b string) int {
	parts := func(v string) []string {
		return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '+' })
	}
	aa, bb := parts(a), parts(b)
	for i := 0; i < len(aa) || i < len(bb); i++ {
		x, y := "0", "0"
		if i < len(aa) {
			x = strings.TrimLeft(aa[i], "0")
			if x == "" {
				x = "0"
			}
		}
		if i < len(bb) {
			y = strings.TrimLeft(bb[i], "0")
			if y == "" {
				y = "0"
			}
		}
		if len(x) != len(y) {
			if len(x) > len(y) {
				return 1
			}
			return -1
		}
		if x != y {
			return strings.Compare(x, y)
		}
	}
	return 0
}

func candidate(tag string, p Policy) (Release, bool) {
	if !strings.HasPrefix(tag, p.Prefix) {
		return Release{}, false
	}
	v := strings.TrimPrefix(tag, p.Prefix)
	r := Release{Version: v, Tag: tag}
	if p.Pattern != "" {
		m := regexp.MustCompile(p.Pattern).FindStringSubmatch(v)
		if len(m) != 3 {
			return Release{}, false
		}
		r.Version, r.Build = m[1], m[2]
	}
	return r, stable.MatchString(v)
}

func (c *Client) Discover(ctx context.Context, p Policy, current string) (Release, error) {
	switch p.Source {
	case "github":
		var item struct {
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
		}
		if err := c.json(ctx, "https://api.github.com/repos/"+p.Repo+"/releases/latest", &item); err != nil {
			return Release{}, err
		}
		r, ok := candidate(item.Tag, p)
		if !ok || item.Draft || item.Prerelease {
			return Release{}, fmt.Errorf("upstream latest release is not stable: %s", item.Tag)
		}
		return r, nil
	case "github-tags":
		var best Release
		endpoint := "releases"
		if p.Source == "github-tags" {
			endpoint = "tags"
		}
		// Pagination is mandatory: tag order is not version order, and old
		// maintenance releases may be published after a newer major release.
		for page := 1; ; page++ {
			var list []struct {
				Name       string `json:"name"`
				Tag        string `json:"tag_name"`
				Draft      bool   `json:"draft"`
				Prerelease bool   `json:"prerelease"`
			}
			if err := c.json(ctx, fmt.Sprintf("https://api.github.com/repos/%s/%s?per_page=100&page=%d", p.Repo, endpoint, page), &list); err != nil {
				return Release{}, err
			}
			for _, item := range list {
				if item.Draft || item.Prerelease {
					continue
				}
				tag := item.Tag
				if endpoint == "tags" {
					tag = item.Name
				}
				r, ok := candidate(tag, p)
				if ok && (best.Version == "" || Compare(r.Version+"+"+r.Build, best.Version+"+"+best.Build) > 0) {
					best = r
				}
			}
			if len(list) < 100 {
				break
			}
			if page == 100 {
				return Release{}, fmt.Errorf("too many upstream releases; narrow the policy")
			}
		}
		if best.Version == "" {
			return Release{}, fmt.Errorf("no stable release found for %s", p.Repo)
		}
		return best, nil
	case "pypi":
		var data struct {
			Info struct {
				Version string `json:"version"`
			}
			Releases map[string][]struct {
				Yanked bool `json:"yanked"`
			}
		}
		if err := c.json(ctx, "https://pypi.org/pypi/"+p.Project+"/json", &data); err != nil {
			return Release{}, err
		}
		var best string
		for v, files := range data.Releases {
			if !stable.MatchString(v) {
				continue
			}
			for _, f := range files {
				if !f.Yanked && (best == "" || Compare(v, best) > 0) {
					best = v
				}
			}
		}
		if best == "" {
			return Release{}, fmt.Errorf("no non-yanked stable PyPI release")
		}
		return Release{Version: best}, nil
	case "npm":
		var tags map[string]string
		if err := c.json(ctx, "https://registry.npmjs.org/-/package/"+p.Project+"/dist-tags", &tags); err != nil {
			return Release{}, err
		}
		if !stable.MatchString(tags["latest"]) {
			return Release{}, fmt.Errorf("npm latest is not stable")
		}
		return Release{Version: tags["latest"]}, nil
	case "minecraft":
		var manifest struct {
			Latest   struct{ Release string }
			Versions []struct{ ID, Type, URL string }
		}
		if err := c.json(ctx, "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
			return Release{}, err
		}
		for _, v := range manifest.Versions {
			if v.ID != manifest.Latest.Release || v.Type != "release" {
				continue
			}
			var version struct {
				ID, Type  string
				Downloads struct{ Server struct{ URL string } }
			}
			if err := c.json(ctx, v.URL, &version); err != nil {
				return Release{}, err
			}
			if version.ID != v.ID || version.Type != "release" || version.Downloads.Server.URL == "" {
				return Release{}, fmt.Errorf("invalid Minecraft server manifest")
			}
			return Release{Version: v.ID, URL: version.Downloads.Server.URL}, nil
		}
		return Release{}, fmt.Errorf("Minecraft stable release missing from manifest")
	case "git":
		var data struct {
			SHA    string
			Commit struct{ Committer struct{ Date time.Time } }
		}
		if err := c.json(ctx, "https://api.github.com/repos/"+p.Repo+"/commits/"+url.PathEscape(p.Branch), &data); err != nil {
			return Release{}, err
		}
		if !commitID.MatchString(data.SHA) || data.Commit.Committer.Date.IsZero() {
			return Release{}, fmt.Errorf("invalid branch commit")
		}
		base := strings.Split(current, "_git")[0]
		return Release{Version: base + "_git" + data.Commit.Committer.Date.UTC().Format("20060102"), Commit: data.SHA}, nil
	case "content":
		return Release{Version: current}, nil
	default:
		return Release{}, fmt.Errorf("unknown source %q", p.Source)
	}
}

func (c *Client) commit(ctx context.Context, repo, tag string) (string, error) {
	// The commits API dereferences lightweight and annotated tags to a commit.
	var result struct{ SHA string }
	if err := c.json(ctx, "https://api.github.com/repos/"+repo+"/commits/"+url.PathEscape(tag), &result); err != nil {
		return "", err
	}
	if !commitID.MatchString(result.SHA) {
		return "", fmt.Errorf("invalid commit for %s", tag)
	}
	return result.SHA, nil
}

func numericEpoch(value string) (string, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == ^uint64(0) {
		return "", fmt.Errorf("invalid epoch %q", value)
	}
	return strconv.FormatUint(n+1, 10), nil
}
