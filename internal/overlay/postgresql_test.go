package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostgreSQLPreservesLatestRuntimeFixes(t *testing.T) {
	directory, err := filepath.Abs("../../databases/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := Read(filepath.Join(directory, "overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/postgresql.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(directory, "tests/fixtures/postgresql-18.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"18.6", "19.1"} {
		t.Run(version, func(t *testing.T) {
			lock.Version = version
			upstream := Clone(baseline)
			Put(Field(upstream, "package"), "version", String(version))
			vars, err := Variables(upstream, lock, 0, "../../wolfi/pipelines")
			if err != nil {
				t.Fatal(err)
			}
			recipe, err := Derive(spec, upstream, lock, directory, vars)
			if err != nil {
				t.Fatal(err)
			}
			before := Clone(recipe)
			tested, err := TestRecipe(spec, recipe, directory, vars)
			if err != nil {
				t.Fatal(err)
			}
			if !Equal(before, recipe) {
				t.Fatal("test setup changed the build recipe")
			}
			pipeline := Field(Field(tested, "test"), "pipeline")
			if Value(pipeline.Content[0], "name") != "Initialize test account databases" {
				t.Fatal("missing disposable account database setup")
			}
			lifecycle := pipeline.Content[len(pipeline.Content)-1]
			if Value(lifecycle, "name") != "Database lifecycle and consumer tests" {
				t.Fatal("database lifecycle was not joined")
			}
			started := false
			for _, step := range Field(Field(recipe, "test"), "pipeline").Content {
				started = started || Value(step, "name") == "Test database creation"
				if started && !strings.Contains(Value(lifecycle, "runs"), Value(step, "runs")) {
					t.Fatalf("lost database assertion %q", Value(step, "name"))
				}
			}
			needs, err := Select(lifecycle, []string{"needs", "packages"})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"openssl", "shadow", "postgresql-dev", "postgresql-contrib"} {
				found := false
				for _, need := range needs.Content {
					found = found || need.Value == name
				}
				if !found {
					t.Fatalf("lost lifecycle dependency %s", name)
				}
			}
			for _, phase := range []struct {
				name string
				path string
			}{
				{"context", "per-version-dirs.patch"},
				{"prepared", "src/Makefile.global.in"},
			} {
				root := t.TempDir()
				path := filepath.Join(root, phase.path)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				text := "override pkglibdir := /usr/lib/postgresql18\nincludedir_server = $(pkgincludedir)/18/server\n"
				if err := os.WriteFile(path, []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
				edits := spec.ContextEdits
				if phase.name == "prepared" {
					edits = spec.PreparedEdits
				}
				if err := EditFiles(edits, root, directory, vars); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want := strings.ReplaceAll(text, "18", strings.Split(version, ".")[0])
				if string(got) != want {
					t.Fatalf("%s directory retargeting: %s", phase.name, got)
				}
				if err := os.WriteFile(path, []byte("changed upstream layout"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := EditFiles(edits, root, directory, vars); err == nil {
					t.Fatal("accepted a changed upstream directory layout")
				}
			}
			// New non-shell lifecycle steps require review rather than being lost.
			bad := Clone(recipe)
			step, err := Node(map[string]string{"uses": "test/unreviewed"})
			if err != nil {
				t.Fatal(err)
			}
			steps := Field(Field(bad, "test"), "pipeline")
			steps.Content = append(steps.Content, step)
			if _, err := TestRecipe(spec, bad, directory, vars); err == nil {
				t.Fatal("accepted changed lifecycle tests")
			}
		})
	}
}
