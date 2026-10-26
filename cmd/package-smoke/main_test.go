package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSmokeKeepsExplicitScratchAndConfinesApplication(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"www/wordpress-sqlite/metadata.json", "www/wordpress-sqlite/wordpress-sqlite.yaml", "tests/smoke.json"} {
		data, err := os.ReadFile(filepath.Join(repository, name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{"wolfi", "tests/smoke", "build/artifacts/wordpress-sqlite/aarch64", "fake-tools"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "build/artifacts/wordpress-sqlite/aarch64/fixture.apk"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fake-tools/melange"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Setenv("PATH", filepath.Join(root, "fake-tools")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(root)
	if err := check(t.Context(), "wordpress-sqlite", "aarch64", "bubblewrap"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TMPDIR") != scratch {
		t.Fatal("explicit scratch directory was replaced")
	}
	paths, err := filepath.Glob("build/smoke/wordpress-sqlite/aarch64-*/test.yaml")
	if err != nil || len(paths) != 1 {
		t.Fatalf("test recipe paths: %v, %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var recipe struct {
		Test struct {
			Environment struct {
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
	if recipe.Test.Environment.Accounts.RunAs != "0" {
		t.Fatal("bootstrap identity changed")
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
