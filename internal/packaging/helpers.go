package packaging

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// InstallHelper compiles the shared build helper for the guest architecture.
// Go's build cache reuses unchanged code; the binary stays outside the APK.
func InstallHelper(ctx context.Context, root, arch, destination string) error {
	target := map[string]string{"x86_64": "amd64", "aarch64": "arm64"}[arch]
	if target == "" {
		return fmt.Errorf("unsupported helper architecture: %s", arch)
	}
	directory := filepath.Join(root, "build/helpers", arch)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".helper-")
	if err != nil {
		return err
	}
	temporary.Close()
	defer os.Remove(temporary.Name())
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-o", temporary.Name(), "./cmd/package-helper")
	command.Dir = root
	command.Env = buildEnv(map[string]string{"GOOS": "linux", "GOARCH": target, "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off"})
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("compile package helper: %w", err)
	}
	if err := os.Chmod(temporary.Name(), 0755); err != nil {
		return err
	}
	return copyFile(temporary.Name(), filepath.Join(destination, "werewolf-tools/package-helper"))
}

func helperContext(ctx context.Context, pkg Package, arch string) (string, error) {
	if err := ensureBuildRoot(pkg.Root); err != nil {
		return "", err
	}
	destination := filepath.Join(pkg.Build, arch, "context")
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	if err := copyTree(pkg.Directory, destination); err != nil {
		return "", err
	}
	data, err := pkg.Config.Recipe()
	if err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(destination, "recipe.yaml"), data, 0644); err != nil {
		return "", err
	}
	if len(pkg.Config.Actions) != 0 {
		if err := InstallHelper(ctx, pkg.Root, arch, destination); err != nil {
			return "", err
		}
	}
	return destination, nil
}
