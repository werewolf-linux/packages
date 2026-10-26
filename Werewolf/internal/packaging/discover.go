package packaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiscoveryResult drives isolated package builds and recoverable publications.
type DiscoveryResult struct {
	Build   []string `json:"build"`
	Publish []string `json:"publish"`
	Failed  []string `json:"failed"`
}

// Discover checks every selected package independently using one Wolfi checkout.
// Local mode neither reads nor changes GitHub state.
func Discover(ctx context.Context, packages []Package, checkout string, github bool) (DiscoveryResult, error) {
	result := DiscoveryResult{Build: []string{}, Publish: []string{}, Failed: []string{}}
	self, err := os.Executable()
	if err != nil {
		return result, err
	}
	for _, pkg := range packages {
		if pkg.Engine != "wolfi" {
			continue
		}
		if err := os.MkdirAll(pkg.Build, 0755); err != nil {
			return result, err
		}
		if err := os.RemoveAll(filepath.Join(pkg.Build, "source")); err != nil {
			return result, err
		}
		failed := false
		for _, step := range []struct{ command, kind string }{{"refresh", "refresh"}, {"derive", "derivation"}, {"prepare", "patches"}} {
			args := []string{step.command, "--root", pkg.Root, "--package", pkg.Name}
			if step.command == "refresh" {
				args = append(args, "--wolfi", checkout)
			}
			cmd := exec.CommandContext(ctx, self, args...)
			var log bytes.Buffer
			cmd.Stdout = io.MultiWriter(os.Stderr, &log)
			cmd.Stderr = cmd.Stdout
			runErr := cmd.Run()
			if step.command == "refresh" && runErr == nil {
				runErr = selectBuild(&pkg)
			}
			if err := writeFile(filepath.Join(pkg.Build, step.kind+".log"), log.Bytes(), 0644); err != nil {
				return result, err
			}
			if runErr == nil {
				continue
			}
			slog.Error("discovery step failed", "package", pkg.Name, "step", step.command, "error", runErr)
			if github {
				if err := driftIssue(ctx, pkg, step.kind, log.String()); err != nil {
					slog.Error("cannot report package drift", "package", pkg.Name, "error", err)
				}
			}
			result.Failed = append(result.Failed, pkg.Name)
			failed = true
			break
		}
		if failed {
			continue
		}
		if github {
			if err := resolveDrift(ctx, pkg); err != nil {
				slog.Warn("cannot close resolved drift", "package", pkg.Name, "error", err)
			}
		}
		if err := Archive(filepath.Join(pkg.Build, "inputs"), filepath.Join(pkg.Root, "Werewolf/.work/bundles", pkg.Name+".tar.gz")); err != nil {
			return result, err
		}
		state := "build"
		if github {
			lock, err := VerifyInputs(pkg)
			if err != nil {
				return result, err
			}
			state, _, err = ReleaseStatus(ctx, pkg.Name+"/build-"+lock.BuildID)
			if err != nil {
				slog.Error("cannot read release status", "package", pkg.Name, "error", err)
				result.Failed = append(result.Failed, pkg.Name)
				continue
			}
		}
		if state == "build" {
			result.Build = append(result.Build, pkg.Name)
		}
		if state != "complete" {
			result.Publish = append(result.Publish, pkg.Name)
		}
		slog.Info("discovery completed", "package", pkg.Name, "state", state)
	}
	if err := ActionsOutput(map[string]any{"build": result.Build, "publish": result.Publish, "failed": result.Failed}); err != nil {
		return result, err
	}
	if !github && len(result.Failed) != 0 {
		return result, fmt.Errorf("package discovery failed: %v", result.Failed)
	}
	return result, nil
}

type drift struct {
	Number int64
	Title  string
	Labels []struct{ Name string }
}

