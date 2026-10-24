package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

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
		"bwrap": "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n",
		"id":    "#!/bin/sh\necho 1000\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", "-ec", smokeCommand("printf '%s\\n' \"it's literal\" '$HOME' '`uname`'"))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	if string(got) != "it's literal\n$HOME\n`uname`\n" {
		t.Fatalf("commands were changed: %q", got)
	}
}
