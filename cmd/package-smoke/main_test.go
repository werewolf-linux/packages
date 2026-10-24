package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
