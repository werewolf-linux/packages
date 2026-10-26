package overlay

import (
	"slices"
	"testing"
)

func TestOutputNamesSubstituteRecipeVars(t *testing.T) {
	recipe, err := Parse([]byte("package: {name: samba, version: '4.25.1'}\n" +
		"vars: {python-version: '3.13'}\n" +
		"subpackages:\n- name: py${{vars.python-version}}-samba\n- name: ${{package.name}}-server\n"))
	if err != nil {
		t.Fatal(err)
	}
	names, err := OutputNames(recipe)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"py3.13-samba", "samba", "samba-server"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	unknown, err := Parse([]byte("package: {name: samba, version: '1'}\nsubpackages:\n- name: py${{vars.missing}}-samba\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OutputNames(unknown); err == nil {
		t.Fatal("accepted an unknown var in a package name")
	}
}
