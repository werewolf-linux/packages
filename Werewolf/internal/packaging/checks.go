package packaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/werewolf-linux/packages/Werewolf/internal/update"
)

// Checks runs explicitly declared contributor checks. It does not infer tests
// from filenames, require extra Go modules, or receive publication credentials.
func Checks(ctx context.Context, packages []Package) error {
	var failures []error
	for _, pkg := range packages {
		for i, check := range pkg.Config.Checks {
			name := check.Name
			if name == "" {
				name = fmt.Sprintf("check %d", i+1)
			}
			directory := pkg.Directory
			if check.WorkingDirectory != "" {
				directory = filepath.Join(directory, check.WorkingDirectory)
			}
			base, err := filepath.EvalSymlinks(pkg.Directory)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %s: %w", pkg.Name, name, err))
				continue
			}
			resolved, err := filepath.EvalSymlinks(directory)
			if err == nil {
				relative, relErr := filepath.Rel(base, resolved)
				if relErr != nil || !filepath.IsLocal(relative) {
					err = fmt.Errorf("check directory escapes the package")
				}
			}
			if err == nil {
				slog.Info("running package check", "package", pkg.Name, "check", name)
				err = update.RunCommand(ctx, resolved, "sh", "-eu", "-c", check.Runs)
			}
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %s: %w", pkg.Name, name, err))
			}
		}
	}
	return errors.Join(failures...)
}
