package packaging

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/apk"
)

func TestLocalAPKReuseAcceptsSignedArtifactsAndChecksIdentityAndPayload(t *testing.T) {
	directory := t.TempDir()
	payload := testTarMember(t, "usr/bin/example", []byte("example payload"))
	sum := sha256.Sum256(payload)
	control := testTarMember(t, ".PKGINFO", []byte("pkgname = example\npkgver = 1-r0\narch = aarch64\ndatahash = "+hex.EncodeToString(sum[:])+"\n"))
	unsigned := filepath.Join(directory, "unsigned.apk")
	if err := os.WriteFile(unsigned, append(control, payload...), 0644); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signed := filepath.Join(directory, "signed.apk")
	if err := apk.SignPackage(unsigned, signed, "example", "1-r0", "aarch64", key); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{unsigned, signed} {
		if err := checkTestAPK(path, "example", "1-r0", "aarch64"); err != nil {
			t.Fatalf("valid local artifact %s: %v", path, err)
		}
		for _, identity := range [][3]string{{"other", "1-r0", "aarch64"}, {"example", "2-r0", "aarch64"}, {"example", "1-r0", "x86_64"}} {
			if err := checkTestAPK(path, identity[0], identity[1], identity[2]); err == nil {
				t.Fatalf("accepted mismatched identity %v for %s", identity, path)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data[len(data)-1] ^= 1
		corrupt := filepath.Join(directory, "corrupt.apk")
		if err := os.WriteFile(corrupt, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := checkTestAPK(corrupt, "example", "1-r0", "aarch64"); err == nil {
			t.Fatal("accepted corrupted local payload")
		}
	}
}

func TestPackageSuitesKeepRunningAndReportEveryFailure(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "package")
	script := `#!/bin/sh
set -eu
test -z "${GH_TOKEN:-}"
test -z "${AWS_SECRET_ACCESS_KEY:-}"
mkdir "$TMPDIR/guest"
stage=$1
shift
name=
repository=
while [ "$#" -gt 0 ]; do
    case $1 in
      --package) name=$2; shift 2;;
      --repository-append) repository=$2; shift 2;;
      *) shift;;
    esac
done
test "$repository" = "/repo with spaces"
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
	results, err := testPackages(t.Context(), packages, TestOptions{Arch: "aarch64", Report: report, Repositories: []string{"/repo with spaces"}}, tool)
	if err == nil || len(results) != 3 || results[0].Status != "passed" || results[1].Status != "failed" || results[2].Status != "passed" {
		t.Fatalf("results=%+v error=%v", results, err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var saved []TestResult
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) != 3 || saved[1].Error == "" || len(saved[1].Repositories) != 1 || saved[1].Repositories[0] != "/repo with spaces" {
		t.Fatalf("saved report=%s error=%v", data, err)
	}
	for _, result := range results {
		log, err := os.ReadFile(result.Log)
		if err != nil || !strings.Contains(string(log), "test "+result.Package) {
			t.Fatalf("missing package log: %+v: %v", result, err)
		}
		roots, err := filepath.Glob(filepath.Join(filepath.Dir(result.Log), ".test-*"))
		if err != nil || len(roots) != 0 {
			t.Fatalf("test scratch retained for %s: %v: %v", result.Package, roots, err)
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
		err := RunMelange(t.Context(), directory, "/bin/sh", []string{"TMPDIR=" + directory}, []string{"-ec", script})
		if (err == nil) != success {
			t.Fatalf("success=%v: %v", success, err)
		}
		roots, err := filepath.Glob(filepath.Join(directory, "run-*", "guest"))
		if err != nil || (len(roots) == 0) != success {
			t.Fatalf("success=%v, retained roots=%v: %v", success, roots, err)
		}
	}
}
