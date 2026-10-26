// package-smoke installs newly built APKs in a disposable Melange test runner.
// It never modifies the build artifacts or receives release credentials.
package smoke

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
	"github.com/werewolf-linux/packages/Werewolf/internal/packaging"
	"github.com/werewolf-linux/packages/Werewolf/internal/update"
	"gopkg.in/yaml.v3"
)

type spec struct {
	Packages       []string `json:"packages"`
	Runs           string   `json:"runs"`
	Allow          []string `json:"allow,omitempty"`
	PreserveMtimes []string `json:"preserve_mtimes,omitempty"`
}

func Check(ctx context.Context, name, arch, runner string) error {
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
	entries, err := packaging.Load(root)
	if err != nil {
		return err
	}
	var pkg packaging.Package
	for _, candidate := range entries {
		if candidate.Name == name {
			pkg = candidate
		}
	}
	if pkg.Name == "" || pkg.Engine != "upstream" {
		return fmt.Errorf("unknown application context %q", name)
	}
	if arch != "x86_64" && arch != "aarch64" {
		return fmt.Errorf("unsupported architecture")
	}
	directory, err := update.ContextPath(root, name)
	if err != nil {
		return err
	}
	definition, err := catalog.Read(filepath.Join(directory, "package.yaml"))
	if err != nil {
		return err
	}
	nativeTest := overlay.Field(&definition.Melange, "test")
	hasTests := nativeTest != nil
	if subs := overlay.Field(&definition.Melange, "subpackages"); subs != nil {
		for _, sub := range subs.Content {
			hasTests = hasTests || overlay.Field(sub, "test") != nil
		}
	}
	if !hasTests {
		fmt.Println(name + ": no additional installed-package tests")
		return nil
	}
	s := spec{Allow: definition.Test.Allow, PreserveMtimes: definition.Test.PreserveMtimes}
	if pkgs := overlay.Field(overlay.Field(overlay.Field(nativeTest, "environment"), "contents"), "packages"); pkgs != nil {
		if err := pkgs.Decode(&s.Packages); err != nil {
			return err
		}
	}
	testData, err := catalog.YAML(&definition.Melange)
	if err != nil {
		return err
	}
	s.Runs = string(testData)
	data, err := definition.Recipe()
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
	base := filepath.Join(pkg.Build, arch, "tests")
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
	apks, _ := filepath.Glob(filepath.Join(packaging.Paths(pkg, arch)["artifacts"], "*.apk"))
	if len(apks) == 0 {
		return fmt.Errorf("missing native artifacts")
	}
	for _, apk := range apks {
		target := filepath.Join(repository, arch, filepath.Base(apk))
		if err := os.Symlink(apk, target); err != nil {
			return err
		}
	}
	tool, err := packaging.Builder(ctx, root)
	if err != nil {
		return err
	}
	index := append([]string{"index", "--arch", arch, "--output", filepath.Join(repository, arch, "APKINDEX.tar.gz")}, apks...)
	if err := update.RunCommand(ctx, root, tool, index...); err != nil {
		return err
	}
	packages := append([]string{"busybox", "bubblewrap", "ca-certificates-bundle", meta.Name + "=" + version}, s.Packages...)
	if runner == "qemu" {
		packages = append(packages, "setpriv", "util-linux-misc", "mount", "iproute2")
	}
	mutations, err := runtimePaths(s)
	if err != nil {
		return err
	}
	setup, err := restoreMtimes(apks, s.PreserveMtimes)
	if err != nil {
		return err
	}
	config := overlay.Clone(&definition.Melange)
	sections := []*yaml.Node{config}
	if subs := overlay.Field(config, "subpackages"); subs != nil {
		sections = append(sections, subs.Content...)
	}
	for _, section := range sections {
		testConfig := overlay.Field(section, "test")
		if testConfig == nil {
			continue
		}
		environment := overlay.Field(testConfig, "environment")
		if environment == nil {
			environment, _ = overlay.Node(map[string]any{})
			overlay.Put(testConfig, "environment", environment)
		}
		contents := overlay.Field(environment, "contents")
		if contents == nil {
			contents, _ = overlay.Node(map[string]any{})
			overlay.Put(environment, "contents", contents)
		}
		packageNode, _ := overlay.Node(packages)
		overlay.Put(contents, "packages", packageNode)
		repositories, _ := overlay.Node([]string{repository, "https://packages.wolfi.dev/os", "https://dist.werewolf-linux.org/apk/hardened", "https://dist.werewolf-linux.org/apk/packages"})
		if existing := overlay.Field(contents, "repositories"); existing != nil {
			repositories.Content = append(repositories.Content, existing.Content...)
		}
		overlay.Put(contents, "repositories", repositories)
		if definition.Test.Confined {
			accounts, _ := overlay.Node(map[string]string{"run-as": "0"})
			overlay.Put(environment, "accounts", accounts)
			paths, _ := overlay.Node(mutations)
			if old := overlay.Field(environment, "paths"); old != nil {
				paths.Content = append(old.Content, paths.Content...)
			}
			overlay.Put(environment, "paths", paths)
			var confine func(*yaml.Node)
			confine = func(n *yaml.Node) {
				if n.Kind == yaml.MappingNode {
					if runs := overlay.Field(n, "runs"); runs != nil {
						runs.Value = setup + smokeCommand(strings.ReplaceAll(runs.Value, "${{package.version}}", meta.Version), runner)
					}
				}
				for _, child := range n.Content {
					confine(child)
				}
			}
			confine(overlay.Field(testConfig, "pipeline"))
		}
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
	if err := os.MkdirAll(fixtures, 0755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(directory, "tests")); err == nil {
		if err := os.CopyFS(fixtures, os.DirFS(filepath.Join(directory, "tests"))); err != nil {
			return err
		}
	}
	if strings.Contains(s.Runs, "shared-tests/") {
		if err := os.CopyFS(filepath.Join(fixtures, "shared-tests"), os.DirFS(filepath.Join(root, "Werewolf/pipelines/tests"))); err != nil {
			return err
		}
	}
	if strings.Contains(s.Runs, "werewolf-tools/package-helper") {
		if err := packaging.InstallHelper(ctx, root, arch, fixtures); err != nil {
			return err
		}
	}
	if runner == "qemu" {
		kernel, initramfs, err := packaging.Guest(ctx, root, arch, tool)
		if err != nil {
			return err
		}
		scratch := filepath.Join(pkg.Build, arch, "test-runner")
		if err := os.MkdirAll(scratch, 0755); err != nil {
			return err
		}
		for key, value := range map[string]string{"QEMU_KERNEL_IMAGE": kernel, "QEMU_BASE_INITRAMFS": initramfs, "QEMU_DISKS_PATH": scratch} {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	args := []string{"test", path, "--arch", arch, "--runner", runner, "--ignore-signatures", "--source-dir", fixtures, "--cpu", "2", "--memory", "4Gi", "--timeout", "10m"}
	names, err := overlay.OutputNames(config)
	if err != nil {
		return err
	}
	for _, name := range names {
		args = append(args, "--test-package-append", name+"="+version)
	}
	return update.RunCommand(ctx, root, tool, args...)

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
	command := "/bin/sh -ec '" + strings.ReplaceAll(runs, "'", "'\"'\"'") + "'"
	// QEMU runs through a chrooted SSH session, which prevents bubblewrap's
	// user namespace and pivot_root. Bind mounts provide the read-only root
	// inside the private VM, then setpriv drops all application privileges.
	if runner == "qemu" {
		bootstrap := "chmod 755 /\n" +
			"mount --make-rprivate /\nmount --bind / /\nmount -o remount,bind,ro /\n" +
			"mount -t tmpfs tmpfs /tmp\nip link set lo up\n" +
			"exec setpriv --reuid=1000 --regid=1000 --clear-groups --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs -- " + command
		return "exec unshare --mount --net --fork /bin/sh -ec '" + strings.ReplaceAll(bootstrap, "'", "'\"'\"'") + "'"
	}
	// Make the root traversable before entering the runtime as UID 1000.
	return "chmod 755 /\nexec bwrap --unshare-user --unshare-pid --unshare-net --die-with-parent " +
		// Melange already supplies procfs in its isolated PID namespace.
		"--ro-bind / / --ro-bind /home/build /home/build --dev /dev --ro-bind /proc /proc " +
		"--tmpfs /tmp --uid 1000 --gid 1000 --cap-drop ALL -- " + command
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
