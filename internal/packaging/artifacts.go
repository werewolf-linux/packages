package packaging

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/werewolf-linux/packages/internal/catalog"
	"github.com/werewolf-linux/packages/internal/overlay"
	"gopkg.in/yaml.v3"
)

var safeAPKName = catalog.Name
var safeAPKVersion = regexp.MustCompile(`^[a-zA-Z0-9_.+~-]+$`)

// AppBuild is the unsigned native builder's provenance record.
type AppBuild struct {
	AppMetadata
	Format          string            `json:"format"`
	Arch            string            `json:"arch"`
	Commit          string            `json:"commit"`
	RunID           string            `json:"run_id"`
	SourceDateEpoch int64             `json:"source_date_epoch"`
	SHA256          map[string]string `json:"sha256"`
}

// WolfiBuild attests that a signed package set passed its native package tests.
type WolfiBuild struct {
	Package   string            `json:"package"`
	BuildID   string            `json:"build_id"`
	Arch      string            `json:"arch"`
	SourceSHA string            `json:"source_sha"`
	RunID     string            `json:"run_id"`
	Version   string            `json:"version"`
	SHA256    map[string]string `json:"sha256"`
}

// Provenance records an already successful Wolfi build and its exact artifacts.
func Provenance(ctx context.Context, pkg Package, arch string) error {
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	out := filepath.Join(pkg.Build, "packages")
	if _, err := digest(filepath.Join(out, arch, "APKINDEX.tar.gz")); err != nil {
		return fmt.Errorf("missing signed package index: %w", err)
	}
	paths := map[string]string{
		filepath.Join(pkg.Build, "inputs/upstream.lock.json"):                 "upstream.lock.json",
		filepath.Join(pkg.Root, "tools.lock.json"):                            "tools.lock.json",
		filepath.Join(pkg.Directory, "policy.json"):                           "policy.json",
		filepath.Join(pkg.Directory, "overlay.yaml"):                          "overlay.yaml",
		filepath.Join(pkg.Build, pkg.Name+".yaml"):                            pkg.Name + ".yaml",
		filepath.Join(pkg.Root, "release/packages.pub"):                       "packages.pub",
		filepath.Join(pkg.Root, "wolfi/build/keys/werewolf-packages.rsa.pub"): "werewolf-packages.rsa.pub",
	}
	for source, name := range paths {
		if err := copyFile(source, filepath.Join(out, name)); err != nil {
			return err
		}
	}
	if err := copyTree(filepath.Join(pkg.Directory, "patches"), filepath.Join(out, "patches")); err != nil {
		return err
	}
	if err := Run(ctx, pkg.Root, "tar", "-czf", filepath.Join(out, pkg.Name+"-source.tar.gz"), "--exclude=.git", "-C", filepath.Join(pkg.Build, "source"), "."); err != nil {
		return err
	}
	if err := Run(ctx, pkg.Root, "tar", "-czf", filepath.Join(out, "wolfi-inputs.tar.gz"), "-C", filepath.Join(pkg.Build, "inputs"), "."); err != nil {
		return err
	}
	hashes, _, err := hashFiles(out, func(path string) bool { return path == "build.json" })
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(out, "build.json"), WolfiBuild{Package: pkg.Name, BuildID: lock.BuildID, Arch: arch, SourceSHA: env("GITHUB_SHA", ""), RunID: env("GITHUB_RUN_ID", ""), Version: lock.Version + "-r" + fmt.Sprint(lock.Revision*pkg.Spec.EpochMultiplier+lock.Epoch), SHA256: hashes})
}

func wait(ctx context.Context) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Fixture derives an overlay from an offline baseline without capturing inputs.
func Fixture(pkg Package, path string) ([]byte, error) {
	baseline, err := readRecipe(path)
	if err != nil {
		return nil, err
	}
	entry := overlay.Field(baseline, "package")
	pin, err := overlay.SourceCommit(baseline)
	if err != nil {
		return nil, err
	}
	var epoch int
	if _, err := fmt.Sscan(overlay.Value(entry, "epoch"), &epoch); err != nil {
		return nil, err
	}
	lock := overlay.Lock{Recipe: overlay.Value(entry, "name") + ".yaml", Version: overlay.Value(entry, "version"), Epoch: epoch, Revision: pkg.Policy.Revision, SourceCommit: pin}
	vars, err := overlay.Variables(baseline, lock, pkg.Policy.MaxFailPerMille, filepath.Join(pkg.Root, "wolfi/pipelines"))
	if err != nil {
		return nil, err
	}
	result, err := overlay.Derive(pkg.Spec, baseline, lock, pkg.Directory, vars)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(result)
}
