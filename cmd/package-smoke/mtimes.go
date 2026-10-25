package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Melange v0.62.1's bubblewrap image loader creates files without restoring
// tar timestamps. Restore explicitly requested files from the APK headers,
// before mounting the runtime read-only. Never derive these from application
// state: the optimized Keycloak server must still validate the packaged mtime.
func restoreMtimes(apks, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	wanted := map[string]bool{}
	for _, name := range paths {
		if !strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "/" || wanted[name] {
			return "", fmt.Errorf("invalid or duplicate timestamp path: %q", name)
		}
		wanted[name] = true
	}
	mtimes := map[string]time.Time{}
	for _, apk := range apks {
		file, err := os.Open(apk)
		if err != nil {
			return "", err
		}
		err = readMtimes(file, wanted, mtimes)
		closeErr := file.Close()
		if err != nil {
			return "", fmt.Errorf("%s: %w", apk, err)
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	var commands strings.Builder
	for _, name := range paths {
		mtime, ok := mtimes[name]
		if !ok {
			return "", fmt.Errorf("timestamp path absent from APKs: %s", name)
		}
		quoted := "'" + strings.ReplaceAll(name, "'", "'\"'\"'") + "'"
		fmt.Fprintf(&commands, "touch -c -d '@%d' -- %s\n", mtime.Unix(), quoted)
	}
	return commands.String(), nil
}

func readMtimes(input io.Reader, wanted map[string]bool, mtimes map[string]time.Time) error {
	buffer := bufio.NewReader(input)
	// APK v2 has separate gzip members for its signature, control and data.
	for {
		member, err := gzip.NewReader(buffer)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		member.Multistream(false)
		archive := tar.NewReader(member)
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				member.Close()
				return err
			}
			name := "/" + strings.TrimPrefix(header.Name, "./")
			if !wanted[name] {
				continue
			}
			if _, duplicate := mtimes[name]; duplicate || header.Typeflag != tar.TypeReg {
				member.Close()
				return fmt.Errorf("duplicate or non-regular timestamp path: %s", name)
			}
			mtimes[name] = header.ModTime
		}
		// Consume member padding and validate its gzip checksum before advancing.
		_, err = io.Copy(io.Discard, member)
		closeErr := member.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
