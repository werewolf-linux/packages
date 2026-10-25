package packaging

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Archive writes reproducible bundles; filesystem timestamps and owners do not
// change the bytes when staging the same validated artifacts again.
func Archive(directory, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".archive-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	compressed := gzip.NewWriter(file)
	defer compressed.Close()
	writer := tar.NewWriter(compressed)
	defer writer.Close()
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in artifact bundle: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular artifact: %s", path)
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = "."
		if relative != "." {
			header.Name = "./" + filepath.ToSlash(relative)
		}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.ModTime = time.Unix(0, 0)
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}

// Extract accepts regular files and directories only, beneath a new directory.
// Release bundles never need symlinks, device nodes, or archive path traversal.
func Extract(archive, directory string) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	var size int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name == "." || name == "" {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return fmt.Errorf("invalid archive root")
		}
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("unsafe archive path: %s", name)
		}
		name = filepath.Clean(name)
		if seen[name] {
			return fmt.Errorf("duplicate archive path: %s", name)
		}
		seen[name] = true
		if header.Size < 0 || header.Size > (64<<30)-size || len(seen) > 1_000_000 {
			return fmt.Errorf("oversized release archive")
		}
		size += header.Size
		path := filepath.Join(directory, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.FileMode(header.Mode)&0777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, reader)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported release archive member: %s", name)
		}
	}
}

// RestoreInputs verifies an extracted bundle before installing it as a package's
// captured inputs. A failed restore leaves a previous capture available.
func RestoreInputs(pkg Package, archive string) error {
	if err := os.MkdirAll(pkg.Build, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(pkg.Build, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := Extract(archive, filepath.Join(stage, "inputs")); err != nil {
		return err
	}
	candidate := pkg
	candidate.Build = stage
	if _, err := VerifyInputs(candidate); err != nil {
		return err
	}
	destination := filepath.Join(pkg.Build, "inputs")
	backup := filepath.Join(stage, "previous")
	hadPrevious := false
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(filepath.Join(stage, "inputs"), destination); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backup, destination); restoreErr != nil {
				return fmt.Errorf("restore inputs: %w; rollback: %v", err, restoreErr)
			}
		}
		return err
	}
	return nil
}
