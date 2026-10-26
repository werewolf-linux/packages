package overlay_test

import (
	"encoding/json"
	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
	. "github.com/werewolf-linux/packages/Werewolf/internal/overlay"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The JSON snapshots were captured from the former Python derivations before
// changing their implementation. They cover complete recipes, not just edits.
func TestDerivationParity(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "package.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no package overlays found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			name := filepath.Base(filepath.Dir(path))
			definition, err := catalog.Read(path)
			if err == nil && definition.Wolfi == nil {
				return
			}
			var spec *Spec
			if err == nil {
				spec = &definition.Wolfi.Spec
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join("testdata", name+".lock.json"))
			if err != nil {
				t.Fatal(err)
			}
			var lock Lock
			if err := json.Unmarshal(data, &lock); err != nil {
				t.Fatal(err)
			}
			fixtures, err := filepath.Glob(filepath.Join(filepath.Dir(path), "tests", "fixtures", "*.yaml"))
			if err != nil || len(fixtures) == 0 {
				t.Fatal("missing baseline fixture", err)
			}
			data, err = os.ReadFile(fixtures[0])
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			before := Clone(baseline)
			vars, err := Variables(baseline, lock, 55, filepath.Join(root, "Werewolf/pipelines"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Derive(spec, baseline, lock, filepath.Dir(path), vars)
			if err != nil {
				t.Fatal(err)
			}
			if !Equal(before, baseline) {
				t.Fatal("derivation modified captured recipe")
			}
			data, err = os.ReadFile(filepath.Join("testdata", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			sortProvides(got)
			sortProvides(want)
			if !Equal(got, want) {
				gotData, _ := yaml.Marshal(got)
				wantData, _ := yaml.Marshal(want)
				t.Errorf("derivation differs from snapshot; got and want written to %s", t.TempDir())
				t.Logf("got:\n%s\nwant:\n%s", gotData, wantData)
			}
			if _, err := OutputNames(got); err != nil {
				t.Fatal(err)
			}
			vars["test.repository"] = "http://127.0.0.1:8080"
			if _, err := TestRecipe(spec, got, filepath.Dir(path), vars); err != nil {
				t.Fatalf("test recipe: %v", err)
			}
			bad := lock
			bad.SourceCommit = strings.Repeat("0", 40)
			if _, err := Derive(spec, baseline, bad, filepath.Dir(path), vars); err == nil {
				t.Fatal("accepted a different source commit")
			}
			pipeline := Field(baseline, "pipeline")
			step, _ := Node(map[string]string{"runs": "build an unreviewed plugin"})
			pipeline.Content = append(pipeline.Content, step)
			if _, err := Derive(spec, baseline, lock, filepath.Dir(path), vars); err == nil {
				t.Fatal("accepted an unreviewed build step")
			}
		})
	}
}

func sortProvides(n *yaml.Node) {
	if provides := Field(n, "provides"); provides != nil {
		slices.SortFunc(provides.Content, func(a, b *yaml.Node) int { return strings.Compare(a.Value, b.Value) })
	}
	for _, child := range n.Content {
		sortProvides(child)
	}
}

func TestEditsFailWithoutPartialChanges(t *testing.T) {
	input, err := Parse([]byte("pipeline:\n- name: build\n  runs: make\n"))
	if err != nil {
		t.Fatal(err)
	}
	before := Clone(input)
	var edits []Edit
	if err := yaml.Unmarshal([]byte("- path: [pipeline, name=build, runs]\n  replace: [{old: make, new: make install}]\n- path: [pipeline, name=missing]\n  delete: true\n"), &edits); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(input, edits, t.TempDir(), nil); err == nil {
		t.Fatal("accepted missing selector")
	}
	if !Equal(before, input) {
		t.Fatal("failed edit modified input")
	}
}

func TestAmbiguousSelector(t *testing.T) {
	input, err := Parse([]byte("pipeline: [{name: build}, {name: build}]"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Select(input, []string{"pipeline", "name=build"}); err == nil {
		t.Fatal("accepted ambiguous selector")
	}
}

func TestUnsafeYAML(t *testing.T) {
	for _, data := range []string{"x: &x [*x]", "x: 1\nx: 2", "x: 1\n---\nx: 2"} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}

func TestIncludeCannotEscapePackage(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "link", outside} {
		if err := Expand(String("${file:"+path+"}"), directory, nil); err == nil {
			t.Errorf("accepted include %s", path)
		}
	}
}
