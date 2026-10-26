package packaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
	"github.com/werewolf-linux/packages/Werewolf/internal/update"
)

func TestWolfiResourcesUsePackageDefaultsAndExplicitOverrides(t *testing.T) {
	definition, err := catalog.Read("../../../lang/node/package.yaml")
	if err != nil {
		t.Fatal(err)
	}
	node := &definition.Wolfi.Spec
	for _, tc := range []struct {
		name, cpu, memory, wantCPU, wantMemory string
		spec                                   *overlay.Spec
	}{
		{"default", "", "", "4", "8Gi", &overlay.Spec{}},
		{"Node", "", "", "2", "16Gi", node},
		{"CPU override", "1", "", "1", "16Gi", node},
		{"memory override", "", "24Gi", "2", "24Gi", node},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MELANGE_CPU", tc.cpu)
			t.Setenv("MELANGE_MEMORY", tc.memory)
			cpu, memory := wolfiResources(tc.spec)
			if cpu != tc.wantCPU || memory != tc.wantMemory {
				t.Fatalf("resources = %s CPU, %s memory; want %s CPU, %s memory", cpu, memory, tc.wantCPU, tc.wantMemory)
			}
		})
	}
}

func TestPackageTestsUseCapturedCheckoutVariables(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		if pkg.Engine != "wolfi" {
			continue
		}
		t.Run(pkg.Name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "Werewolf/internal/overlay/testdata", pkg.Name+".lock.json"))
			if err != nil {
				t.Fatal(err)
			}
			var lock Inputs
			if err := json.Unmarshal(data, &lock.Lock); err != nil {
				t.Fatal(err)
			}
			fixtures, err := filepath.Glob(filepath.Join(pkg.Directory, "tests/fixtures/*.yaml"))
			if err != nil || len(fixtures) == 0 {
				t.Fatalf("missing fixture: %v", err)
			}
			data, err = os.ReadFile(fixtures[0])
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := overlay.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			vars, err := overlay.Variables(baseline, lock.Lock, pkg.Policy.MaxFailPerMille, filepath.Join(root, "Werewolf/pipelines"))
			if err != nil {
				t.Fatal(err)
			}
			recipe, err := overlay.Derive(pkg.Spec, baseline, lock.Lock, pkg.Directory, vars)
			if err != nil {
				t.Fatal(err)
			}
			before := overlay.Clone(recipe)
			pkg.Build = t.TempDir()
			if err := writeFile(filepath.Join(pkg.Build, "inputs/upstream.yaml"), data, 0644); err != nil {
				t.Fatal(err)
			}
			config, err := wolfiTestRecipe(pkg, recipe, lock, "http://10.0.2.2:12345")
			if err != nil {
				t.Fatal(err)
			}
			if !overlay.Equal(before, recipe) {
				t.Fatal("test setup modified the build recipe")
			}
			if _, err := overlay.OutputNames(config); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLatestDoesNotFallBackFromUnpinnedStream(t *testing.T) {
	directory := t.TempDir()
	for name, pin := range map[string]string{"lang-1": "1111111111111111111111111111111111111111", "lang-2": "not pinned"} {
		text := "package:\n  name: " + name + "\n  version: '" + name[5:] + ".0'\npipeline:\n- uses: git-checkout\n  with:\n    expected-commit: " + pin + "\n"
		if err := os.WriteFile(filepath.Join(directory, name+".yaml"), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pkg := Package{Spec: &overlay.Spec{}}
	pkg.Name = "fixture"
	pkg.Update.Family = "lang-"
	if _, _, err := Latest(pkg, directory, nil); err == nil {
		t.Fatal("fell back from unpinned newest stream")
	}
}

func TestBuildIdentityIgnoresUnrelatedWolfiCommit(t *testing.T) {
	lock := Inputs{Lock: overlay.Lock{Recipe: "fixture.yaml", SourceCommit: "1111111111111111111111111111111111111111", Revision: 1}}
	before, err := identity(lock)
	if err != nil {
		t.Fatal(err)
	}
	lock.WolfiCommit = "2222222222222222222222222222222222222222"
	after, err := identity(lock)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("unrelated Wolfi commit changed build identity")
	}
	lock.Revision++
	after, err = identity(lock)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("policy revision did not change build identity")
	}
}

func TestEffectiveReleaseKeepsCapturedRecipe(t *testing.T) {
	baseline, err := overlay.Parse([]byte("package: {name: lang-1, version: '1.0', epoch: 4}\npipeline:\n- uses: git-checkout\n  with: {expected-commit: 1111111111111111111111111111111111111111}\nsubpackages: [{name: lang-1-dev}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	before := overlay.Clone(baseline)
	pkg := Package{Spec: &overlay.Spec{}}
	pkg.Update.Family = "lang-"
	lock := Inputs{Lock: overlay.Lock{Version: "2.0", SourceCommit: "2222222222222222222222222222222222222222"}, Release: &update.Release{Version: "2.0"}}
	recipe, err := effective(pkg, baseline, lock)
	if err != nil {
		t.Fatal(err)
	}
	if !overlay.Equal(before, baseline) {
		t.Fatal("modified captured recipe")
	}
	if got := overlay.Value(overlay.Field(recipe, "package"), "name"); got != "lang-2" {
		t.Fatalf("name = %s", got)
	}
	if got, err := overlay.SourceCommit(recipe); err != nil || got != lock.SourceCommit {
		t.Fatalf("pin = %s, %v", got, err)
	}
}
