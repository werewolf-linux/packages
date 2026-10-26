package packaging

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type CIJob struct {
	Package string `json:"package"`
	Engine  string `json:"engine"`
	Arch    string `json:"arch"`
	InputID string `json:"definition_id"`
	Reason  string `json:"reason"`
}

type CIResult struct {
	Include  []CIJob  `json:"include"`
	Packages []string `json:"packages"`
	Wolfi    []string `json:"wolfi"`
}

// PlanCI selects both engines using the catalog, never a workflow package list.
// Shared code conservatively selects every package until dependency-aware test
// selection can prove a narrower set. Files are NUL-delimited throughout.
func PlanCI(ctx context.Context, packages []Package, base, head string) (CIResult, error) {
	result := CIResult{Include: []CIJob{}, Packages: []string{}, Wolfi: []string{}}
	if len(packages) == 0 {
		return result, nil
	}
	reasons := map[string]string{}
	if base == "" {
		for _, p := range packages {
			reasons[p.Name] = "explicit or complete validation"
		}
	} else {
		root := packages[0].Root
		if head != "" {
			expected, err := output(ctx, root, "git", "rev-parse", "--verify", head+"^{commit}")
			if err != nil {
				return result, err
			}
			actual, err := output(ctx, root, "git", "rev-parse", "HEAD")
			if err != nil {
				return result, err
			}
			if actual != expected {
				return result, fmt.Errorf("check out the selected --head before planning its definitions")
			}
		}
		args := []string{"diff", "--name-only", "--no-renames", "-z", base}
		if head != "" {
			args = append(args, head)
		}
		args = append(args, "--")
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		data, err := cmd.Output()
		if err != nil {
			return result, fmt.Errorf("changed inputs: %w", err)
		}
		files := strings.Split(string(data), "\x00")
		if head == "" {
			cmd = exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard", "-z")
			cmd.Dir = root
			data, err = cmd.Output()
			if err != nil {
				return result, err
			}
			files = append(files, strings.Split(string(data), "\x00")...)
		}
		filtered := make([]string, 0, len(files))
		for _, file := range files {
			if strings.HasSuffix(file, "/package.yaml") {
				beforeCmd := exec.CommandContext(ctx, "git", "show", base+":"+file)
				beforeCmd.Dir = root
				if before, err := beforeCmd.Output(); err == nil {
					after, readErr := os.ReadFile(filepath.Join(root, file))
					left, leftErr := definitionDataDigest(before, true)
					right, rightErr := definitionDataDigest(after, true)
					if readErr == nil && leftErr == nil && rightErr == nil && left == right {
						continue
					}
				}
			}
			filtered = append(filtered, file)
		}
		reasons = selectedPaths(packages, filtered)
	}
	for _, p := range packages {
		reason, ok := reasons[p.Name]
		if !ok {
			continue
		}
		if p.Update.Hold != "" { // Training packages still validate when changed explicitly.
			reason += "; release discovery held"
		}
		id, err := InputID(p)
		if err != nil {
			return result, err
		}
		if p.Engine == "wolfi" {
			result.Wolfi = append(result.Wolfi, p.Name)
		}
		result.Packages = append(result.Packages, p.Name)
		for _, arch := range Arches {
			result.Include = append(result.Include, CIJob{p.Name, p.Engine, arch, id, reason})
		}
	}
	sort.Strings(result.Packages)
	sort.Strings(result.Wolfi)
	return result, nil
}

// CheckFixtures checks Wolfi policy against the reviewed offline baseline. Updating
// the baseline is a separate discovery operation, so PR validation is repeatable.
func CheckFixtures(pkg Package) error {
	fixtures, err := filepath.Glob(filepath.Join(pkg.Directory, "tests/fixtures/*.yaml"))
	if err != nil {
		return err
	}
	if len(fixtures) == 0 {
		return fmt.Errorf("%s: missing reviewed Wolfi fixture", pkg.Name)
	}
	for _, path := range fixtures {
		// Multiple snapshots may represent released streams. Every retained baseline
		// must either derive successfully or be an explicitly older informational file.
		if _, err := Fixture(pkg, path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func SaveReport(path string, value any) error {
	if path == "" {
		return nil
	}
	return writeJSON(path, value)
}

func selectedPaths(packages []Package, files []string) map[string]string {
	reasons := map[string]string{}
	for _, file := range files {
		if file == "" || strings.HasSuffix(file, ".md") || strings.HasPrefix(file, "docs/") || strings.HasPrefix(file, "LICENSES/") || file == "LICENSE" {
			continue
		}
		owner := ""
		for _, p := range packages {
			if strings.HasPrefix(file, filepath.ToSlash(p.Path)+"/") {
				owner = p.Name
				break
			}
		}
		if owner != "" {
			reasons[owner] = "package input changed: " + file
			continue
		}
		if strings.HasPrefix(file, "build/") {
			continue
		}
		for _, p := range packages {
			reasons[p.Name] = "shared input changed: " + file
		}
	}
	return reasons
}
