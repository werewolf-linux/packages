package packaging

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/werewolf-linux/packages/internal/apk"
	"github.com/werewolf-linux/packages/internal/overlay"
	"github.com/werewolf-linux/packages/internal/update"
	"gopkg.in/yaml.v3"
)

// Arches are built and validated independently before publication.
var Arches = []string{"x86_64", "aarch64"}

// Architecture returns APK's native architecture spelling.
func Architecture() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x86_64"
	default:
		return runtime.GOARCH
	}
}

func melange(ctx context.Context, root string) (string, string, error) {
	tool := os.Getenv("MELANGE")
	if tool == "" {
		tool = "melange"
		for _, path := range []string{"build/tools/melange"} {
			candidate := filepath.Join(root, path)
			if _, err := os.Stat(candidate); err == nil {
				tool = candidate
				break
			}
		}
	}
	pin, err := melangePin(root)
	if err != nil {
		return "", "", err
	}
	version, err := output(ctx, root, tool, "version")
	if err != nil {
		return "", "", err
	}
	if !strings.Contains(version, "v"+pin.Version) {
		return "", "", fmt.Errorf("install pinned Melange v%s", pin.Version)
	}
	return tool, pin.Version, nil
}

// Builder locates and verifies the pinned Melange used by every build and test command.
func Builder(ctx context.Context, root string) (string, error) {
	tool, _, err := melange(ctx, root)
	return tool, err
}

// UnwrapKernel extracts arm64's kernel Image from an EFI zboot wrapper.
func UnwrapKernel(image []byte) ([]byte, error) {
	if len(image) < 8 || string(image[4:8]) != "zimg" {
		return image, nil
	}
	if len(image) < 16 {
		return nil, fmt.Errorf("truncated zboot header")
	}
	offset, size := uint64(binary.LittleEndian.Uint32(image[8:12])), uint64(binary.LittleEndian.Uint32(image[12:16]))
	if offset > uint64(len(image)) || size > uint64(len(image))-offset {
		return nil, fmt.Errorf("truncated zboot image")
	}
	reader, err := gzip.NewReader(bytes.NewReader(image[offset : offset+size]))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 512<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 512<<20 {
		return nil, fmt.Errorf("oversized kernel image")
	}
	return data, nil
}

