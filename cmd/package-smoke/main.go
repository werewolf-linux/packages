// package-smoke installs newly built APKs in a disposable Melange test runner.
// It never modifies the build artifacts or receives release credentials.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/werewolf-linux/packages/internal/packaging"
	"github.com/werewolf-linux/packages/internal/update"
	"gopkg.in/yaml.v3"
)

type spec struct {
	Packages       []string `json:"packages"`
	Runs           string   `json:"runs"`
	Allow          []string `json:"allow,omitempty"`
	PreserveMtimes []string `json:"preserve_mtimes,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	name := flag.String("package", "", "package context")
	arch := flag.String("arch", "", "native APK architecture")
	runner := flag.String("runner", "", "Melange runner: bubblewrap or qemu; defaults to the host platform")
	flag.Parse()
	if err := check(ctx, *name, *arch, *runner); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(ctx context.Context, name, arch, runner string) error {
	if runner == "" {
		runner = "qemu"
		if runtime.GOOS == "linux" {
			runner = "bubblewrap"
		}
	}
	if runner != "qemu" && runner != "bubblewrap" {
		return fmt.Errorf("unsupported Melange runner: %s", runner)
	}
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
	base := filepath.Join(root, "build/smoke", name)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, arch+"-")
	if err != nil {
		return err
	}
	// Test roots can be gigabytes. A runner's /tmp may be a small tmpfs.
	if os.Getenv("TMPDIR") == "" {
		if err := os.Setenv("TMPDIR", dir); err != nil {
			return err
		}
	}
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
	index := append([]string{"index", "--arch", arch, "--output", filepath.Join(repository, arch, "APKINDEX.tar.gz")}, apks...)
	if err := update.RunCommand(ctx, root, "melange", index...); err != nil {
		return err
	}
	packages := append([]string{"busybox", "bubblewrap", "ca-certificates-bundle", meta.Name + "=" + version}, s.Packages...)
	mutations, err := runtimePaths(s)
	if err != nil {
		return err
	}
	setup, err := restoreMtimes(apks, s.PreserveMtimes)
	if err != nil {
		return err
	}
	config := map[string]any{
		"package": map[string]any{"name": meta.Name, "version": meta.Version, "epoch": meta.Epoch, "description": "Native package smoke test"},
		"test": map[string]any{
			// Melange must mount its owner-only temporary root before the
			// nested bubblewrap command enters UID 1000 and drops capabilities.
			"environment": map[string]any{"accounts": map[string]any{"run-as": "0"}, "paths": mutations, "contents": map[string]any{
				"repositories": []string{repository, "https://packages.wolfi.dev/os", "https://dist.werewolf-linux.org/apk/hardened", "https://dist.werewolf-linux.org/apk/packages"},
				"packages":     packages,
			}},
			"pipeline": []map[string]string{{"runs": setup + smokeCommand(strings.ReplaceAll(s.Runs, "${{package.version}}", meta.Version), runner)}},
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
	fixtures := filepath.Join(dir, "fixtures")
	if err := os.CopyFS(fixtures, os.DirFS(filepath.Join(root, "tests/smoke"))); err != nil {
		return err
	}
	if strings.Contains(s.Runs, "werewolf-tools/package-helper") {
		if err := packaging.InstallHelper(ctx, root, arch, fixtures); err != nil {
			return err
		}
	}
	if runner == "qemu" {
		kernel, initramfs, err := packaging.Guest(ctx, root, arch, "melange")
		if err != nil {
			return err
		}
		scratch := filepath.Join(root, "build/smoke-runner", arch)
		if err := os.MkdirAll(scratch, 0755); err != nil {
			return err
		}
		for key, value := range map[string]string{"QEMU_KERNEL_IMAGE": kernel, "QEMU_BASE_INITRAMFS": initramfs, "QEMU_DISKS_PATH": scratch} {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return update.RunCommand(ctx, root, "melange", "test", path, meta.Name, "--arch", arch, "--runner", runner, "--ignore-signatures", "--source-dir", fixtures, "--cpu", "2", "--memory", "4Gi", "--timeout", "10m")
}

// Installed scripts must run from a read-only filesystem, as they do in an
// appliance. Melange's package-installation root is writable. A nested mount
// namespace makes the runtime and fixtures read-only, with scratch in /tmp.
func smokeCommand(runs, runner string) string {
	runs = `test "$(id -u)" -ne 0
awk '
  /^Cap(Inh|Prm|Eff|Amb):/ {
    if ($2 != "0000000000000000") bad = 1
    count++
  }
  END { exit (count != 4 || bad) }
' /proc/self/status
` + runs
	// QEMU runs through a chrooted SSH session, where Linux rejects creating
	// a user namespace. The private VM already supplies that outer boundary.
	userNamespace := "--unshare-user "
	if runner == "qemu" {
		userNamespace = ""
	}
	// Make the root traversable before entering the runtime as UID 1000.
	return "chmod 755 /\nexec bwrap " + userNamespace + "--unshare-pid --unshare-net --die-with-parent " +
		// Melange already supplies procfs in its isolated PID namespace.
		"--ro-bind / / --ro-bind /home/build /home/build --dev /dev --ro-bind /proc /proc " +
		"--tmpfs /tmp --uid 1000 --gid 1000 --cap-drop ALL -- /bin/sh -ec '" +
		strings.ReplaceAll(runs, "'", "'\"'\"'") + "'"
}

// Allowances name both the runtime and the group, such as python:native.
func runtimePaths(s spec) ([]map[string]any, error) {
	mutations := []map[string]any{}
	directories := map[string]bool{}
	for _, allow := range s.Allow {
		language, group, qualified := strings.Cut(allow, ":")
		if !qualified || (language != "node" && language != "python") || (group != "native" && group != "writable-source") {
			return nil, fmt.Errorf("unsupported runtime test allowance: %s", allow)
		}
		directory := "/etc/runtime-hardening/allow/all/" + language
		if !directories[directory] {
			mutations = append(mutations, map[string]any{"path": directory, "type": "directory", "permissions": 0o755})
			directories[directory] = true
		}
		mutations = append(mutations, map[string]any{"path": directory + "/" + group, "type": "empty-file", "permissions": 0o444})
	}
	return mutations, nil
}
