// Package packaging implements the common package build and release commands.
package packaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/werewolf-linux/packages/internal/catalog"
	"github.com/werewolf-linux/packages/internal/overlay"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// Package holds the catalog entry and its checked Wolfi overlay, if any.
type Package struct {
	catalog.Package
	Root      string
	Directory string
	Build     string
	Spec      *overlay.Spec
	Policy    struct {
		Revision        int `json:"revision"`
		MaxFailPerMille int `json:"max_fail_per_mille,omitempty"`
	}
}

// Load reads all package definitions without contacting upstream services.
func Load(root string) ([]Package, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := catalog.Load(root)
	if err != nil {
		return nil, err
	}
	packages := make([]Package, 0, len(entries))
	for _, entry := range entries {
		pkg := Package{Package: entry, Root: root, Directory: filepath.Join(root, entry.Path), Build: filepath.Join(root, "build/packages", entry.Name)}
		if entry.Engine == "wolfi" {
			pkg.Build = filepath.Join(root, "build/packages", entry.Name)
			pkg.Spec = &entry.Config.Wolfi.Spec
			pkg.Policy.Revision = entry.Config.Wolfi.Revision
			pkg.Policy.MaxFailPerMille = entry.Config.Wolfi.MaxFailPerMille
		}
		if err := selectBuild(&pkg); err != nil {
			return nil, err
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// Run streams a tool's output and propagates cancellation and failure.
func Run(ctx context.Context, directory, name string, args ...string) error {
	slog.Debug("running tool", "tool", name, "directory", directory)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func output(ctx context.Context, directory, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		detail := stdout.String() + stderr.String()
		if len(detail) > 4096 {
			detail = detail[len(detail)-4096:]
		}
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(detail))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'), 0644)
}

func writeFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".packages-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func copyFile(source, destination string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, f)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, info.Mode())
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in build inputs: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func hashFiles(directory string, exclude func(string) bool) (map[string]string, map[string]uint32, error) {
	hashes := map[string]string{}
	modes := map[string]uint32{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if exclude(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in build inputs: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular build input: %s", path)
		}
		sum, err := digest(path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(relative)] = sum
		modes[filepath.ToSlash(relative)] = unixMode(info)
		return nil
	})
	return hashes, modes, err
}

func unixMode(info fs.FileInfo) uint32 {
	mode := uint32(info.Mode().Perm())
	if info.Mode()&os.ModeSetuid != 0 {
		mode |= 04000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		mode |= 02000
	}
	if info.Mode()&os.ModeSticky != 0 {
		mode |= 01000
	}
	return mode
}
