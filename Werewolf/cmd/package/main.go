// Command package maintains the categorized ports tree using shared Go code
// and checked YAML overlays.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/werewolf-linux/packages/Werewolf/internal/catalog"
	"github.com/werewolf-linux/packages/Werewolf/internal/packaging"
	"github.com/werewolf-linux/packages/Werewolf/internal/plex"
	"github.com/werewolf-linux/packages/Werewolf/internal/probe"
	"github.com/werewolf-linux/packages/Werewolf/internal/smoke"
	"github.com/werewolf-linux/packages/Werewolf/internal/update"
	"gopkg.in/yaml.v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "runner-probe" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("package command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: package COMMAND [flags]\ncommands: new, schema, render, paths, test, checks, catalog, check, plan, release-plan, updates, refresh, discover, derive, verify, prepare, build, fixture, install-melange, setup-runner, signing-key, provenance, stage, publish, feed, status, recover, probe")
	}
	command := args[0]
	if command == "probe" {
		return probe.Command(ctx, args[1:])
	}
	if command == "runner-probe" {
		return packaging.RunnerProbe(ctx)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	base := flags.String("base", "", "base commit for changed-package CI selection")
	head := flags.String("head", "", "head commit; empty includes working tree changes")
	repo := flags.String("repo", "", "release repository OWNER/PROJECT for a new package")
	fromWolfi := flags.String("from-wolfi", "", "reviewed local Wolfi recipe for a new package")
	report := flags.String("report", "", "save the machine-readable report")
	runner := flags.String("runner", "", "Melange test runner")
	root := flags.String("root", ".", "repository root")
	selected := flags.String("package", "", "one package; empty selects all applicable packages")
	category := flags.String("category", "", "select a package category")
	buildTests := flags.Bool("build", false, "build missing APKs before running Melange tests")
	engine := flags.String("engine", "", "filter catalog entries by upstream or wolfi")
	arch := flags.String("arch", packaging.Architecture(), "target APK architecture")
	wolfi := flags.String("wolfi", "", "clean local Wolfi checkout")
	source := flags.String("source", "", "local Git source for offline patch validation")
	destination := flags.String("dest", "", "output file or new source directory")
	fixture := flags.String("fixture", "", "offline Wolfi recipe baseline")
	jsonOutput := flags.Bool("json", false, "write machine-readable catalog output")
	production := flags.Bool("production", false, "require the production signing credential")
	testOnly := flags.Bool("test-only", false, "test existing signed APKs")
	offline := flags.Bool("offline", false, "plan builds without contacting the publication API")
	artifacts := flags.String("artifacts", "Werewolf/.work/artifacts", "downloaded native build artifacts")
	assets := flags.String("assets", "", "staged Wolfi release assets")
	github := flags.Bool("github", false, "report discovery drift and read release state on GitHub")
	runID := flags.String("run", "", "original native build workflow run for recovery")
	sourceSHA := flags.String("source-sha", "", "validated historical source commit")
	werewolf := flags.String("werewolf", "", "sibling Werewolf checkout for local application builds")
	write := flags.Bool("write", false, "write complete application source updates")
	fresh := flags.Bool("fresh", false, "replace the generated source checkout before patch preparation")
	expectDenied := flags.Bool("expect-denied", false, "require the chroot probe to fail with EPERM")
	archive := flags.String("archive", "", "captured input archive to restore")
	expectedVersion := flags.String("expected-version", "", "expected signed Plex vendor release")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if command == "chroot-probe" {
		err := syscall.Chroot("/")
		if *expectDenied {
			if !errors.Is(err, syscall.EPERM) {
				return fmt.Errorf("expected chroot EPERM, got %v", err)
			}
			return nil
		}
		return err
	}
	absolute, err := catalog.RepositoryRoot(*root)
	if err != nil {
		return err
	}
	switch command {
	case "schema":
		data, err := catalog.Schema()
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *destination != "" {
			if *offline {
				previous, err := os.ReadFile(*destination)
				if err != nil {
					return err
				}
				if !bytes.Equal(previous, data) {
					return errors.New("schema is stale; run package schema --dest Werewolf/package.schema.json")
				}
				return nil
			}
			return os.WriteFile(*destination, data, 0644)
		}
		_, err = os.Stdout.Write(data)
		return err
	case "new":
		if flags.NArg() != 1 {
			return errors.New("new needs CATEGORY/NAME and --repo OWNER/PROJECT")
		}
		path, err := catalog.New(absolute, flags.Arg(0), *repo, *fromWolfi)
		if err == nil {
			fmt.Println(path)
		}
		return err
	case "plex-update":
		return plex.Refresh(ctx, absolute, *expectedVersion)
	case "plex-verify":
		_, err := plex.Verify(ctx, absolute)
		return err
	case "install-melange":
		if *destination == "" {
			*destination = filepath.Join(absolute, "Werewolf/.work/tools/melange")
		}
		return packaging.InstallMelange(ctx, absolute, *destination)
	case "setup-runner":
		return packaging.SetupRunner(ctx, absolute)
	case "signing-key":
		directory := absolute
		path, err := packaging.SigningKey(directory, *production)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	packages, err := packaging.Load(absolute)
	if err != nil {
		return err
	}
	if *engine != "" && *engine != "wolfi" && *engine != "upstream" {
		return errors.New("engine must be upstream or wolfi")
	}
	var chosen []packaging.Package
	for _, pkg := range packages {
		if (*selected == "" || pkg.Name == *selected) && (*engine == "" || pkg.Engine == *engine) && (*category == "" || strings.Split(pkg.Path, "/")[0] == *category) {
			chosen = append(chosen, pkg)
		}
	}
	if len(chosen) == 0 {
		return fmt.Errorf("no matching packages (package=%q, category=%q, engine=%q)", *selected, *category, *engine)
	}
	if command == "catalog" {
		action := "validate"
		if flags.NArg() > 0 {
			action = flags.Arg(0)
		}
		switch action {
		case "validate":
			fmt.Printf("%d package maintenance contracts validated\n", len(packages))
		case "list":
			var names []string
			for _, pkg := range chosen {
				names = append(names, pkg.Name)
			}
			if *jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(names)
			}
			for _, name := range names {
				fmt.Println(name)
			}
		case "path", "show":
			if flags.NArg() != 2 {
				return errors.New("catalog path/show needs a package name")
			}
			for _, pkg := range chosen {
				if pkg.Name == flags.Arg(1) {
					if action == "path" {
						fmt.Println(pkg.Path)
						return nil
					}
					data, err := os.ReadFile(filepath.Join(pkg.Directory, "package.yaml"))
					if err != nil {
						return err
					}
					if *jsonOutput {
						var value map[string]any
						if err := yaml.Unmarshal(data, &value); err != nil {
							return err
						}
						return json.NewEncoder(os.Stdout).Encode(value)
					}
					_, err = os.Stdout.Write(data)
					return err
				}
			}
			return fmt.Errorf("unknown package: %s", flags.Arg(1))
		default:
			return fmt.Errorf("unknown catalog command: %s", action)
		}
		return nil
	}
	if command == "checks" {
		return packaging.Checks(ctx, chosen)
	}
	if command == "paths" {
		if *selected == "" {
			return errors.New("paths requires --package")
		}
		paths := packaging.Paths(chosen[0], *arch)
		if *github {
			values := map[string]any{}
			for k, v := range paths {
				values[k] = v
			}
			if err := packaging.ActionsOutput(values); err != nil {
				return err
			}
		}
		return json.NewEncoder(os.Stdout).Encode(paths)
	}
	if command == "render" {
		if *selected == "" {
			return errors.New("render requires --package")
		}
		data, err := packaging.Render(chosen[0])
		if err != nil {
			return err
		}
		if *destination != "" {
			return os.WriteFile(*destination, data, 0644)
		}
		_, err = os.Stdout.Write(data)
		return err
	}
	if command == "test" {
		if flags.NArg() != 0 {
			return errors.New("test accepts --package or --category, without positional arguments")
		}
		if *fresh && !*buildTests {
			return errors.New("test --fresh requires --build")
		}
		if len(chosen) > 1 || *buildTests || *report != "" || *jsonOutput {
			results, testErr := packaging.TestPackages(ctx, chosen, packaging.TestOptions{Arch: *arch, Runner: *runner, Build: *buildTests, Fresh: *fresh, Report: *report})
			if *jsonOutput {
				if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
					return err
				}
			} else {
				passed := 0
				for _, result := range results {
					if result.Status == "passed" {
						passed++
					}
				}
				fmt.Printf("%d/%d package suites passed (%s)\n", passed, len(chosen), *arch)
			}
			return testErr
		}
		if chosen[0].Engine == "wolfi" {
			return packaging.BuildWolfi(ctx, chosen[0], *arch, true, *runner)
		}
		old, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := os.Chdir(absolute); err != nil {
			return err
		}
		defer os.Chdir(old)
		return smoke.Check(ctx, chosen[0].Name, *arch, *runner)
	}
	if command == "plan" {
		result, err := packaging.PlanCI(ctx, chosen, *base, *head)
		if err != nil {
			return err
		}
		if err := packaging.SaveReport(*report, result); err != nil {
			return err
		}
		if *github {
			if err := packaging.ActionsOutput(map[string]any{"matrix": map[string]any{"include": result.Include}, "packages": result.Packages, "wolfi": result.Wolfi, "has_build": len(result.Include) > 0}); err != nil {
				return err
			}
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if command == "release-plan" {
		result, err := packaging.Plan(ctx, chosen, *offline)
		if writeErr := json.NewEncoder(os.Stdout).Encode(result); writeErr != nil {
			return writeErr
		}
		return err
	}
	if command == "feed" {
		return packaging.Feed(ctx, chosen)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional argument; select a package with --package")
	}
	if slices.Contains([]string{"build", "verify", "derive", "prepare", "fixture", "provenance", "stage", "publish", "status", "recover", "verify-tag", "artifact-verify"}, command) && *selected == "" {
		return fmt.Errorf("%s requires --package", command)
	}
	if command == "check" {
		if _, err := update.LoadPolicies(absolute); err != nil {
			return err
		}
		owners := map[string]string{}
		for _, pkg := range chosen {
			if pkg.Engine == "wolfi" {
				if err := packaging.CheckFixtures(pkg); err != nil {
					return err
				}
			}
			if pkg.Engine == "upstream" {
				meta, err := packaging.Metadata(ctx, pkg)
				if err != nil {
					return err
				}
				for _, name := range meta.Names {
					if owner := owners[name]; owner != "" {
						return fmt.Errorf("APK name %s is shared by %s and %s", name, owner, pkg.Name)
					}
					owners[name] = pkg.Name
				}
			}
		}
		return nil
	}
	needsCheckout := false
	if command == "refresh" || command == "discover" || command == "updates" {
		for _, pkg := range chosen {
			needsCheckout = needsCheckout || pkg.Engine == "wolfi"
		}
	}
	if needsCheckout && *wolfi == "" {
		directory, err := os.MkdirTemp("", "packages-wolfi-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(directory)
		*wolfi = filepath.Join(directory, "os")
		if err := packaging.Run(ctx, absolute, "git", "clone", "--depth=1", "https://github.com/wolfi-dev/os.git", *wolfi); err != nil {
			return err
		}
	}
	if command == "discover" {
		result, err := packaging.Discover(ctx, chosen, *wolfi, *github)
		if writeErr := json.NewEncoder(os.Stdout).Encode(result); writeErr != nil {
			return writeErr
		}
		return err
	}
	if command == "updates" {
		results, updateErr := packaging.Updates(ctx, chosen, *wolfi, *write)
		if err := packaging.SaveReport(*report, results); err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
			return err
		}
		return updateErr
	}
	var failed []error
	for _, pkg := range chosen {
		var err error
		if pkg.Engine != "wolfi" && command != "build" && command != "publish" && command != "updates" && command != "artifact-verify" {
			if *selected != "" {
				return fmt.Errorf("%s applies to Wolfi packages", command)
			}
			continue
		}
		switch command {
		case "refresh":
			_, err = packaging.Capture(ctx, &pkg, *wolfi)
		case "derive", "verify":
			_, err = packaging.Derive(pkg)
		case "prepare":
			target := *destination
			if target == "" {
				target = filepath.Join(pkg.Build, "source")
				if *fresh {
					if err := os.RemoveAll(target); err != nil {
						return err
					}
				}
			} else if *fresh {
				return errors.New("--fresh only replaces the generated source directory")
			}
			err = packaging.Prepare(ctx, pkg, target, *source)
		case "build":
			if pkg.Engine == "wolfi" {
				err = packaging.BuildWolfi(ctx, pkg, *arch, *testOnly, *runner)
			} else {
				if *werewolf != "" {
					err = packaging.BuildLocalApplication(ctx, pkg, *arch, *werewolf)
				} else {
					err = packaging.BuildApplication(ctx, pkg, *arch)
				}
			}
		case "artifact-verify":
			if pkg.Engine != "upstream" {
				return errors.New("artifact-verify applies to application builds")
			}
			var meta packaging.AppMetadata
			meta, err = packaging.Metadata(ctx, pkg)
			if err == nil {
				directory := *destination
				if directory == "" {
					directory = packaging.Paths(pkg, *arch)["artifacts"]
				}
				_, err = packaging.ValidateApplication(directory, meta, *arch, os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_RUN_ID"))
			}
		case "fixture":
			if *fixture == "" {
				return errors.New("fixture requires --fixture PATH")
			}
			var data []byte
			data, err = packaging.Fixture(pkg, *fixture)
			if err == nil {
				_, err = os.Stdout.Write(data)
			}
		case "provenance":
			err = packaging.Provenance(ctx, pkg, *arch)
		case "restore-inputs":
			if *archive == "" {
				return errors.New("restore-inputs requires --archive")
			}
			err = packaging.RestoreInputs(pkg, *archive)
		case "recover":
			err = packaging.Recover(ctx, pkg, *runID)
		case "verify-tag":
			err = packaging.VerifyTag(ctx, pkg, *sourceSHA)
		case "stage":
			directory := *assets
			if directory == "" {
				directory = filepath.Join(pkg.Build, "release")
			}
			err = packaging.StageWolfi(ctx, pkg, *artifacts, directory)
		case "status":
			var lock packaging.Inputs
			lock, err = packaging.VerifyInputs(pkg)
			if err == nil {
				var state string
				state, _, err = packaging.ReleaseStatus(ctx, pkg.Name+"/build-"+lock.BuildID)
				if err == nil {
					fmt.Println(state)
					err = packaging.ActionsOutput(map[string]any{"state": state, "build_id": lock.BuildID})
				}
			}
		case "publish":
			if pkg.Engine == "upstream" {
				err = packaging.PublishApplication(ctx, pkg, *artifacts)
			} else {
				directory := *assets
				if directory == "" {
					directory = filepath.Join(pkg.Build, "release")
				}
				err = packaging.PublishWolfi(ctx, pkg, directory)
			}
		default:
			return fmt.Errorf("unknown package command: %s", command)
		}
		if err != nil {
			slog.Error("package operation failed", "package", pkg.Name, "command", command, "error", err)
			failed = append(failed, fmt.Errorf("%s: %w", pkg.Name, err))
		} else {
			slog.Info("package operation completed", "package", pkg.Name, "command", command)
		}
	}
	return errors.Join(failed...)
}