// Guest supplies the cached Linux kernel and initramfs used by Melange QEMU.
func Guest(ctx context.Context, root, arch, tool string) (string, string, error) {
	kernel, initramfs := os.Getenv("QEMU_KERNEL_IMAGE"), os.Getenv("QEMU_BASE_INITRAMFS")
	if kernel != "" && initramfs != "" {
		return kernel, initramfs, nil
	}
	directory := filepath.Join(root, "build/guest", arch)
	kernel, initramfs = filepath.Join(directory, "vmlinuz"), filepath.Join(directory, "initramfs.cpio")
	if info, err := os.Stat(kernel); err == nil && info.Mode().IsRegular() {
		if info, err := os.Stat(initramfs); err == nil && info.Mode().IsRegular() {
			return kernel, initramfs, nil
		}
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", "", err
	}
	work, err := os.MkdirTemp(directory, ".guest-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(work)
	if err := os.MkdirAll(filepath.Join(work, "root/usr"), 0755); err != nil {
		return "", "", err
	}
	rootfs := filepath.Join(work, "rootfs.tar")
	if err := Run(ctx, filepath.Join(root, "tools/guest"), "apko", "build-minirootfs", "--build-arch", arch, "melange-guest.yaml", rootfs); err != nil {
		return "", "", err
	}
	base := filepath.Join(work, "base.cpio")
	if err := Run(ctx, root, tool, "initramfs", "--arch", arch, "--output", base); err != nil {
		return "", "", err
	}
	if err := Run(ctx, root, "bsdtar", "-xf", rootfs, "-C", filepath.Join(work, "root/usr"), "--exclude", "lib/modules/*/vmlinuz", "lib/modules"); err != nil {
		return "", "", err
	}
	if err := Run(ctx, root, "bsdtar", "-xf", rootfs, "-C", filepath.Join(work, "root"), "boot/vmlinuz-virt"); err != nil {
		return "", "", err
	}
	image, err := os.ReadFile(filepath.Join(work, "root/boot/vmlinuz-virt"))
	if err != nil {
		return "", "", err
	}
	image, err = UnwrapKernel(image)
	if err != nil {
		return "", "", err
	}
	assembled := filepath.Join(work, "initramfs.cpio")
	if err := copyFile(base, assembled); err != nil {
		return "", "", err
	}
	out, err := os.OpenFile(assembled, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, "bsdtar", "--format", "newc", "--uid", "0", "--gid", "0", "--numeric-owner", "-cf", "-", "usr/lib/modules")
	cmd.Dir = filepath.Join(work, "root")
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		return "", "", runErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if err := writeFile(kernel, image, 0644); err != nil {
		return "", "", err
	}
	if err := os.Rename(assembled, initramfs); err != nil {
		return "", "", err
	}
	return kernel, initramfs, nil
}

func buildEnv(extra map[string]string) []string {
	var env []string
	for _, key := range []string{"PATH", "HOME", "LANG", "SSL_CERT_FILE", "QEMU_KERNEL_IMAGE", "QEMU_BASE_INITRAMFS"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func runBuild(ctx context.Context, root, tool string, env []string, args []string) error {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}

// BuildWolfi builds and tests one derived recipe with signed local packages.
func BuildWolfi(ctx context.Context, pkg Package, arch string, testOnly bool) error {
	if !slices.Contains(Arches, arch) {
		return fmt.Errorf("unsupported architecture %q", arch)
	}
	var err error
	pkg, err = NativeWorkspace(pkg, arch)
	if err != nil {
		return err
	}
	if !testOnly {
		if _, err := os.Stat(filepath.Join(pkg.Build, "source")); os.IsNotExist(err) {
			if err := Prepare(ctx, pkg, filepath.Join(pkg.Build, "source"), ""); err != nil {
				return err
			}
		}
	}
	recipe, err := Derive(pkg)
	if err != nil {
		return err
	}
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	tool, version, err := melange(ctx, pkg.Root)
	if err != nil {
		return err
	}
	engine := pkg.Root
	if _, err := SigningKey(engine, false); err != nil {
		return err
	}
	runner := "qemu"
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err == nil {
			runner = "bubblewrap"
		}
	}
	scratch := filepath.Join(pkg.Build, "runner")
	if err := os.MkdirAll(scratch, 0755); err != nil {
		return err
	}
	gate, err := os.OpenFile(filepath.Join(scratch, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer gate.Close()
	// Nonblocking attempts preserve cancellation while another build owns the guest.
	for {
		err := syscall.Flock(int(gate.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		if err := wait(ctx); err != nil {
			return err
		}
	}
	defer syscall.Flock(int(gate.Fd()), syscall.LOCK_UN)
	env := buildEnv(map[string]string{"TMPDIR": scratch, "SOURCE_DATE_EPOCH": strconv.FormatInt(lock.SourceDateEpoch, 10)})
	cpu, memory := wolfiResources(pkg.Spec)
	manifest := map[string]any{"package": pkg.Name, "runner": runner, "arch": arch, "melange_version": version, "source_date_epoch": lock.SourceDateEpoch, "cpu": cpu, "memory": memory}
	if runner == "qemu" {
		kernel, initramfs, err := Guest(ctx, pkg.Root, arch, tool)
		if err != nil {
			return err
		}
		env = append(env, "QEMU_KERNEL_IMAGE="+kernel, "QEMU_BASE_INITRAMFS="+initramfs, "QEMU_DISKS_PATH="+scratch)
		for name, path := range map[string]string{"kernel_sha256": kernel, "guest_sha256": initramfs} {
			sum, err := digest(path)
			if err != nil {
				return err
			}
			manifest[name] = sum
		}
	} else {
		kernel, err := output(ctx, pkg.Root, "uname", "-r")
		if err != nil {
			return err
		}
		if pkg.Spec.MinKernel != "" && update.Compare(strings.SplitN(kernel, "-", 2)[0], pkg.Spec.MinKernel) < 0 {
			return fmt.Errorf("host kernel %s is older than %s minimum %s", kernel, pkg.Name, pkg.Spec.MinKernel)
		}
		manifest["kernel"] = kernel
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			return err
		}
		bwrap, err = filepath.EvalSymlinks(bwrap)
		if err != nil {
			return err
		}
		sum, err := digest(bwrap)
		if err != nil {
			return err
		}
		manifest["bubblewrap_sha256"] = sum
		if _, err := os.Stat(bwrap + ".apparmor"); err == nil {
			sum, err := digest(bwrap + ".apparmor")
			if err != nil {
				return err
			}
			manifest["apparmor_profile_sha256"] = sum
		}
	}
	packages := filepath.Join(pkg.Build, "packages")
	filename := "runner.json"
	if testOnly {
		filename = "test-runner.json"
	}
	if err := writeJSON(filepath.Join(packages, filename), manifest); err != nil {
		return err
	}
	common := []string{"--runner", runner, "--arch", arch, "--cpu", cpu, "--memory", memory}
	pipelines := filepath.Join(pkg.Build, "inputs/pipelines")
	wolfiKey := filepath.Join(pkg.Build, "inputs/wolfi-signing.rsa.pub")
	if !testOnly {
		if len(pkg.Config.Actions) != 0 {
			if err := copyFile(filepath.Join(pkg.Directory, "package.yaml"), filepath.Join(pkg.Build, "context/package.yaml")); err != nil {
				return err
			}
			if err := InstallHelper(ctx, pkg.Root, arch, filepath.Join(pkg.Build, "context")); err != nil {
				return err
			}
		}
		args := append([]string{"build", filepath.Join(pkg.Build, pkg.Name+".yaml")}, common...)
		args = append(args, "--source-dir", filepath.Join(pkg.Build, "context"), "--namespace", "werewolf", "--pipeline-dir", pipelines, "--repository-append", "https://packages.wolfi.dev/os", "--keyring-append", wolfiKey, "--signing-key", filepath.Join(engine, "build/keys/werewolf-packages.rsa"), "--cache-dir", filepath.Join(scratch, "cache"), "--out-dir", packages)
		if err := runBuild(ctx, engine, tool, env, args); err != nil {
			return err
		}
	}
	testContext := filepath.Join(pkg.Build, "test-context")
	if err := os.RemoveAll(testContext); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(pkg.Build, "inputs/upstream"), testContext); err != nil {
		return err
	}
	if len(pkg.Config.Actions) != 0 {
		if err := copyFile(filepath.Join(pkg.Directory, "package.yaml"), filepath.Join(testContext, "package.yaml")); err != nil {
			return err
		}
		if err := InstallHelper(ctx, pkg.Root, arch, testContext); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: http.FileServer(http.Dir(packages)), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Close()
		if err := <-served; err != nil && err != http.ErrServerClosed {
			slog.Error("package repository server failed", "error", err)
		}
	}()
	repository := "http://" + listener.Addr().String()
	inside := repository
	if runner == "qemu" {
		inside = "http://10.0.2.2:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	}
	config, err := wolfiTestRecipe(pkg, recipe, lock, inside)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	testRecipe := filepath.Join(pkg.Build, pkg.Name+"-test.yaml")
	if err := writeFile(testRecipe, data, 0644); err != nil {
		return err
	}
	names, err := overlay.OutputNames(config)
	if err != nil {
		return err
	}
	entry := overlay.Field(config, "package")
	fullVersion := overlay.Value(entry, "version") + "-r" + overlay.Value(entry, "epoch")
	args := append([]string{"test", testRecipe}, common...)
	args = append(args, "--source-dir", testContext, "--pipeline-dirs", pipelines)
	for _, name := range names {
		unpinned := false
		for _, pattern := range pkg.Spec.TestUnpinned {
			match, err := filepath.Match(pattern, name)
			if err != nil {
				return err
			}
			unpinned = unpinned || match
		}
		if !unpinned {
			args = append(args, "--test-package-append", name+"="+fullVersion)
		}
	}
	extras := pkg.Spec.TestPackages
	if extras == nil {
		extras = []string{"busybox", "apk-tools"}
	}
	for _, name := range extras {
		args = append(args, "--test-package-append", name)
	}
	args = append(args, "--repository-append", repository, "--repository-append", "https://packages.wolfi.dev/os", "--keyring-append", filepath.Join(engine, "build/keys/werewolf-packages.rsa.pub"), "--keyring-append", wolfiKey)
	return runBuild(ctx, engine, tool, env, args)
}

func wolfiResources(spec *overlay.Spec) (string, string) {
	cpu, memory := "4", "8Gi"
	if spec.Resources.CPU != "" {
		cpu = spec.Resources.CPU
	}
	if spec.Resources.Memory != "" {
		memory = spec.Resources.Memory
	}
	return env("MELANGE_CPU", cpu), env("MELANGE_MEMORY", memory)
}

func wolfiTestRecipe(pkg Package, recipe *yaml.Node, lock Inputs, repository string) (*yaml.Node, error) {
	baseline, err := readRecipe(filepath.Join(pkg.Build, "inputs/upstream.yaml"))
	if err != nil {
		return nil, err
	}
	upstream, err := effective(pkg, baseline, lock)
	if err != nil {
		return nil, err
	}
	vars, err := overlay.Variables(upstream, lock.Lock, pkg.Policy.MaxFailPerMille, filepath.Join(pkg.Root, "pipelines"))
	if err != nil {
		return nil, err
	}
	vars["test.repository"] = repository
	return overlay.TestRecipe(pkg.Spec, recipe, pkg.Directory, vars)
}

// AppMetadata describes the complete APK set and its downstream build inputs.
type AppMetadata struct {
	Context string   `json:"context"`
	Names   []string `json:"names"`
	Version string   `json:"version"`
	Inputs  string   `json:"inputs"`
}

func appRecipe(pkg Package) (string, error) {
	data, err := pkg.Config.Recipe()
	if err != nil {
		return "", err
	}
	path := filepath.Join(pkg.Build, "recipe.yaml")
	return path, writeFile(path, data, 0644)
}

// Metadata queries Melange so recipe parsing agrees with the actual builder.
func Metadata(ctx context.Context, pkg Package) (AppMetadata, error) {
	var metadata AppMetadata
	recipe, err := appRecipe(pkg)
	if err != nil {
		return metadata, err
	}
	tool, _, err := melange(ctx, pkg.Root)
	if err != nil {
		return metadata, err
	}
	template := `{"name":{{printf "%q" .Package.Name}},"version":{{printf "%q" .Package.Version}},"epoch":{{.Package.Epoch}},"subpackages":[{{range $i,$s := .Subpackages}}{{if $i}},{{end}}{{printf "%q" $s.Name}}{{end}}]}`
	text, err := output(ctx, pkg.Root, tool, "query", recipe, template)
	if err != nil {
		return metadata, err
	}
	var found struct {
		Name, Version string
		Epoch         int
		Subpackages   []string
	}
	if err := json.Unmarshal([]byte(text), &found); err != nil {
		return metadata, err
	}
	if !safeAPKName.MatchString(found.Name) || !safeAPKVersion.MatchString(found.Version) || found.Epoch < 0 {
		return metadata, fmt.Errorf("unsafe APK identity")
	}
	if strings.HasSuffix(pkg.Name, "-vuln-demo") && found.Name != pkg.Name {
		return metadata, fmt.Errorf("vulnerable APK must use its context name")
	}
	names := []string{found.Name}
	for _, name := range found.Subpackages {
		names = append(names, strings.NewReplacer("${{package.name}}", found.Name, "${{package.version}}", found.Version).Replace(name))
	}
	slices.Sort(names)
	for i, name := range names {
		if !safeAPKName.MatchString(name) || (i > 0 && names[i-1] == name) {
			return metadata, fmt.Errorf("invalid or duplicate APK name: %s", name)
		}
	}
	id, err := InputID(pkg)
	if err != nil {
		return metadata, err
	}
	return AppMetadata{Context: pkg.Name, Names: names, Version: found.Version + "-r" + strconv.Itoa(found.Epoch), Inputs: id}, nil
}

// BuildApplication runs the native builder with a minimal credential-free environment.
func BuildApplication(ctx context.Context, pkg Package, arch string) error {
	if runtime.GOOS != "linux" || arch != Architecture() {
		return fmt.Errorf("application CI build requires a native Linux %s runner", arch)
	}
	metadata, err := Metadata(ctx, pkg)
	if err != nil {
		return err
	}
	recipe, err := appRecipe(pkg)
	if err != nil {
		return err
	}
	directory, err := helperContext(ctx, pkg, arch)
	if err != nil {
		return err
	}
	recipe = filepath.Join(directory, filepath.Base(recipe))
	tool, _, err := melange(ctx, pkg.Root)
	if err != nil {
		return err
	}
	commit := os.Getenv("GITHUB_SHA")
	if commit == "" {
		commit, err = output(ctx, pkg.Root, "git", "-c", "safe.directory="+pkg.Root, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
	}
	date, err := applicationTimestamp(ctx, pkg, commit)
	if err != nil {
		return err
	}
	out := filepath.Join(pkg.Build, arch, "packages")
	if _, err := os.Lstat(filepath.Join(out, arch)); !os.IsNotExist(err) {
		return fmt.Errorf("use a clean artifact directory")
	}
	scratch := filepath.Join(pkg.Build, arch, "runner")
	if err := os.MkdirAll(scratch, 0755); err != nil {
		return err
	}
	args := []string{"build", recipe, "--runner", "bubblewrap", "--arch", arch, "--source-dir", directory, "--out-dir", out, "--cache-dir", filepath.Join(pkg.Root, "build/cache/melange"), "--apk-cache-dir", filepath.Join(pkg.Root, "build/cache/apk"), "--generate-index=false", "--namespace", "werewolf", "--git-commit", commit, "--cpu", env("MELANGE_CPU", strconv.Itoa(runtime.NumCPU())), "--memory", env("MELANGE_MEMORY", "8Gi"), "--timeout", "5h"}
	if err := runBuild(ctx, pkg.Root, tool, buildEnv(map[string]string{"TMPDIR": scratch, "SOURCE_DATE_EPOCH": strconv.FormatInt(date, 10)}), args); err != nil {
		return err
	}
	hashes := map[string]string{}
	actual, err := filepath.Glob(filepath.Join(out, arch, "*.apk"))
	if err != nil {
		return err
	}
	if len(actual) != len(metadata.Names) {
		return fmt.Errorf("missing or unexpected built packages")
	}
	for _, name := range metadata.Names {
		filename := name + "-" + metadata.Version + ".apk"
		path := filepath.Join(out, arch, filename)
		if _, err := apk.Check(path, name, metadata.Version, arch, nil); err != nil {
			return err
		}
		sum, err := digest(path)
		if err != nil {
			return err
		}
		hashes[filename] = sum
	}
	return writeJSON(filepath.Join(out, arch, "build.json"), AppBuild{AppMetadata: metadata, Format: "werewolf-package-build/1", Arch: arch, Commit: commit, RunID: os.Getenv("GITHUB_RUN_ID"), SourceDateEpoch: date, SHA256: hashes})
}

// New local contexts have no path history yet. Anchor those builds to the
// recorded commit; never use the wall clock for reproducible package inputs.
func applicationTimestamp(ctx context.Context, pkg Package, commit string) (int64, error) {
	timestamp, err := output(ctx, pkg.Root, "git", "-c", "safe.directory="+pkg.Root, "log", "-1", "--format=%ct", commit, "--", pkg.Path)
	if err != nil {
		return 0, err
	}
	if timestamp == "" {
		timestamp, err = output(ctx, pkg.Root, "git", "-c", "safe.directory="+pkg.Root, "show", "-s", "--format=%ct", commit)
		if err != nil {
			return 0, err
		}
	}
	date, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || date <= 0 {
		return 0, fmt.Errorf("missing source timestamp for %s at %s", pkg.Name, commit)
	}
	return date, nil
}

// BuildLocalApplication uses the sibling Werewolf checkout's native build API.
func BuildLocalApplication(ctx context.Context, pkg Package, arch, werewolf string) error {
	werewolf, err := filepath.Abs(werewolf)
	if err != nil {
		return err
	}
	recipe, err := appRecipe(pkg)
	if err != nil {
		return err
	}
	directory, err := helperContext(ctx, pkg, arch)
	if err != nil {
		return err
	}
	recipe = filepath.Join(directory, filepath.Base(recipe))
	if err := Run(ctx, werewolf, "make", "howl"); err != nil {
		return err
	}
	return Run(ctx, werewolf, filepath.Join(werewolf, "build/host/howl"), "build-apk", recipe, "--arch", arch)
}
