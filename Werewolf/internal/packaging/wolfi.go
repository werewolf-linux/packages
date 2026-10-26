package packaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
	"github.com/werewolf-linux/packages/Werewolf/internal/update"
	"gopkg.in/yaml.v3"
)

// Inputs records immutable source, recipe, and downstream policy identities.
type Inputs struct {
	overlay.Lock
	Package         string            `json:"package"`
	Repository      string            `json:"repository"`
	WolfiCommit     string            `json:"wolfi_commit"`
	SourceDateEpoch int64             `json:"source_date_epoch"`
	SHA256          map[string]string `json:"sha256"`
	Mode            map[string]uint32 `json:"mode"`
	PolicySHA256    map[string]string `json:"policy_sha256"`
	BuildID         string            `json:"build_id"`
	Wolfi           *Baseline         `json:"wolfi,omitempty"`
	Release         *update.Release   `json:"release,omitempty"`
	SourceKind      string            `json:"source_kind,omitempty"`
	EffectiveRecipe string            `json:"effective_recipe,omitempty"`
}

// Baseline distinguishes the captured Wolfi version from an upstream release.
type Baseline struct {
	Version      string `json:"version"`
	Epoch        int    `json:"epoch"`
	SourceCommit string `json:"source_commit"`
}

var stableRecipe = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*(?:_p[0-9]+|_git[0-9]{8})?$`)
var streamVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)
var exactCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func policyHashes(pkg Package) (map[string]string, error) {
	result := map[string]string{}
	shared := []string{"Werewolf/go.mod", "Werewolf/go.sum", "Makefile", "Werewolf/tools.lock.json", "Werewolf/keys/packages.pub", "Werewolf/runner/ci-root", "Werewolf/runner/guest/melange-guest.yaml", ".github/workflows/wolfi-build.yml", "Werewolf/cmd/package/main.go", "Werewolf/cmd/package-helper/main.go", ".github/actions/setup-builder/action.yml", ".github/workflows/build.yml"}
	for _, directory := range []string{"Werewolf/internal/apk", "Werewolf/internal/catalog", "Werewolf/internal/overlay", "Werewolf/internal/packaging", "Werewolf/internal/probe", "Werewolf/internal/transform", "Werewolf/internal/update", "Werewolf/internal/smoke", "Werewolf/pipelines", "Werewolf/runner/guest"} {
		err := filepath.WalkDir(sourcePath(pkg.Root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "templates" {
					return filepath.SkipDir
				}
				return nil
			}
			// Repairing publication or reporting must not invalidate an already
			// validated native build's inputs during release recovery.
			if directory == "Werewolf/internal/packaging" && slices.Contains([]string{"application_release.go", "archive.go", "discover.go", "history.go", "notes.go", "recovery.go", "store.go", "wolfi_release.go"}, entry.Name()) {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/testdata/") {
				return nil
			}
			relative, err := filepath.Rel(pkg.Root, path)
			if err != nil {
				return err
			}
			shared = append(shared, relative)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, name := range shared {
		sum, err := digest(sourcePath(pkg.Root, name))
		if err != nil {
			return nil, err
		}
		result[filepath.ToSlash(sourceName(pkg.Root, name))] = sum
	}
	hashes, _, err := hashFiles(pkg.Directory, excludedInput)
	if err != nil {
		return nil, err
	}
	if _, ok := hashes["package.yaml"]; ok {
		sum, err := definitionDigest(filepath.Join(pkg.Directory, "package.yaml"))
		if err != nil {
			return nil, err
		}
		hashes["package.yaml"] = sum
	}
	for name, sum := range hashes {
		result[filepath.ToSlash(filepath.Join(pkg.Name, name))] = sum
	}
	return result, nil
}

func identity(lock Inputs) (string, error) {
	relevant := map[string]any{"recipe": lock.Recipe, "source_commit": lock.SourceCommit, "revision": lock.Revision, "sha256": lock.SHA256, "mode": lock.Mode, "policy_sha256": lock.PolicySHA256}
	if lock.Release != nil {
		relevant["release"] = lock.Release
		relevant["version"] = lock.Version
		relevant["epoch"] = lock.Epoch
	}
	data, err := json.Marshal(relevant)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readRecipe(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return overlay.Parse(data)
}

// Latest selects the newest released recipe stream without silently falling
// back when its source checkout has changed or lost its immutable pin.
func Latest(pkg Package, checkout string, release *update.Release) (string, *yaml.Node, error) {
	var paths []string
	if pkg.Update.Recipe != "" {
		paths = []string{filepath.Join(checkout, pkg.Update.Recipe+".yaml")}
	} else {
		var err error
		paths, err = filepath.Glob(filepath.Join(checkout, pkg.Update.Family+"*.yaml"))
		if err != nil {
			return "", nil, err
		}
	}
	var selected string
	var selectedRecipe *yaml.Node
	best := ""
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".yaml")
		stream := "0"
		if pkg.Update.Family != "" {
			stream = strings.TrimPrefix(name, pkg.Update.Family)
		}
		if !streamVersion.MatchString(stream) {
			continue
		}
		config, err := readRecipe(path)
		if err != nil {
			return "", nil, err
		}
		entry := overlay.Field(config, "package")
		version := overlay.Value(entry, "version")
		if overlay.Value(entry, "name") != name || !stableRecipe.MatchString(version) {
			continue
		}
		if pkg.Update.Family != "" && version != stream && !strings.HasPrefix(version, stream+".") {
			continue
		}
		if release != nil && update.Compare(strings.Split(version, "_git")[0], release.Version) > 0 {
			continue
		}
		if pkg.Spec.StableProvides != "" {
			deps := overlay.Field(overlay.Field(entry, "dependencies"), "provides")
			found := false
			if deps != nil {
				for _, p := range deps.Content {
					found = found || p.Value == pkg.Spec.StableProvides
				}
			}
			if !found {
				continue
			}
		}
		if selected == "" || update.Compare(stream, best) > 0 {
			selected, selectedRecipe, best = path, config, stream
		}
	}
	if selected == "" {
		return "", nil, fmt.Errorf("%s: no stable Wolfi recipe", pkg.Name)
	}
	if _, err := overlay.SourceCommit(selectedRecipe); err != nil {
		return "", nil, fmt.Errorf("latest recipe %s: %w", selected, err)
	}
	return selected, selectedRecipe, nil
}

func effective(pkg Package, baseline *yaml.Node, lock Inputs) (*yaml.Node, error) {
	config := overlay.Clone(baseline)
	if lock.Release == nil {
		return config, nil
	}
	entry := overlay.Field(config, "package")
	old := overlay.Value(entry, "name")
	if pkg.Update.Family != "" {
		parts := strings.Split(lock.Version, ".")
		count := pkg.Spec.StreamParts
		if count == 0 {
			count = 1
		}
		if len(parts) < count {
			return nil, fmt.Errorf("release version has too few stream components")
		}
		name := pkg.Update.Family + strings.Join(parts[:count], ".")
		re := regexp.MustCompile(regexp.QuoteMeta(old) + `([^0-9.]|$)`)
		rewrite(config, func(value string) string { return re.ReplaceAllString(value, name+"${1}") })
	}
	overlay.Put(entry, "version", overlay.String(lock.Version))
	epoch, err := overlay.Node(lock.Epoch)
	if err != nil {
		return nil, err
	}
	overlay.Put(entry, "epoch", epoch)
	checkout, err := overlay.Select(config, []string{"pipeline", "uses=git-checkout", "with"})
	if err != nil {
		return nil, err
	}
	pin := overlay.Value(checkout, "expected-commit")
	if key, ok := strings.CutPrefix(pin, "${{vars."); ok && strings.HasSuffix(key, "}}") {
		variables := overlay.Field(config, "vars")
		if variables == nil {
			return nil, fmt.Errorf("checkout references missing vars")
		}
		overlay.Put(variables, strings.TrimSuffix(key, "}}"), overlay.String(lock.SourceCommit))
	} else {
		overlay.Put(checkout, "expected-commit", overlay.String(lock.SourceCommit))
	}
	if len(pkg.Spec.ReleaseEdits) > 0 {
		vars, err := overlay.Variables(config, lock.Lock, pkg.Policy.MaxFailPerMille, sourcePath(pkg.Root, "Werewolf/pipelines"))
		if err != nil {
			return nil, err
		}
		return overlay.Apply(config, pkg.Spec.ReleaseEdits, pkg.Directory, vars)
	}
	return config, nil
}

func rewrite(n *yaml.Node, change func(string) string) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		n.Value = change(n.Value)
	}
	if n.Kind == yaml.MappingNode {
		for i := 1; i < len(n.Content); i += 2 {
			rewrite(n.Content[i], change)
		}
		return
	}
	for _, child := range n.Content {
		rewrite(child, change)
	}
}

// Capture stages a complete input snapshot and validates the effective recipe
// before replacing previously usable inputs.
func Capture(ctx context.Context, target *Package, checkout string) (Inputs, error) {
	return capture(ctx, target, checkout, true)
}

func capture(ctx context.Context, target *Package, checkout string, install bool) (Inputs, error) {
	pkg := *target
	var lock Inputs
	release, err := update.NewClient(os.Getenv("GH_TOKEN")).ResolveRelease(ctx, *pkg.Update.Upstream)
	if err != nil {
		return lock, err
	}
	path, config, err := Latest(pkg, checkout, &release)
	if err != nil {
		return lock, err
	}
	commit, err := output(ctx, checkout, "git", "rev-parse", "HEAD")
	if err != nil {
		return lock, err
	}
	if !exactCommit.MatchString(commit) {
		return lock, fmt.Errorf("invalid Wolfi commit")
	}
	status, err := output(ctx, checkout, "git", "status", "--porcelain")
	if err != nil {
		return lock, err
	}
	if status != "" {
		return lock, fmt.Errorf("Wolfi checkout has local changes")
	}
	date, err := output(ctx, checkout, "git", "log", "-1", "--format=%ct")
	if err != nil {
		return lock, err
	}
	timestamp, err := strconv.ParseInt(date, 10, 64)
	if err != nil || timestamp <= 0 {
		return lock, fmt.Errorf("invalid Wolfi timestamp")
	}
	source, err := overlay.SourceCommit(config)
	if err != nil {
		return lock, err
	}
	epoch, err := strconv.Atoi(overlay.Value(overlay.Field(config, "package"), "epoch"))
	if err != nil {
		return lock, err
	}
	if err := os.MkdirAll(pkg.Build, 0755); err != nil {
		return lock, err
	}
	candidate, err := os.MkdirTemp(pkg.Build, ".inputs-")
	if err != nil {
		return lock, err
	}
	defer os.RemoveAll(candidate)
	if err := copyFile(path, filepath.Join(candidate, "upstream.yaml")); err != nil {
		return lock, err
	}
	sourceDirectory := strings.TrimSuffix(path, ".yaml")
	if _, err := os.Stat(sourceDirectory); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Join(candidate, "upstream"), 0755); err != nil {
			return lock, err
		}
	} else if err != nil {
		return lock, err
	} else if err := copyTree(sourceDirectory, filepath.Join(candidate, "upstream")); err != nil {
		return lock, err
	}
	if err := copyTree(filepath.Join(checkout, "pipelines"), filepath.Join(candidate, "pipelines")); err != nil {
		return lock, err
	}
	if err := copyFile(filepath.Join(checkout, "wolfi-signing.rsa.pub"), filepath.Join(candidate, "wolfi-signing.rsa.pub")); err != nil {
		return lock, err
	}
	lock = Inputs{Lock: overlay.Lock{Recipe: filepath.Base(path), Version: overlay.Value(overlay.Field(config, "package"), "version"), Epoch: epoch, Revision: pkg.Policy.Revision, SourceCommit: source}, Package: pkg.Name, Repository: "https://github.com/wolfi-dev/os", WolfiCommit: commit, SourceDateEpoch: timestamp, Release: &release, SourceKind: "wolfi"}
	lock.Wolfi = &Baseline{Version: lock.Version, Epoch: lock.Epoch, SourceCommit: lock.SourceCommit}
	if release.Version != lock.Version {
		lock.Version = release.Version
		lock.Epoch = 0
		lock.SourceCommit = release.Commit
		lock.SourceDateEpoch = release.Timestamp
		lock.SourceKind = "upstream-release"
	}
	recipe, err := effective(pkg, config, lock)
	if err != nil {
		return lock, err
	}
	lock.EffectiveRecipe = overlay.Value(overlay.Field(recipe, "package"), "name") + ".yaml"
	vars, err := overlay.Variables(recipe, lock.Lock, pkg.Policy.MaxFailPerMille, sourcePath(pkg.Root, "Werewolf/pipelines"))
	if err != nil {
		return lock, err
	}
	if _, err := overlay.Derive(pkg.Spec, recipe, lock.Lock, pkg.Directory, vars); err != nil {
		return lock, fmt.Errorf("effective recipe: %w", err)
	}
	if err := copyUpstreamPatches(pkg, filepath.Join(candidate, "upstream"), ""); err != nil {
		return lock, err
	}
	lock.SHA256, lock.Mode, err = hashFiles(candidate, func(string) bool { return false })
	if err != nil {
		return lock, err
	}
	lock.PolicySHA256, err = policyHashes(pkg)
	if err != nil {
		return lock, err
	}
	lock.BuildID, err = identity(lock)
	if err != nil {
		return lock, err
	}
	if err := writeJSON(filepath.Join(candidate, "upstream.lock.json"), lock); err != nil {
		return lock, err
	}
	if !install {
		return lock, nil
	}
	pkg.Build = filepath.Join(packageBuildRoot(pkg), lock.BuildID)
	if err := os.MkdirAll(pkg.Build, 0755); err != nil {
		return lock, err
	}
	destination := filepath.Join(pkg.Build, "inputs")
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if err := os.Rename(candidate, destination); err != nil {
			return lock, err
		}
	} else if err != nil {
		return lock, err
	}
	if _, err := VerifyInputs(pkg); err != nil {
		return lock, err
	}
	if err := selectCaptured(pkg, lock.BuildID); err != nil {
		return lock, err
	}
	*target = pkg
	slog.Info("captured package inputs", "package", pkg.Name, "version", lock.Version, "wolfi_version", lock.Wolfi.Version, "build_id", lock.BuildID)
	return lock, nil
}

// VerifyInputs authenticates the exact set of captured files and local policy.
func VerifyInputs(pkg Package) (Inputs, error) {
	var lock Inputs
	directory := filepath.Join(pkg.Build, "inputs")
	data, err := os.ReadFile(filepath.Join(directory, "upstream.lock.json"))
	if err != nil {
		return lock, err
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return lock, err
	}
	if lock.Package != pkg.Name || !exactCommit.MatchString(lock.WolfiCommit) || !exactCommit.MatchString(lock.SourceCommit) {
		return lock, fmt.Errorf("invalid locked package identity")
	}
	hashes, modes, err := hashFiles(directory, func(path string) bool { return path == "upstream.lock.json" })
	if err != nil {
		return lock, err
	}
	if !reflect.DeepEqual(hashes, lock.SHA256) || !reflect.DeepEqual(modes, lock.Mode) {
		return lock, fmt.Errorf("captured input contents or modes changed")
	}
	policy, err := policyHashes(pkg)
	if err != nil {
		return lock, err
	}
	if !reflect.DeepEqual(policy, lock.PolicySHA256) {
		return lock, fmt.Errorf("downstream policy changed; refresh inputs")
	}
	id, err := identity(lock)
	if err != nil {
		return lock, err
	}
	if id != lock.BuildID {
		return lock, fmt.Errorf("build identity mismatch")
	}
	if lock.Release != nil {
		baseline, err := readRecipe(filepath.Join(directory, "upstream.yaml"))
		if err != nil {
			return lock, err
		}
		pin, err := overlay.SourceCommit(baseline)
		if err != nil {
			return lock, err
		}
		epoch, err := strconv.Atoi(overlay.Value(overlay.Field(baseline, "package"), "epoch"))
		if err != nil {
			return lock, err
		}
		facts := Baseline{Version: overlay.Value(overlay.Field(baseline, "package"), "version"), Epoch: epoch, SourceCommit: pin}
		if lock.Wolfi == nil || facts != *lock.Wolfi {
			return lock, fmt.Errorf("Wolfi baseline differs from captured recipe")
		}
		expectedVersion, expectedCommit := facts.Version, facts.SourceCommit
		switch lock.SourceKind {
		case "upstream-release":
			expectedVersion, expectedCommit = lock.Release.Version, lock.Release.Commit
		case "wolfi":
		default:
			return lock, fmt.Errorf("unknown effective source kind")
		}
		if lock.Version != expectedVersion || lock.SourceCommit != expectedCommit || lock.Version != lock.Release.Version {
			return lock, fmt.Errorf("effective source differs from selected release")
		}
	}
	return lock, nil
}

// Derive writes a context only after checking its inputs, patch series and edits.
func Derive(pkg Package) (*yaml.Node, error) {
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return nil, err
	}
	if err := verifySeries(pkg); err != nil {
		return nil, err
	}
	baseline, err := readRecipe(filepath.Join(pkg.Build, "inputs/upstream.yaml"))
	if err != nil {
		return nil, err
	}
	effective, err := effective(pkg, baseline, lock)
	if err != nil {
		return nil, err
	}
	vars, err := overlay.Variables(effective, lock.Lock, pkg.Policy.MaxFailPerMille, sourcePath(pkg.Root, "Werewolf/pipelines"))
	if err != nil {
		return nil, err
	}
	recipe, err := overlay.Derive(pkg.Spec, effective, lock.Lock, pkg.Directory, vars)
	if err != nil {
		return nil, err
	}
	context := filepath.Join(pkg.Build, "context")
	if err := os.RemoveAll(context); err != nil {
		return nil, err
	}
	if err := copyTree(filepath.Join(pkg.Build, "inputs/upstream"), context); err != nil {
		return nil, err
	}
	if err := copyUpstreamPatches(pkg, filepath.Join(pkg.Build, "inputs/upstream"), context); err != nil {
		return nil, err
	}
	for source, target := range pkg.Spec.Context {
		if !filepath.IsLocal(source) || !filepath.IsLocal(target) {
			return nil, fmt.Errorf("unsafe context copy")
		}
		if err := copyTree(filepath.Join(pkg.Directory, source), filepath.Join(context, target)); err != nil {
			return nil, err
		}
	}
	if err := overlay.EditFiles(pkg.Spec.ContextEdits, context, pkg.Directory, vars); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(recipe)
	if err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(pkg.Build, pkg.Name+".yaml"), data, 0644); err != nil {
		return nil, err
	}
	return recipe, nil
}

func verifySeries(pkg Package) error {
	data, err := os.ReadFile(filepath.Join(pkg.Directory, "patches/series"))
	if os.IsNotExist(err) && pkg.Spec.Patches == "" {
		return nil
	}
	if err != nil {
		return err
	}
	names := strings.Fields(string(data))
	paths, err := filepath.Glob(filepath.Join(pkg.Directory, "patches/*.patch"))
	if err != nil {
		return err
	}
	if len(paths) != len(names) {
		return fmt.Errorf("patches/series differs from patches directory")
	}
	for i, name := range names {
		if !regexp.MustCompile(fmt.Sprintf(`^%04d-[a-z0-9-]+\.patch$`, i+1)).MatchString(name) {
			return fmt.Errorf("invalid patch sequence: %s", name)
		}
		data, err := os.ReadFile(filepath.Join(pkg.Directory, "patches", name))
		if err != nil {
			return err
		}
		header := strings.SplitN(string(data), "\ndiff --git", 2)[0]
		for _, field := range []string{"Subject:", "Upstream-Status:", "Reference:"} {
			if !strings.Contains(header, field) {
				return fmt.Errorf("%s has no %s", name, field)
			}
		}
	}
	if (len(names) > 0) != (pkg.Spec.Patches != "") {
		return fmt.Errorf("patch series differs from overlay patch policy")
	}
	return nil
}

// Prepare checks out the exact source and applies patches in build order.
// It refuses to reset an existing source checkout.
func Prepare(ctx context.Context, pkg Package, destination, localSource string) error {
	if err := ensureWorkRoot(pkg.Root); err != nil {
		return err
	}
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	if err := verifySeries(pkg); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("use a new source destination: %s", destination)
	}
	baseline, err := readRecipe(filepath.Join(pkg.Build, "inputs/upstream.yaml"))
	if err != nil {
		return err
	}
	recipe, err := effective(pkg, baseline, lock)
	if err != nil {
		return err
	}
	vars, err := overlay.Variables(recipe, lock.Lock, pkg.Policy.MaxFailPerMille, sourcePath(pkg.Root, "Werewolf/pipelines"))
	if err != nil {
		return err
	}
	sources := []string{}
	if lock.Release != nil {
		address := lock.Release.Repository
		if !strings.HasPrefix(address, "https://") {
			address = "https://github.com/" + address + ".git"
		}
		sources = append(sources, address)
	}
	for _, source := range pkg.Spec.Sources {
		n := overlay.String(source)
		if err := overlay.Expand(n, pkg.Directory, vars); err != nil {
			return err
		}
		sources = append(sources, n.Value)
	}
	if localSource != "" {
		info, err := os.Stat(localSource)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("local source must be a Git repository directory")
		}
		localSource, err = filepath.Abs(localSource)
		if err != nil {
			return err
		}
		sources = []string{localSource}
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	if _, err := output(ctx, destination, "git", "init", "-q"); err != nil {
		return err
	}
	fetched := false
attempts:
	for _, delay := range []time.Duration{0, 15 * time.Second, 45 * time.Second} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		for _, source := range sources {
			slog.Info("fetching locked source", "package", pkg.Name, "commit", lock.SourceCommit, "source", source)
			_, err := output(ctx, destination, "git", "-c", "core.hooksPath=/dev/null", "-c", "fetch.fsckObjects=true", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=120", "fetch", "--no-tags", "--depth=1", "--", source, lock.SourceCommit)
			if err != nil {
				slog.Warn("source fetch failed", "package", pkg.Name, "source", source, "error", err)
				continue
			}
			pin, err := output(ctx, destination, "git", "rev-parse", "FETCH_HEAD")
			if err != nil {
				return err
			}
			if pin != lock.SourceCommit {
				return fmt.Errorf("fetched commit differs from lock")
			}
			if _, err := output(ctx, destination, "git", "-c", "core.hooksPath=/dev/null", "checkout", "-q", "--detach", lock.SourceCommit); err != nil {
				return err
			}
			fetched = true
			break attempts
		}
	}
	if !fetched {
		return fmt.Errorf("all source mirrors failed")
	}
	pipeline := overlay.Field(recipe, "pipeline")
	for _, step := range pipeline.Content {
		if overlay.Value(step, "uses") != "patch" {
			continue
		}
		for _, name := range strings.Fields(overlay.Value(overlay.Field(step, "with"), "patches")) {
			if pkg.Spec.UpstreamPatchPrefix != "" {
				if overlay.Value(step, "working-directory") != pkg.Spec.PatchWorkingDirectory || !strings.HasPrefix(name, pkg.Spec.UpstreamPatchPrefix) {
					return fmt.Errorf("review changed upstream patch path or directory")
				}
				name = strings.TrimPrefix(name, pkg.Spec.UpstreamPatchPrefix)
			}
			if !filepath.IsLocal(name) {
				return fmt.Errorf("unsafe upstream patch: %s", name)
			}
			path, err := upstreamPatch(pkg, filepath.Join(pkg.Build, "inputs/upstream"), name)
			if err != nil {
				return err
			}
			if err := applyPatch(ctx, pkg, destination, path, "Wolfi's "+name); err != nil {
				return err
			}
		}
	}
	series, err := os.ReadFile(filepath.Join(pkg.Directory, "patches/series"))
	if err != nil && !(os.IsNotExist(err) && pkg.Spec.Patches == "") {
		return err
	}
	for _, name := range strings.Fields(string(series)) {
		if err := applyPatch(ctx, pkg, destination, filepath.Join(pkg.Directory, "patches", name), "patches/"+name); err != nil {
			return err
		}
	}
	for source, target := range pkg.Spec.Prepared {
		if !filepath.IsLocal(source) || !filepath.IsLocal(target) {
			return fmt.Errorf("unsafe prepared file")
		}
		if err := copyFile(filepath.Join(pkg.Build, "inputs/upstream", source), filepath.Join(destination, target)); err != nil {
			return err
		}
	}
	return overlay.EditFiles(pkg.Spec.PreparedEdits, destination, pkg.Directory, vars)
}

func applyPatch(ctx context.Context, pkg Package, destination, path, label string) error {
	args := []string{"-p1", "--forward", "--batch", "--fuzz=2", "--no-backup-if-mismatch", "-d", destination, "-i", path}
	log, err := output(ctx, pkg.Root, "patch", append([]string{"--dry-run", "--verbose"}, args...)...)
	if err != nil {
		return fmt.Errorf("prepare: %s does not apply\n%w", label, err)
	}
	if err := Run(ctx, pkg.Root, "patch", args...); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(log), "with fuzz") {
		slog.Warn("patch applied with fuzz", "package", pkg.Name, "patch", label, "output", log)
	} else {
		slog.Info("patch applied", "package", pkg.Name, "patch", label)
	}
	return nil
}
