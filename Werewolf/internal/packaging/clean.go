package packaging

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// WorkLock prevents explicit cleanup from removing an active build or test.
// Shared locks permit independent package jobs to run concurrently.
func WorkLock(root string, exclusive bool) (func(), error) {
	if err := ensureWorkRoot(root); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, "Werewolf/.work/maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("build, test or cleanup already running: %w", err)
	}
	return func() { file.Close() }, nil
}

// CleanRepositoryCache evicts only one temporary repository's cached APKs.
// Remote repositories remain cached across runs, avoiding repeated downloads.
func CleanRepositoryCache(cache, repository string) error {
	if filepath.IsAbs(repository) {
		repository = (&url.URL{Scheme: "file", Path: repository}).String()
	}
	entries, err := os.ReadDir(cache)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		decoded, err := url.QueryUnescape(entry.Name())
		if err == nil && strings.TrimSuffix(decoded, "/") == strings.TrimSuffix(repository, "/") {
			if err := os.RemoveAll(filepath.Join(cache, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Clean removes disposable scratch and shared download caches. Captures, source
// checkouts, APKs, signing keys, reports and diagnostic logs remain available.
func Clean(root string, packages []Package, arch string) error {
	if !slices.Contains(Arches, arch) {
		return fmt.Errorf("unsupported architecture %q", arch)
	}
	unlock, err := WorkLock(root, true)
	if err != nil {
		return err
	}
	defer unlock()
	work, err := os.OpenRoot(filepath.Join(root, "Werewolf/.work"))
	if err != nil {
		return err
	}
	defer work.Close()
	remove := func(path string) error {
		if _, err := work.Lstat(path); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		if err := work.RemoveAll(path); err != nil {
			return err
		}
		slog.Info("removed generated work", "path", path)
		return nil
	}
	for _, pkg := range packages {
		base := filepath.Join("packages", pkg.Name)
		identities, err := fs.ReadDir(work.FS(), filepath.ToSlash(base))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, id := range identities {
			if !id.IsDir() {
				continue
			}
			native := filepath.Join(base, id.Name(), arch)
			for _, name := range []string{"runner", "test-runner", "tests", "test-context"} {
				if err := remove(filepath.Join(native, name)); err != nil {
					return err
				}
			}
			logs := filepath.Join(native, "logs")
			entries, err := fs.ReadDir(work.FS(), filepath.ToSlash(logs))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".test-") || strings.HasPrefix(entry.Name(), ".build-") {
					if err := remove(filepath.Join(logs, entry.Name())); err != nil {
						return err
					}
				}
			}
		}
	}
	return remove("cache")
}
