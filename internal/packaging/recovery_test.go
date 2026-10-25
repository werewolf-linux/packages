package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/internal/overlay"
)

func TestVerifyRecoveryTag(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg := Package{Root: root, Directory: filepath.Join(root, "devel/libcap"), Build: t.TempDir()}
	pkg.Name, pkg.Path = "libcap", "devel/libcap"
	directory := filepath.Join(pkg.Build, "inputs")
	if err := writeFile(filepath.Join(directory, "upstream.yaml"), []byte("fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat("a", 40)
	lock := Inputs{Package: pkg.Name, WolfiCommit: source, Lock: overlay.Lock{SourceCommit: source}}
	lock.SHA256, lock.Mode, err = hashFiles(directory, func(string) bool { return false })
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
	if err := writeJSON(filepath.Join(directory, "upstream.lock.json"), lock); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	script := `#!/bin/sh
test "$1" = api || exit 2
test "$2" = "repos/test/runtime/git/ref/tags/libcap/build-$BUILD_ID" || exit 2
printf '%s\n' "$GH_RESPONSE"
exit "$GH_EXIT"
`
	if err := os.WriteFile(filepath.Join(tools, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITHUB_REPOSITORY", "test/runtime")
	t.Setenv("BUILD_ID", lock.BuildID)
	for _, tc := range []struct {
		name, response, exit string
		accepted             bool
	}{
		{"absent", "gh: Not Found (HTTP 404)", "1", true},
		{"matching", `{"object":{"type":"commit","sha":"` + source + `"}}`, "0", true},
		{"wrong commit", `{"object":{"type":"commit","sha":"` + strings.Repeat("b", 40) + `"}}`, "0", false},
		{"annotated tag", `{"object":{"type":"tag","sha":"` + source + `"}}`, "0", false},
		{"authentication failure", "gh: Forbidden (HTTP 403)", "1", false},
		{"service failure", "gh: Internal Server Error (HTTP 500)", "1", false},
		{"unknown failure", "", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GH_RESPONSE", tc.response)
			t.Setenv("GH_EXIT", tc.exit)
			err := VerifyTag(t.Context(), pkg, source)
			if (err == nil) != tc.accepted {
				t.Fatalf("VerifyTag = %v; accepted = %t", err, tc.accepted)
			}
		})
	}
}
