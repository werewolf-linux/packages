package smoke

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/packaging"
	"gopkg.in/yaml.v3"
)

func TestSmokeKeepsExplicitScratchAndConfinesApplication(t *testing.T) {
	repository, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"www/wordpress-sqlite/package.yaml"} {
		data, err := os.ReadFile(filepath.Join(repository, name))
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), "\nchanges:", "\n  subpackages:\n    - name: ${{package.name}}-extra\n      pipeline: [{runs: \"true\"}]\n      test:\n        pipeline: [{runs: \"true\"}]\nchanges:", 1))
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{"fake-tools"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"Werewolf/internal", "Werewolf/pipelines", "Werewolf/runner", "Werewolf/keys", ".github/workflows", ".github/actions", "Werewolf/cmd/package", "Werewolf/cmd/package-helper"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join(repository, dir))); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"Werewolf/go.mod", "Werewolf/go.sum", "Makefile", "Werewolf/tools.lock.json"} {
		data, err := os.ReadFile(filepath.Join(repository, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := packaging.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := packaging.Paths(entries[0], "aarch64")["artifacts"]
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "fixture.apk"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	pinData, err := os.ReadFile(filepath.Join(root, "Werewolf/tools.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pin map[string]map[string]string
	if err := json.Unmarshal(pinData, &pin); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo v" + pin["melange"]["version"] + "; fi\n" + `if [ "$1" = test ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --repository-append ]; then
      test "$2" = "/repo with spaces" || exit 1
      exit 0
    fi
    shift
  done
  exit 1
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(root, "fake-tools/melange"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Setenv("MELANGE", filepath.Join(root, "fake-tools/melange"))
	t.Setenv("PATH", filepath.Join(root, "fake-tools")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(root)
	if err := Check(t.Context(), "wordpress-sqlite", "aarch64", "bubblewrap", "/repo with spaces"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TMPDIR") != scratch {
		t.Fatal("explicit scratch directory was replaced")
	}
	paths, err := filepath.Glob(filepath.Join(entries[0].Build, "aarch64/tests/aarch64-*/test.yaml"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("test recipe paths: %v, %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var recipe struct {
		Subpackages []struct {
			Name string
			Test struct{ Pipeline []struct{ Runs string } }
		}
		Test struct {
			Environment struct {
				Contents struct {
					Packages []string
				}
				Accounts struct {
					RunAs string `yaml:"run-as"`
				}
			}
			Pipeline []struct{ Runs string }
		}
	}
	if err := yaml.Unmarshal(data, &recipe); err != nil {
		t.Fatal(err)
	}
	if len(recipe.Subpackages) != 1 || recipe.Subpackages[0].Name != "${{package.name}}-extra" || len(recipe.Subpackages[0].Test.Pipeline) != 1 {
		t.Fatal("native subpackage test was lost")
	}
	if recipe.Test.Environment.Accounts.RunAs != "0" {
		t.Fatal("bootstrap identity changed")
	}
	if !slices.Contains(recipe.Test.Environment.Contents.Packages, "mksh") {
		t.Fatal("test root needs a shell when hardened BusyBox omits /bin/sh")
	}
	if len(recipe.Test.Pipeline) != 1 {
		t.Fatal("expected one confined runtime command")
	}
	for _, constraint := range []string{"--ro-bind / /", "--uid 1000 --gid 1000 --cap-drop ALL", `test "$(id -u)" -ne 0`, "/proc/self/status"} {
		if !strings.Contains(recipe.Test.Pipeline[0].Runs, constraint) {
			t.Fatalf("missing runtime restriction %q", constraint)
		}
	}
}

func TestRuntimePaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec spec
		want []string
	}{
		{"Node", spec{Allow: []string{"node:native", "node:writable-source"}}, []string{"node", "node/native", "node/writable-source"}},
		{"Langflow Python", spec{Allow: []string{"python:native"}}, []string{"python", "python/native"}},
		{"mixed runtimes", spec{Allow: []string{"node:native", "python:writable-source", "python:native"}}, []string{"node", "node/native", "python", "python/writable-source", "python/native"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := runtimePaths(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			want := make([]map[string]any, 0, len(tc.want))
			for _, path := range tc.want {
				kind, permissions := "directory", 0o755
				if filepath.Dir(path) != "." {
					kind, permissions = "empty-file", 0o444
				}
				want = append(want, map[string]any{"path": "/etc/runtime-hardening/allow/all/" + path, "type": kind, "permissions": permissions})
			}
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("paths = %v; want %v", paths, want)
			}
		})
	}
}

func TestRuntimePathsRejectUnsupportedAllowances(t *testing.T) {
	for _, s := range []spec{
		{Allow: []string{"native"}},
		{Allow: []string{"writable-source"}},
		{Allow: []string{"inline"}},
		{Allow: []string{"ruby:native"}},
		{Allow: []string{"python:inline"}},
		{Allow: []string{"python:../inline"}},
	} {
		if _, err := runtimePaths(s); err == nil {
			t.Fatalf("accepted unsupported allowances: %+v", s)
		}
	}
}

func TestSmokeCommandPreservesShellLiterals(t *testing.T) {
	dir := t.TempDir()
	// Only emulate the mount command here; native smoke tests verify the
	// actual UID, capability sets, mount flags and interpreter restrictions.
	for name, script := range map[string]string{
		"bwrap":   "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n",
		"unshare": "#!/bin/sh\nwhile [ \"${1#--}\" != \"$1\" ]; do shift; done\nexec \"$@\"\n",
		"setpriv": "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n",
		"mount":   "#!/bin/sh\nexit 0\n",
		"ip":      "#!/bin/sh\ntest \"$*\" = 'link set lo up'\n",
		"id":      "#!/bin/sh\necho 1000\n",
		"chmod":   "#!/bin/sh\ntest \"$*\" = '755 /'\n",
		"awk":     "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, runner := range []string{"bubblewrap", "qemu"} {
		t.Run(runner, func(t *testing.T) {
			cmd := exec.Command("sh", "-ec", smokeCommand("printf '%s\\n' \"it's literal\" '$HOME' '`uname`'", runner))
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
			got, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, got)
			}
			if string(got) != "it's literal\n$HOME\n`uname`\n" {
				t.Fatalf("commands were changed: %q", got)
			}
		})
	}
}
