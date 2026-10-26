package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
)

func TestDeclaredChecksUsePackageDirectoriesAndRetainOtherResults(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "tests"), 0755); err != nil {
		t.Fatal(err)
	}
	pkg := Package{Directory: directory, Package: catalog.Package{Name: "example", Config: &catalog.Definition{Checks: []catalog.Check{
		{Name: "failing check", Runs: "exit 3"},
		{Name: "package-local check", WorkingDirectory: "tests", Runs: "printf success > result"},
	}}}}
	if err := Checks(t.Context(), []Package{pkg}); err == nil || !strings.Contains(err.Error(), "failing check") {
		t.Fatalf("check result: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "tests/result"))
	if err != nil || string(data) != "success" {
		t.Fatalf("other check did not finish: %s %v", data, err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err != nil {
		t.Fatal(err)
	}
	pkg.Config.Checks = []catalog.Check{{WorkingDirectory: "escape", Runs: "touch unexpected"}}
	if err := Checks(t.Context(), []Package{pkg}); err == nil {
		t.Fatal("allowed a check to escape its context")
	}
	if _, err := os.Stat(filepath.Join(outside, "unexpected")); !os.IsNotExist(err) {
		t.Fatal("executed the escaped check")
	}
}
