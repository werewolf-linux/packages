package packaging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/werewolf-linux/packages/Werewolf/internal/apk"
	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

type TestOptions struct {
	Arch, Runner, Report string
	Build, Fresh         bool
	Repositories         []string
}

type TestResult struct {
	Package      string   `json:"package"`
	Arch         string   `json:"arch"`
	Inputs       string   `json:"inputs"`
	Status       string   `json:"status"`
	Stage        string   `json:"stage,omitempty"`
	Log          string   `json:"log"`
	Error        string   `json:"error,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
}

// TestPackages runs the existing single-package commands, isolating failures
// and keeping one log per context. The test programs remain native Melange data.
func TestPackages(ctx context.Context, packages []Package, options TestOptions) ([]TestResult, error) {
	tool, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return testPackages(ctx, packages, options, tool)
}

func testPackages(ctx context.Context, packages []Package, options TestOptions, tool string) ([]TestResult, error) {
	if !slices.Contains(Arches, options.Arch) {
		return nil, fmt.Errorf("unsupported architecture %q", options.Arch)
	}
	results := make([]TestResult, len(packages))
	for i, pkg := range packages {
		results[i] = TestResult{Package: pkg.Name, Arch: options.Arch, Inputs: filepath.Base(pkg.Build), Status: "pending", Log: filepath.Join(Paths(pkg, options.Arch)["logs"], "test.log"), Repositories: options.Repositories}
	}
	if err := SaveReport(options.Report, results); err != nil {
		return results, err
	}
	var failures []error
	for i, pkg := range packages {
		if err := ctx.Err(); err != nil {
			return results, errors.Join(append(failures, err)...)
		}
		result := &results[i]
		result.Status = "running"
		if err := SaveReport(options.Report, results); err != nil {
			return results, err
		}
		err := testPackage(ctx, pkg, options, tool, result)
		result.Status = "passed"
		if err != nil {
			result.Status, result.Error = "failed", err.Error()
			failures = append(failures, fmt.Errorf("%s: %w", pkg.Name, err))
			if ctx.Err() != nil {
				result.Status = "cancelled"
			}
		}
		slog.Info("package tests finished", "package", pkg.Name, "status", result.Status, "log", result.Log)
		if err := SaveReport(options.Report, results); err != nil {
			return results, errors.Join(append(failures, err)...)
		}
	}
	return results, errors.Join(failures...)
}

func testPackage(ctx context.Context, pkg Package, options TestOptions, tool string, result *TestResult) error {
	if options.Fresh {
		if err := os.RemoveAll(Paths(pkg, options.Arch)["work"]); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(result.Log), 0755); err != nil {
		return err
	}
	log, err := os.Create(result.Log)
	if err != nil {
		return err
	}
	defer log.Close()
	run := func(stage string) (runErr error) {
		result.Stage = stage
		slog.Info("running package tests", "package", pkg.Name, "stage", stage)
		args := []string{stage, "--root", pkg.Root, "--package", pkg.Name, "--arch", options.Arch}
		if options.Runner != "" {
			args = append(args, "--runner", options.Runner)
		}
		for _, repository := range options.Repositories {
			args = append(args, "--repository-append", repository)
		}
		command := exec.CommandContext(ctx, tool, args...)
		command.Dir = pkg.Root
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
		command.WaitDelay = 10 * time.Second
		scratch, err := os.MkdirTemp(filepath.Dir(result.Log), "."+stage+"-")
		if err != nil {
			return err
		}
		defer func() {
			if runErr == nil || stage == "test" {
				runErr = errors.Join(runErr, os.RemoveAll(scratch))
			}
		}()
		command.Env = buildEnv(map[string]string{"TMPDIR": scratch})
		for _, key := range []string{"MELANGE", "MELANGE_CPU", "MELANGE_MEMORY"} {
			if value, ok := os.LookupEnv(key); ok {
				command.Env = append(command.Env, key+"="+value)
			}
		}
		command.Stdout = io.MultiWriter(os.Stderr, log)
		command.Stderr = command.Stdout
		if err := command.Run(); err != nil {
			return fmt.Errorf("%s failed; see %s: %w", stage, result.Log, err)
		}
		return nil
	}
	if options.Build {
		result.Stage = "build"
		ready, err := localTestArtifacts(ctx, pkg, options.Arch)
		if err != nil {
			return fmt.Errorf("inspect local APKs (use --fresh to rebuild): %w", err)
		}
		if !ready {
			if err := run("build"); err != nil {
				return err
			}
			// Wolfi's build command already runs its installed Melange suites.
			if pkg.Engine == "wolfi" {
				return nil
			}
		}
	}
	return run("test")
}

// Existing APKs are reusable only when every output has the selected identity.
// This is a local testing check; publication retains its separate provenance gate.
func localTestArtifacts(ctx context.Context, pkg Package, arch string) (bool, error) {
	var names []string
	var version string
	directory := Paths(pkg, arch)["artifacts"]
	if pkg.Engine == "wolfi" {
		recipe, err := Derive(pkg)
		if err != nil {
			return false, err
		}
		names, err = overlay.OutputNames(recipe)
		if err != nil {
			return false, err
		}
		identity := overlay.Field(recipe, "package")
		version = overlay.Value(identity, "version") + "-r" + overlay.Value(identity, "epoch")
		directory = filepath.Join(directory, arch)
	} else {
		meta, err := Metadata(ctx, pkg)
		if err != nil {
			return false, err
		}
		names, version = meta.Names, meta.Version
	}
	found := 0
	for _, name := range names {
		path := filepath.Join(directory, name+"-"+version+".apk")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return false, err
		}
		if err := checkTestAPK(path, name, version, arch); err != nil {
			return false, err
		}
		found++
	}
	if found != 0 && found != len(names) {
		return false, fmt.Errorf("incomplete APK set: %d/%d outputs", found, len(names))
	}
	return found > 0, nil
}

// Local artifacts can be unsigned application outputs or signed Wolfi outputs.
// Inspect checks their payload hash; the selected identity must still match.
// Melange verifies Wolfi signatures when installing the actual test environment.
func checkTestAPK(path, name, version, arch string) error {
	info, err := apk.Inspect(path)
	if err != nil {
		return err
	}
	if info.Name != name || info.Version != version || info.Arch != arch {
		return fmt.Errorf("APK metadata differs from expected package: %s", path)
	}
	return nil
}
