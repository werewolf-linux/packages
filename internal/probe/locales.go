package probe

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// LocaleOptions selects upstream's declared locale fixtures and compiled path.
type LocaleOptions struct {
	Build       string `yaml:"build"`
	Source      string `yaml:"source,omitempty"`
	Makefile    string `yaml:"makefile"`
	Destination string `yaml:"destination"`
}

func Locales(ctx context.Context, options LocaleOptions) error {
	build, err := filepath.Abs(options.Build)
	if err != nil {
		return err
	}
	source := options.Source
	if source == "" {
		source = filepath.Dir(build)
	}
	makefile, err := filepath.Abs(options.Makefile)
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(source, "*/Makefile"))
	if err != nil {
		return err
	}
	declaration := regexp.MustCompile(`(?m)^LOCALES\s*[:+?]?=`)
	for _, file := range files {
		if filepath.Base(filepath.Dir(file)) == "benchtests" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !declaration.Match(data) {
			continue
		}
		if _, err := checked(ctx, "make", "-C", filepath.Dir(file), "objdir="+build, "-f", "Makefile", "-f", makefile, "rtld-prefix=", "stop-on-test-failure=yes", "-j4", "neutered-locale-fixtures"); err != nil {
			return err
		}
	}
	locales, err := os.ReadDir(filepath.Join(build, "localedata"))
	if err != nil {
		return err
	}
	count := 0
	for _, locale := range locales {
		directory := filepath.Join(build, "localedata", locale.Name())
		if !locale.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(directory, "LC_CTYPE")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("invalid locale fixture: %s", directory)
		}
		destination := filepath.Join(options.Destination, locale.Name())
		if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("non-regular locale fixture: %s", path)
			}
			input, err := os.Open(path)
			if err != nil {
				return err
			}
			defer input.Close()
			output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, input)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("no upstream locale fixtures generated")
	}
	slog.Info("installed explicit upstream locale fixtures", "count", count, "destination", options.Destination)
	return nil
}
