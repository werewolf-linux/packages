package packaging

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageSuitesKeepRunningAndReportEveryFailure(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "package")
	script := `#!/bin/sh
set -eu
test -z "${GH_TOKEN:-}"
test -z "${AWS_SECRET_ACCESS_KEY:-}"
stage=$1
shift
name=
while [ "$#" -gt 0 ]; do
    case $1 in --package) name=$2; shift 2;; *) shift;; esac
done
echo "$stage $name"
test "$name" != broken
`
	if err := os.WriteFile(tool, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_TOKEN", "fixture-token")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-secret")
	var packages []Package
	for _, name := range []string{"first", "broken", "last"} {
		var pkg Package
		pkg.Name, pkg.Root, pkg.Build = name, root, filepath.Join(root, name, "input-id")
		packages = append(packages, pkg)
	}
	report := filepath.Join(root, "report.json")
	results, err := testPackages(t.Context(), packages, TestOptions{Arch: "aarch64", Report: report}, tool)
	if err == nil || len(results) != 3 || results[0].Status != "passed" || results[1].Status != "failed" || results[2].Status != "passed" {
		t.Fatalf("results=%+v error=%v", results, err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var saved []TestResult
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) != 3 || saved[1].Error == "" {
		t.Fatalf("saved report=%s error=%v", data, err)
	}
	for _, result := range results {
		log, err := os.ReadFile(result.Log)
		if err != nil || !strings.Contains(string(log), "test "+result.Package) {
			t.Fatalf("missing package log: %+v: %v", result, err)
		}
	}
}

func TestPackageSuitesDoNotStartAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var pkg Package
	pkg.Name, pkg.Root, pkg.Build = "pending", t.TempDir(), t.TempDir()
	results, err := testPackages(ctx, []Package{pkg}, TestOptions{Arch: "aarch64"}, "must-not-run")
	if err == nil || len(results) != 1 || results[0].Status != "pending" {
		t.Fatalf("cancelled batch: %+v: %v", results, err)
	}
}

func TestBuildScratchIsRemovedOnlyOnSuccess(t *testing.T) {
	for _, success := range []bool{true, false} {
		directory := t.TempDir()
		script := `mkdir "$TMPDIR/guest"; exit 1`
		if success {
			script = `mkdir "$TMPDIR/guest"`
		}
		err := runBuild(t.Context(), directory, "/bin/sh", []string{"TMPDIR=" + directory}, []string{"-ec", script})
		if (err == nil) != success {
			t.Fatalf("success=%v: %v", success, err)
		}
		roots, err := filepath.Glob(filepath.Join(directory, "run-*", "guest"))
		if err != nil || (len(roots) == 0) != success {
			t.Fatalf("success=%v, retained roots=%v: %v", success, roots, err)
		}
	}
}
