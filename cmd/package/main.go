// Command package maintains the categorized ports tree using shared Go code
// and checked YAML overlays.
package main

import (
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
	"syscall"

	"github.com/werewolf-linux/packages/internal/packaging"
	"github.com/werewolf-linux/packages/internal/plex"
	"github.com/werewolf-linux/packages/internal/probe"
	"github.com/werewolf-linux/packages/internal/update"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("package command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: package COMMAND [flags]\ncommands: catalog, check, plan, updates, refresh, discover, derive, verify, prepare, build, fixture, install-melange, setup-runner, signing-key, provenance, stage, publish, feed, status, recover, probe")
	}
	command := args[0]
	if command == "probe" {
		return probe.Command(ctx, args[1:])
	}
	if command == "runner-probe" {
		return packaging.RunnerProbe()
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	selected := flags.String("package", "", "one package; empty selects all applicable packages")
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
	artifacts := flags.String("artifacts", "artifacts", "downloaded native build artifacts")
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
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if filepath.Base(absolute) == "wolfi" {
		absolute = filepath.Dir(absolute)
	}
	switch command {
	case "plex-update":
		return plex.Refresh(ctx, absolute, *expectedVersion)
	case "plex-verify":
		_, err := plex.Verify(ctx, absolute)
		return err
	case "install-melange":
		if *destination == "" {
			*destination = filepath.Join(absolute, "build/tools/melange")
		}
		return packaging.InstallMelange(ctx, absolute, *destination)
	case "setup-runner":
		return packaging.SetupRunner(ctx, absolute)
	case "signing-key":
		directory := absolute
		if *engine == "wolfi" {
			directory = filepath.Join(absolute, "wolfi")
		}
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
		if (*selected == "" || pkg.Name == *selected) && (*engine == "" || pkg.Engine == *engine) {
			chosen = append(chosen, pkg)
		}
	}
	if len(chosen) == 0 {
		return fmt.Errorf("no matching package: %s", *selected)
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
					return json.NewEncoder(os.Stdout).Encode(pkg.Package)
				}
			}
			return fmt.Errorf("unknown package: %s", flags.Arg(1))
		default:
			return fmt.Errorf("unknown catalog command: %s", action)
		}
		return nil
	}
	if command == "plan" {
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
	if slices.Contains([]string{"build", "verify", "derive", "prepare"}, command) && !*testOnly {
		for _, pkg := range chosen {
			if pkg.Engine != "wolfi" {
				continue
			}
			if _, err := os.Stat(filepath.Join(pkg.Build, "inputs/upstream.lock.json")); os.IsNotExist(err) {
				needsCheckout = true
			}
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
	var failed []error
	for _, pkg := range chosen {
		var err error
		if pkg.Engine != "wolfi" && command != "build" && command != "publish" && command != "updates" && command != "artifact-verify" {
			if *selected != "" {
				return fmt.Errorf("%s applies to Wolfi packages", command)
			}
			continue
		}
		if pkg.Engine == "wolfi" && slices.Contains([]string{"build", "verify", "derive", "prepare"}, command) && !*testOnly {
			if _, statErr := os.Stat(filepath.Join(pkg.Build, "inputs/upstream.lock.json")); os.IsNotExist(statErr) {
				_, err = packaging.Capture(ctx, pkg, *wolfi)
				if err != nil {
					failed = append(failed, fmt.Errorf("%s: %w", pkg.Name, err))
					continue
				}
			}
		}
		switch command {
		case "refresh":
			_, err = packaging.Capture(ctx, pkg, *wolfi)
		case "updates":
			if pkg.Engine == "wolfi" {
				_, err = packaging.Capture(ctx, pkg, *wolfi)
			} else {
				result := update.NewClient(os.Getenv("GH_TOKEN")).Update(ctx, absolute, pkg.Name, pkg.Update, *write, update.RunCommand)
				if result.Status == "failed" {
					err = errors.New(result.Error)
				}
				if writeErr := json.NewEncoder(os.Stdout).Encode(result); writeErr != nil {
					return writeErr
				}
			}
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
				err = packaging.BuildWolfi(ctx, pkg, *arch, *testOnly)
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
					directory = filepath.Join(absolute, "build/artifacts", pkg.Name, *arch)
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
