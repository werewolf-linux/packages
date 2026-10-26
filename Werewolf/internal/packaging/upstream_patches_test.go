package packaging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

func TestUpstreamPatchRefreshPreservesCaptureAndMatchesBuildContext(t *testing.T) {
	pkg := Package{Directory: t.TempDir(), Spec: &overlay.Spec{}}
	captured, context := t.TempDir(), t.TempDir()
	original := filepath.Join(captured, "fix.patch")
	if err := writeFile(original, []byte("captured context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	refreshed := filepath.Join(pkg.Directory, "upstream-patches/fix.patch")
	if err := writeFile(refreshed, []byte("refreshed context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := digest(original)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Spec.UpstreamPatches = map[string]overlay.UpstreamPatch{
		"fix.patch": {SHA256: sum, Source: "upstream-patches/fix.patch"},
	}
	prepared, err := upstreamPatch(pkg, captured, "fix.patch")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyUpstreamPatches(pkg, captured, context); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{prepared, filepath.Join(context, "fix.patch")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "refreshed context\n" {
			t.Fatalf("prepare and build must use the refresh: %s: %s, %v", path, data, err)
		}
	}
	data, err := os.ReadFile(original)
	if err != nil || string(data) != "captured context\n" {
		t.Fatal("refresh modified captured inputs")
	}
	if err := os.WriteFile(original, []byte("Wolfi changed its patch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := upstreamPatch(pkg, captured, "fix.patch"); err == nil {
		t.Fatal("accepted a changed upstream patch")
	}
	if err := copyUpstreamPatches(pkg, captured, ""); err == nil {
		t.Fatal("capture accepted a stale patch refresh")
	}
}

func TestUpstreamPatchPathsStayInsideTheirRoots(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := writeFile(filepath.Join(outside, "fix.patch"), []byte("outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../fix.patch", filepath.Join(outside, "fix.patch"), "escape/fix.patch"} {
		if _, err := patchFile(root, name); err == nil {
			t.Fatalf("accepted escaping patch path %s", name)
		}
	}
}
