package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRefreshDispatchPreservesLiteralPackageSelector(t *testing.T) {
	type step struct {
		ID  string            `yaml:"id"`
		Env map[string]string `yaml:"env"`
		Run string            `yaml:"run"`
	}
	var workflow struct {
		Jobs map[string]struct{ Steps []step } `yaml:"jobs"`
	}
	data, err := os.ReadFile("../.github/workflows/wolfi-refresh.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var discovery step
	for _, candidate := range workflow.Jobs["discover"].Steps {
		if candidate.ID == "discover" {
			discovery = candidate
		}
	}
	if discovery.Run == "" || discovery.Env["PACKAGE"] != "${{ inputs.package }}" {
		t.Fatal("discovery must receive the workflow selector through PACKAGE")
	}
	for _, selector := range []string{"", "postgresql", "--local", "postgresql; touch unexpected", "$(touch unexpected)"} {
		t.Run(selector, func(t *testing.T) {
			root := t.TempDir()
			for _, directory := range []string{"build/tools", "wolfi"} {
				if err := os.MkdirAll(filepath.Join(root, directory), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "build/tools/package"), []byte("#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$RECORDED_ARGS\"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "args")
			command := exec.CommandContext(t.Context(), "bash", "-eu", "-c", discovery.Run)
			command.Dir = filepath.Join(root, "wolfi")
			command.Env = append(os.Environ(), "PACKAGE="+selector, "RECORDED_ARGS="+output)
			if data, err := command.CombinedOutput(); err != nil {
				t.Fatalf("discovery command: %v: %s", err, data)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
			want := []string{"discover", "--github", "--package", selector}
			if !slices.Equal(got, want) {
				t.Fatalf("arguments = %q; want %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(command.Dir, "unexpected")); !os.IsNotExist(err) {
				t.Fatal("selector was executed as shell code")
			}
		})
	}
}
