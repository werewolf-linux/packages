package packaging

import (
	"context"
	"github.com/werewolf-linux/packages/internal/overlay"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefinitionDigestDistinguishesBuildInputsFromExplanation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.yaml")
	original := "update: {github: {identifier: example/project}}\nmelange:\n  package: {name: example, version: '1.0', epoch: 1}\n  pipeline: [{runs: make}]\nchanges: [{what: Example, why: Reason, evidence: [package.yaml]}]\nactions: {build: {files: [{path: x, require-contains: old}]}}\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	baseline, err := definitionDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"# a comment\n" + original, strings.Replace(original, "Reason", "Clearer documentation", 1)} {
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := definitionDigest(path)
		if err != nil || got != baseline {
			t.Fatalf("prose changed build identity: %s %v", got, err)
		}
	}
	for _, text := range []string{strings.Replace(original, "runs: make", "runs: ninja", 1), strings.Replace(original, "require-contains: old", "require-contains: reviewed", 1)} {
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := definitionDigest(path)
		if err != nil || got == baseline {
			t.Fatalf("build change was not bound: %s %v", got, err)
		}
	}
}

func TestNativeWorkspacesShareVerifiedInputsAndIsolateMutation(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	for _, entry := range entries {
		if entry.Name == "libcap" {
			pkg = entry
		}
	}
	pkg.Build = t.TempDir()
	inputs := filepath.Join(pkg.Build, "inputs")
	if err := writeFile(filepath.Join(inputs, "upstream.yaml"), []byte("fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lock := Inputs{Package: pkg.Name, WolfiCommit: strings.Repeat("a", 40), Lock: overlay.Lock{SourceCommit: strings.Repeat("b", 40), Revision: pkg.Policy.Revision}}
	lock.SHA256, lock.Mode, err = hashFiles(inputs, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	lock.PolicySHA256, err = policyHashes(pkg)
	if err != nil {
		t.Fatal(err)
	}
	lock.BuildID, err = identity(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(inputs, "upstream.lock.json"), lock); err != nil {
		t.Fatal(err)
	}
	x86, err := NativeWorkspace(pkg, "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	arm, err := NativeWorkspace(pkg, "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if x86.Build == arm.Build || x86.Build == pkg.Build {
		t.Fatal("architectures share a mutable workspace")
	}
	if err := os.WriteFile(filepath.Join(x86.Build, "inputs/upstream.yaml"), []byte("tampered\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyInputs(x86); err == nil {
		t.Fatal("accepted altered private inputs")
	}
	if _, err := VerifyInputs(arm); err != nil {
		t.Fatalf("other architecture changed: %v", err)
	}
	if _, err := VerifyInputs(pkg); err != nil {
		t.Fatalf("shared capture changed: %v", err)
	}
}

func TestCISelectsLocalChangesAndSharedChanges(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	var packages []Package
	for _, p := range entries {
		if p.Name == "caddy" || p.Name == "python" {
			packages = append(packages, p)
		}
	}
	changed := []string{"www/caddy/probe.sh"}
	reasons := selectedPaths(packages, changed)
	if len(reasons) != 1 || reasons["caddy"] == "" {
		t.Fatalf("local selection: %v", reasons)
	}
	if got := selectedPaths(packages, []string{"README.md"}); len(got) != 0 {
		t.Fatalf("docs selected builds: %v", got)
	}
	if got := selectedPaths(packages, []string{"internal/shared.go"}); len(got) != 2 {
		t.Fatalf("shared selection: %v", got)
	}
	empty, err := PlanCI(context.Background(), nil, "HEAD", "")
	if err != nil || len(empty.Include) != 0 {
		t.Fatalf("empty plan: %+v %v", empty, err)
	}
}
