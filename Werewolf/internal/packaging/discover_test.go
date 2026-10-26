package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

func discoveryGH(t *testing.T) (Package, string) {
	t.Helper()
	root := t.TempDir()
	log := filepath.Join(root, "calls")
	script := `#!/bin/sh
case "$1 $2" in
  'label create') test "$3" != fuzz || exit 9 ;;
  'issue list') printf '%s\n' "$DRIFT_ISSUES" ;;
  'issue close'|'issue create') printf '%s\n' "$@" >> "$DRIFT_CALLS" ;;
  *) exit 8 ;;
esac
`
	if err := writeFile(filepath.Join(root, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DRIFT_CALLS", log)
	t.Setenv("DRIFT_ISSUES", "[]")
	pkg := Package{Root: root, Build: filepath.Join(root, "build"), Spec: &overlay.Spec{Title: "Example"}}
	pkg.Name, pkg.Path = "example", "devel/example"
	return pkg, log
}

func TestDiscoveryReportsFailuresWithoutFuzzAdvisories(t *testing.T) {
	pkg, log := discoveryGH(t)
	if err := driftIssue(t.Context(), pkg, "fuzz", "patch applied with fuzz"); err == nil {
		t.Fatal("accepted fuzz as an issue kind")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("fuzz reporting contacted GitHub")
	}
	if err := driftIssue(t.Context(), pkg, "patches", "patch no longer applies"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "issue\ncreate\n") || strings.Contains(string(data), "\nfuzz\n") {
		t.Fatalf("expected only a failure issue: %s", data)
	}
}

func TestSuccessfulDiscoveryClosesFailureAndOldFuzzIssues(t *testing.T) {
	pkg, log := discoveryGH(t)
	t.Setenv("DRIFT_ISSUES", `[{"number":12,"labels":[{"name":"drift"}]},{"number":13,"labels":[{"name":"fuzz"}]}]`)
	if err := resolveDrift(t.Context(), pkg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"close\n12\n", "close\n13\n", "issues are reserved for failed package operations"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q: %s", want, data)
		}
	}
}