func driftIssues(ctx context.Context, pkg Package) ([]drift, error) {
	for _, label := range []struct{ name, color, description string }{{"drift", "B60205", "A release no longer builds with these protections"}, {"package/" + pkg.Name, "1D76DB", pkg.Path}} {
		if _, err := output(ctx, pkg.Root, "gh", "label", "create", label.name, "--color", label.color, "--force", "--description", label.description); err != nil {
			return nil, err
		}
	}
	text, err := output(ctx, pkg.Root, "gh", "issue", "list", "--label", "drift", "--label", "package/"+pkg.Name, "--state", "open", "--json", "number,title,labels", "--limit", "200")
	if err != nil {
		return nil, err
	}
	var issues []drift
	if err := json.Unmarshal([]byte(text), &issues); err != nil {
		return nil, err
	}
	return issues, nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func driftIssue(ctx context.Context, pkg Package, kind, log string) error {
	lines := strings.Split(strings.ReplaceAll(ansi.ReplaceAllString(log, ""), "```", "'''"), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	quoted := []rune(strings.Join(lines, "\n"))
	if len(quoted) > 50000 {
		quoted = quoted[len(quoted)-50000:]
	}
	version := pkg.Spec.Title + " (latest Wolfi)"
	data, err := os.ReadFile(filepath.Join(pkg.Build, "inputs/upstream.lock.json"))
	var lock Inputs
	facts := "- Wolfi recipe was not captured.\n"
	if kind != "refresh" && err == nil && json.Unmarshal(data, &lock) == nil {
		version = fmt.Sprintf("%s %s-r%d", pkg.Spec.Title, lock.Version, lock.Epoch)
		facts = fmt.Sprintf("- Wolfi recipe: `%s` at `%s`\n- Effective source: `%s`\n", lock.Recipe, lock.WolfiCommit, lock.SourceCommit)
	}
	reason := map[string]string{"refresh": "capturing the recipe stopped", "derivation": "the derivation stopped", "patches": "a patch no longer applies"}[kind]
	if reason == "" {
		return fmt.Errorf("unknown drift kind")
	}
	title := version + ": " + reason
	body := version + ": " + reason + ".\n\n" + facts
	if run := os.Getenv("GITHUB_RUN_ID"); run != "" {
		body += "- Run: " + env("GITHUB_SERVER_URL", "https://github.com") + "/" + os.Getenv("GITHUB_REPOSITORY") + "/actions/runs/" + run + "\n"
	}
	body += "\n```\n" + string(quoted) + "\n```\n"
	sum := sha256.Sum256([]byte(title + "\n" + string(quoted)))
	marker := "<!-- drift " + kind + ":" + hex.EncodeToString(sum[:])[:16] + " -->"
	issues, err := driftIssues(ctx, pkg)
	if err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "packages-drift-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "body.md")
	if err := writeFile(path, []byte(body+"\n"+marker+"\n"), 0600); err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.Title != title {
			continue
		}
		text, err := output(ctx, pkg.Root, "gh", "issue", "view", strconv.FormatInt(issue.Number, 10), "--json", "body,comments")
		if err != nil {
			return err
		}
		var previous struct {
			Body     string
			Comments []struct{ Body string }
		}
		if err := json.Unmarshal([]byte(text), &previous); err != nil {
			return err
		}
		if strings.Contains(previous.Body, marker) {
			return nil
		}
		for _, comment := range previous.Comments {
			if strings.Contains(comment.Body, marker) {
				return nil
			}
		}
		_, err = output(ctx, pkg.Root, "gh", "issue", "comment", strconv.FormatInt(issue.Number, 10), "--body-file", path)
		return err
	}
	args := []string{"issue", "create", "--title", title, "--body-file", path, "--label", "drift", "--label", "package/" + pkg.Name}
	_, err = output(ctx, pkg.Root, "gh", args...)
	return err
}

func resolveDrift(ctx context.Context, pkg Package) error {
	issues, err := driftIssues(ctx, pkg)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		isFuzz := false
		for _, label := range issue.Labels {
			isFuzz = isFuzz || label.Name == "fuzz"
		}
		message := "Package capture, derivation and patch preparation succeeded."
		if isFuzz {
			message = "Package discovery succeeded. Patch fuzz remains in the logs; issues are reserved for failed package operations."
		}
		if _, err := output(ctx, pkg.Root, "gh", "issue", "close", strconv.FormatInt(issue.Number, 10), "--comment", message); err != nil {
			return err
		}
	}
	return nil
}
