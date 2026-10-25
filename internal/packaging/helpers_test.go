package packaging

import (
	"debug/elf"
	"os"
	"path/filepath"
	"testing"
)

func TestHelperBuildDoesNotRequireVCSAccess(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                     "module example.com/helper\n\ngo 1.27.0\n",
		"cmd/package-helper/main.go": "package main\nfunc main() {}\n",
		".git":                       "gitdir: /nonexistent/inaccessible-git-metadata\n",
	}
	for name, data := range files {
		if err := writeFile(filepath.Join(root, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	destination := t.TempDir()
	if err := InstallHelper(t.Context(), root, "aarch64", destination); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination, "werewolf-tools/package-helper")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("helper permissions = %o", info.Mode().Perm())
	}
	binary, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	if binary.Machine != elf.EM_AARCH64 {
		t.Fatalf("helper architecture = %s", binary.Machine)
	}
	for _, program := range binary.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatal("helper requires a guest dynamic loader")
		}
	}
}
