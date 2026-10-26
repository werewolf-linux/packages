package packaging

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestTemporaryRepositoryCacheEvictionKeepsRemoteAndOtherRuns(t *testing.T) {
	for _, repository := range []string{"/tmp/package tests/repository", "http://127.0.0.1:1234/"} {
		t.Run(repository, func(t *testing.T) {
			cache := t.TempDir()
			local := repository
			if filepath.IsAbs(local) {
				local = (&url.URL{Scheme: "file", Path: local}).String()
			}
			for _, address := range []string{local, local + "-other", "https://packages.wolfi.dev/os"} {
				if err := os.MkdirAll(filepath.Join(cache, url.QueryEscape(address)), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := CleanRepositoryCache(cache, repository); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 2 {
				t.Fatalf("remaining cache entries: %v, %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(cache, url.QueryEscape(local))); !os.IsNotExist(err) {
				t.Fatalf("temporary repository cache remains: %v", err)
			}
		})
	}
}

func TestCleanPreservesArtifactsInputsLogsAndOtherArchitectures(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "Werewolf/.work")
	var pkg Package
	pkg.Name = "example"
	removed := []string{"cache/apk/download", "packages/example/old/aarch64/runner/run-x/guest", "packages/example/old/aarch64/tests/root", "packages/example/old/aarch64/logs/.test-x/guest", "packages/example/new/aarch64/logs/.build-x/guest"}
	kept := []string{"packages/example/old/inputs/source", "packages/example/old/aarch64/packages/example.apk", "packages/example/old/aarch64/logs/test.log", "packages/example/old/x86_64/tests/root", "keys/private.rsa", "tests/report.json"}
	for _, name := range append(append([]string{}, removed...), kept...) {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	unlock, err := WorkLock(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Clean(root, []Package{pkg}, "aarch64"); err == nil {
		t.Fatal("cleanup must refuse to run during builds or tests")
	}
	unlock()
	if err := Clean(root, []Package{pkg}, "aarch64"); err != nil {
		t.Fatal(err)
	}
	for _, name := range removed {
		if _, err := os.Stat(filepath.Join(work, name)); !os.IsNotExist(err) {
			t.Errorf("generated scratch remains: %s: %v", name, err)
		}
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(work, name)); err != nil {
			t.Errorf("retained file removed: %s: %v", name, err)
		}
	}
}

func TestFailedMelangeTestReleasesGuestRoot(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "melange")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nmkdir \"$TMPDIR/guest\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := RunMelange(t.Context(), directory, tool, []string{"TMPDIR=" + directory}, []string{"test"}); err == nil {
		t.Fatal("failed suite reported success")
	}
	roots, err := filepath.Glob(filepath.Join(directory, "run-*"))
	if err != nil || len(roots) != 0 {
		t.Fatalf("failed test scratch remains: %v, %v", roots, err)
	}
}

func TestMelangeCreatesPersistentCachesBeforeLaunchingGuest(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "melange")
	script := `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
    case $1 in
    --cache-dir|--apk-cache-dir)
        shift
        test -d "$1"
        echo cached > "$1/populated"
        ;;
    esac
    shift
done
`
	if err := os.WriteFile(tool, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cache, apkCache := filepath.Join(directory, "build-cache"), filepath.Join(directory, "apk-cache")
	if err := RunMelange(t.Context(), directory, tool, []string{"TMPDIR=" + directory}, []string{"build", "--cache-dir", cache, "--apk-cache-dir", apkCache}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cache, apkCache} {
		if _, err := os.Stat(filepath.Join(path, "populated")); err != nil {
			t.Fatalf("cache did not survive the invocation: %s: %v", path, err)
		}
	}
}
