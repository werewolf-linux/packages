package packaging

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

type sourceRun struct {
	ID         int64 `json:"id"`
	RunNumber  int64 `json:"run_number"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	Path       string `json:"path"`
}
type buildJob struct {
	Name       string
	Conclusion string
}

func checkedRun(run, repository, pkg string, record sourceRun, jobs []buildJob) (string, int64, error) {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(run) || strconv.FormatInt(record.ID, 10) != run || record.Repository.FullName != repository || record.HeadRepository.FullName != repository || record.HeadBranch != "main" || record.Path != ".github/workflows/wolfi-refresh.yml" || !exactCommit.MatchString(record.HeadSHA) {
		return "", 0, fmt.Errorf("source run is not this repository's runtime publication workflow")
	}
	if record.Event != "push" && record.Event != "schedule" && record.Event != "workflow_dispatch" {
		return "", 0, fmt.Errorf("source workflow event is not eligible for publication")
	}
	pattern := regexp.MustCompile(`^build \(` + regexp.QuoteMeta(pkg) + `\) / build \((x86_64|aarch64), [^)]+\)$`)
	seen := map[string]bool{}
	for _, job := range jobs {
		match := pattern.FindStringSubmatch(job.Name)
		if match == nil {
			continue
		}
		if job.Conclusion != "success" || seen[match[1]] {
			return "", 0, fmt.Errorf("both native builds must have passed exactly once")
		}
		seen[match[1]] = true
	}
	if len(seen) != len(Arches) || record.RunNumber < 1 {
		return "", 0, fmt.Errorf("both successful native builds are required")
	}
	return record.HeadSHA, record.RunNumber, nil
}

// Recover validates the original workflow and its first-attempt native build
// jobs before a repaired publisher may use the historical build artifacts.
func Recover(ctx context.Context, pkg Package, run string) error {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(run) {
		return fmt.Errorf("source workflow run must be a positive integer")
	}
	endpoint, err := githubRepository()
	if err != nil {
		return err
	}
	var record sourceRun
	if err := githubJSON(ctx, &record, endpoint+"/actions/runs/"+run); err != nil {
		return err
	}
	var pages []struct{ Jobs []buildJob }
	if err := githubJSON(ctx, &pages, "--paginate", "--slurp", endpoint+"/actions/runs/"+run+"/attempts/1/jobs?per_page=100"); err != nil {
		return err
	}
	var jobs []buildJob
	for _, page := range pages {
		jobs = append(jobs, page.Jobs...)
	}
	sha, number, err := checkedRun(run, record.Repository.FullName, pkg.Name, record, jobs)
	if err != nil {
		return err
	}
	// Bind both repositories to the caller's configured repository, not a value
	// copied from an untrusted workflow record.
	if endpoint != "repos/"+record.Repository.FullName {
		return fmt.Errorf("source run belongs to another repository")
	}
	return ActionsOutput(map[string]any{"source_sha": sha, "source_run_number": number})
}

// VerifyTag accepts an absent tag for first publication, but refuses an existing
// tag that names another source. Staging rechecks it before creating the draft.
func VerifyTag(ctx context.Context, pkg Package, source string) error {
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	_, err = tagTarget(ctx, pkg.Name+"/build-"+lock.BuildID, source)
	return err
}
