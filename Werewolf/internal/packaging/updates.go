package packaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/werewolf-linux/packages/Werewolf/internal/update"
)

// Updates preserves package failure isolation and produces one report contract
// for both engines. Discovery-only runs never replace selected source inputs.
func Updates(ctx context.Context, packages []Package, checkout string, write bool) ([]update.Result, error) {
	results := make([]update.Result, 0, len(packages))
	var failures []error
	client := update.NewClient(env("GH_TOKEN", ""))
	for _, pkg := range packages {
		perPackage, cancel := context.WithTimeout(ctx, 30*time.Minute)
		var result update.Result
		if pkg.Engine != "wolfi" {
			result = client.Update(perPackage, pkg.Root, pkg.Name, pkg.Update, write, update.RunCommand)
		} else {
			result = update.Result{Package: pkg.Name, Status: "failed"}
			before, _ := VerifyInputs(pkg)
			result.Current = before.Version
			lock, err := capture(perPackage, &pkg, checkout, write)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Latest = lock.Version
				result.Status = "current"
				if before.BuildID != lock.BuildID {
					result.Status = "available"
					if write {
						result.Status = "updated"
					}
				}
			}
		}
		cancel()
		if result.Status == "failed" {
			failures = append(failures, fmt.Errorf("%s: %s", pkg.Name, result.Error))
		}
		results = append(results, result)
	}
	return results, errors.Join(failures...)
}
