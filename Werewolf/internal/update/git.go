package update

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Upstreams without a GitHub mirror, such as libcap on git.kernel.org, are
// read with git itself: their tags from the ref advertisement, and a
// release's commit time from a depth-one fetch of its tag.

var gitURL = regexp.MustCompile(`^https://[A-Za-z0-9./_-]+$`)
var gitTag = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// gitTags lists the tags of the repository at address and the commit each names.
func gitTags(ctx context.Context, address string) (map[string]string, error) {
	if !gitURL.MatchString(address) {
		return nil, fmt.Errorf("invalid git source %q", address)
	}
	out, err := git(ctx, "", "ls-remote", "--tags", "--", address)
	if err != nil {
		return nil, err
	}
	return parseTags(out)
}

// parseTags reads git ls-remote output. An annotated tag's peeled line names
// its commit and wins over the tag object, in whichever order they come.
func parseTags(out string) (map[string]string, error) {
	tags := map[string]string{}
	peeled := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		id, ref, ok := strings.Cut(line, "\t")
		if !ok || !commitID.MatchString(id) {
			return nil, fmt.Errorf("invalid ref advertisement line %q", line)
		}
		name, ok := strings.CutPrefix(ref, "refs/tags/")
		if !ok {
			continue
		}
		if base, ok := strings.CutSuffix(name, "^{}"); ok {
			tags[base], peeled[base] = id, true
		} else if !peeled[name] {
			tags[name] = id
		}
	}
	return tags, nil
}

// newestTag is the highest stable release the tags hold under p. Tags are
// visited in name order and only a strictly newer version replaces the
// choice, so two tags of one version always resolve the same way.
func newestTag(tags map[string]string, p Policy) (Release, error) {
	var best Release
	for _, tag := range slices.Sorted(maps.Keys(tags)) {
		r, ok := candidate(tag, p)
		if !ok || !gitTag.MatchString(tag) {
			continue
		}
		if best.Version == "" || Compare(r.Version+"+"+r.Build, best.Version+"+"+best.Build) > 0 {
			r.Commit = tags[tag]
			best = r
		}
	}
	if best.Version == "" {
		return Release{}, fmt.Errorf("no stable release found at %s", p.URL)
	}
	best.Repository = p.URL
	return best, nil
}

// gitCommitTime fetches tag alone, at depth one, into a scratch repository,
// checks it still names commit, and returns that commit's committer time.
func gitCommitTime(ctx context.Context, address, tag, commit string) (int64, error) {
	if !gitURL.MatchString(address) || !gitTag.MatchString(tag) || !commitID.MatchString(commit) {
		return 0, fmt.Errorf("invalid git release %s %s", address, tag)
	}
	dir, err := os.MkdirTemp("", "upstream-git-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	if _, err := git(ctx, dir, "init", "-q"); err != nil {
		return 0, err
	}
	if _, err := git(ctx, dir, "fetch", "-q", "--no-tags", "--depth=1", "--", address, "refs/tags/"+tag); err != nil {
		return 0, err
	}
	got, err := git(ctx, dir, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(got) != commit {
		return 0, fmt.Errorf("%s at %s names %s, not the listed %s", tag, address, strings.TrimSpace(got), commit)
	}
	out, err := git(ctx, dir, "log", "-1", "--format=%ct", commit)
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("invalid commit time for %s", tag)
	}
	return seconds, nil
}

// git runs git over HTTPS alone, without hooks, prompts or system
// configuration, and gives up on a transfer that stalls for a minute.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=60",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
