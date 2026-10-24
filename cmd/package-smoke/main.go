// package-smoke installs newly built APKs in a disposable Melange test runner.
// It never modifies the build artifacts or receives release credentials.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/werewolf-linux/packages/internal/update"
	"gopkg.in/yaml.v3"
)

type spec struct {
	Packages []string `json:"packages"`
	Runs     string   `json:"runs"`
	Allow    []string `json:"allow,omitempty"`
}

func main() {
	name := flag.String("package", "", "package context")
	arch := flag.String("arch", "", "native APK architecture")
	flag.Parse()
	if err := check(*name, *arch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(name, arch string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	policies, err := update.LoadPolicies(root)
	if err != nil {
		return err
	}
	if _, ok := policies[name]; !ok {
		return fmt.Errorf("unknown package context")
	}
	if arch != "x86_64" && arch != "aarch64" {
		return fmt.Errorf("unsupported architecture")
	}
	data, err := os.ReadFile("tests/smoke.json")
	if err != nil {
		return err
	}
	specs := map[string]spec{}
	if err := json.Unmarshal(data, &specs); err != nil {
		return err
	}
	s, ok := specs[name]
	if !ok {
		fmt.Println(name + ": native build and APK validation passed; no additional runtime smoke test")
		return nil
	}
	directory, err := update.ContextPath(root, name)
	if err != nil {
		return err
	}
	paths, _ := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if len(paths) != 1 {
		return fmt.Errorf("expected one recipe")
	}
	data, err = os.ReadFile(paths[0])
	if err != nil {
		return err
	}
	var recipe struct {
		Package struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
			Epoch   uint64 `yaml:"epoch"`
		} `yaml:"package"`
	}
	if err := yaml.Unmarshal(data, &recipe); err != nil {
		return err
	}
	meta := recipe.Package
	version := fmt.Sprintf("%s-r%d", meta.Version, meta.Epoch)
	dir := filepath.Join(root, "build/smoke", name, arch)
	repository := filepath.Join(dir, "repository")
	if err := os.MkdirAll(filepath.Join(repository, arch), 0o755); err != nil {
		return err
	}
	apks, _ := filepath.Glob(filepath.Join(root, "build/artifacts", name, arch, "*.apk"))
	if len(apks) == 0 {
		return fmt.Errorf("missing native artifacts")
	}
	for _, apk := range apks {
		target := filepath.Join(repository, arch, filepath.Base(apk))
		if err := os.Symlink(apk, target); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	index := append([]string{"index", "--arch", arch, "--output", filepath.Join(repository, arch, "APKINDEX.tar.gz")}, apks...)
	if err := update.RunCommand(ctx, root, "melange", index...); err != nil {
		return err
	}
	packages := append([]string{"busybox", "ca-certificates-bundle", meta.Name + "=" + version}, s.Packages...)
	mutations := []map[string]any{}
	if len(s.Allow) != 0 {
		directories := map[string]bool{}
		for _, allow := range s.Allow {
			language, group := "node", allow
			switch allow {
			case "native", "writable-source":
			case "python:writable-source":
				language, group = "python", "writable-source"
			default:
				return fmt.Errorf("unsupported runtime test allowance: %s", allow)
			}
			directory := "/etc/runtime-hardening/allow/all/" + language
			if !directories[directory] {
				mutations = append(mutations, map[string]any{"path": directory, "type": "directory", "permissions": 0o755})
				directories[directory] = true
			}
			mutations = append(mutations, map[string]any{"path": directory + "/" + group, "type": "empty-file", "permissions": 0o444})
		}
	}
	config := map[string]any{
		"package": map[string]any{"name": meta.Name, "version": meta.Version, "epoch": meta.Epoch, "description": "Native package smoke test"},
		"test": map[string]any{
			"environment": map[string]any{"accounts": map[string]any{"run-as": "1000"}, "paths": mutations, "contents": map[string]any{
				"repositories": []string{repository, "https://packages.wolfi.dev/os", "https://dist.werewolf-linux.org/apk/hardened", "https://dist.werewolf-linux.org/apk/packages"},
				"packages":     packages,
			}},
			"pipeline": []map[string]string{{"runs": "test \"$(id -u)\" -ne 0\n" + strings.ReplaceAll(s.Runs, "${{package.version}}", meta.Version)}},
		},
	}
	data, err = yaml.Marshal(config)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return update.RunCommand(ctx, root, "melange", "test", path, meta.Name, "--arch", arch, "--runner", "bubblewrap", "--ignore-signatures", "--source-dir", filepath.Join(root, "tests/smoke"), "--timeout", "10m")
}
