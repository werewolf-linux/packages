package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = `update:
  github: {identifier: example/project, strip-prefix: v}
melange:
  package: {name: example, version: "1.0", epoch: 0}
  pipeline: [{runs: "make install"}]
changes:
  - what: Build example.
    why: Provide the required runtime.
    evidence: [package.yaml]
`

func TestDefinitionRejectsAmbiguousOrMisspelledConfiguration(t *testing.T) {
	for _, text := range []string{
		example + "typo: true\n",
		example + "---\nother: true\n",
		example + "update: {source: content}\n",
		strings.Replace(example, "github: {identifier: example/project, strip-prefix: v}", "github: {identifier: example/project, strp-prefix: v}", 1),
		strings.Replace(example, "melange:", "wolfi: {recipe: example, revision: 1, sources: [https://example.org/repo.git]}\nmelange:", 1),
		strings.Replace(example, "  package:", "  update: {enabled: true}\n  package:", 1),
		example + "actions: {x: {files: [{path: x}], curl: {binary: curl}}}\n",
	} {
		path := filepath.Join(t.TempDir(), "package.yaml")
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Errorf("accepted invalid definition:\n%s", text)
		}
	}
}

func TestNewPackageIsDiscoverableWithoutRegistryEdits(t *testing.T) {
	root := t.TempDir()
	path, err := New(root, "science/example", "example/project", "")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Update.Repo != "example/project" {
		t.Fatalf("catalog: %+v", packages)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "science/example", "example/other", ""); err == nil {
		t.Fatal("overwrote an existing package")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing definition changed")
	}
	if _, err := New(root, "net/example", "example/project", ""); err == nil {
		t.Fatal("allowed duplicate context identity")
	}
}

func TestGeneratedEditorSchemaMatchesLoader(t *testing.T) {
	actual, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	checked, err := os.ReadFile("../../package.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(actual), bytes.TrimSpace(checked)) {
		t.Fatal("regenerate the schema with package schema --dest Werewolf/package.schema.json")
	}
}
