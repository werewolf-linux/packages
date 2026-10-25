package packaging

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/werewolf-linux/packages/internal/apk"
	"github.com/werewolf-linux/packages/internal/update"
)

const publicApplications = "https://dist.werewolf-linux.org/apk/packages/"

// AppManifest is a signed recovery point covering both native architectures.
type AppManifest struct {
	AppMetadata
	Format        string                       `json:"format"`
	Commit        string                       `json:"commit"`
	RunNumber     int64                        `json:"run_number"`
	RunID         string                       `json:"run_id"`
	Architectures map[string]map[string]string `json:"architectures"`
}

// PlanResult is the build and publication matrix consumed by GitHub Actions.
type PlanResult struct {
	Build struct {
		Include []map[string]string `json:"include"`
	} `json:"build"`
	Publish  []string `json:"publish"`
	Releases struct {
		Include []map[string]any `json:"include"`
	} `json:"releases"`
	Failed []string `json:"failed"`
}

func download(ctx context.Context, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "werewolf-packages (+https://github.com/werewolf-linux/packages)")
	response, err := update.NewClient("").HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read %s: %s", address, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("oversized metadata: %s", address)
	}
	return data, nil
}

func trustKey(root string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(filepath.Join(root, "release/packages.pub"))
	if err != nil {
		return nil, err
	}
	return apk.PublicKey(data)
}

func sameMetadata(a, b AppMetadata) bool {
	return a.Context == b.Context && a.Inputs == b.Inputs && a.Version == b.Version && slices.Equal(a.Names, b.Names)
}

// AppState preserves a completed release, resumes a signed snapshot, or asks
// for a build. A remote read error never masquerades as a missing publication.
func AppState(ctx context.Context, meta AppMetadata, trust *rsa.PublicKey) (string, error) {
	data, err := download(ctx, publicApplications+"contexts/"+meta.Context+"/current.json")
	if err != nil {
		return "", err
	}
	if data != nil {
		var previous AppManifest
		if err := OpenEnvelope(data, trust, &previous); err != nil {
			return "", err
		}
		if previous.Format != "werewolf-package-manifest/1" || previous.Context != meta.Context {
			return "", fmt.Errorf("release pointer names another context")
		}
		if previous.Inputs == meta.Inputs {
			if !sameMetadata(previous.AppMetadata, meta) {
				return "", fmt.Errorf("release pointer differs from recipe")
			}
			return "complete", nil
		}
		if previous.Version == meta.Version {
			return "", fmt.Errorf("published version has different inputs; increment package epoch: %s", meta.Context)
		}
	}
	data, err = download(ctx, publicApplications+"builds/"+meta.Context+"/"+meta.Inputs+"/release.json")
	if err != nil {
		return "", err
	}
	if data != nil {
		var staged AppManifest
		if err := OpenEnvelope(data, trust, &staged); err != nil {
			return "", err
		}
		if !sameMetadata(staged.AppMetadata, meta) {
			return "", fmt.Errorf("staged release differs from recipe")
		}
		return "resume", nil
	}
	return "build", nil
}

// Plan selects packages independently, retaining successful work when another
// package's source, recipe, or remote release metadata is unavailable.
func Plan(ctx context.Context, packages []Package, offline bool) (PlanResult, error) {
	var result PlanResult
	result.Build.Include = []map[string]string{}
	result.Publish = []string{}
	result.Releases.Include = []map[string]any{}
	result.Failed = []string{}
	var trust *rsa.PublicKey
	if !offline && len(packages) > 0 {
		var err error
		trust, err = trustKey(packages[0].Root)
		if err != nil {
			return result, err
		}
	}
	for _, pkg := range packages {
		if pkg.Engine != "upstream" {
			continue
		}
		meta, err := Metadata(ctx, pkg)
		state := "build"
		if err == nil && !offline {
			state, err = AppState(ctx, meta, trust)
		}
		if err != nil {
			slog.Error("cannot plan package", "package", pkg.Name, "error", err)
			result.Failed = append(result.Failed, pkg.Name)
			continue
		}
		slog.Info("selected package state", "package", pkg.Name, "state", state, "version", meta.Version, "inputs", meta.Inputs)
		if state == "build" {
			for _, arch := range Arches {
				result.Build.Include = append(result.Build.Include, map[string]string{"package": pkg.Name, "arch": arch})
			}
		}
		if state != "complete" {
			result.Publish = append(result.Publish, pkg.Name)
			result.Releases.Include = append(result.Releases.Include, map[string]any{"package": pkg.Name, "build": state == "build"})
		}
	}
	if err := ActionsOutput(map[string]any{"build": result.Build, "publish": result.Publish, "releases": result.Releases, "failed": result.Failed, "has_build": len(result.Build.Include) > 0}); err != nil {
		return result, err
	}
	if offline && len(result.Failed) > 0 {
		return result, fmt.Errorf("offline recipe validation failed: %v", result.Failed)
	}
	return result, nil
}

// ActionsOutput appends single-line JSON values to the Actions output protocol.
func ActionsOutput(values map[string]any) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer output.Close()
	for key, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if text, ok := value.(string); ok {
			if strings.ContainsAny(text, "\r\n") {
				return fmt.Errorf("Actions output must fit one line")
			}
			data = []byte(text)
		}
		if _, err := fmt.Fprintf(output, "%s=%s\n", key, data); err != nil {
			return err
		}
	}
	return nil
}

// ValidateApplication rejects mixed builds, extra files, symlinks and payload
// corruption before a publisher receives a reason to sign any artifact.
func ValidateApplication(directory string, meta AppMetadata, arch, commit, runID string) (AppBuild, error) {
	var record AppBuild
	data, err := os.ReadFile(filepath.Join(directory, "build.json"))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.Format != "werewolf-package-build/1" || record.Arch != arch || !sameMetadata(record.AppMetadata, meta) || commit == "" || runID == "" || record.Commit != commit || record.RunID != runID {
		return record, fmt.Errorf("artifact identity or native build provenance differs")
	}
	hashes, _, err := hashFiles(directory, func(path string) bool { return path == "build.json" })
	if err != nil {
		return record, err
	}
	if !reflect.DeepEqual(hashes, record.SHA256) || len(hashes) != len(meta.Names) {
		return record, fmt.Errorf("missing, altered, or unexpected artifact files")
	}
	for _, name := range meta.Names {
		filename := name + "-" + meta.Version + ".apk"
		if _, ok := hashes[filename]; !ok {
			return record, fmt.Errorf("missing APK: %s", filename)
		}
		if _, err := apk.Check(filepath.Join(directory, filename), name, meta.Version, arch, nil); err != nil {
			return record, err
		}
	}
	return record, nil
}

func verifyAppIndex(directory string, meta AppMetadata, arch string, trust *rsa.PublicKey) (map[string]string, error) {
	entries, err := apk.Index(filepath.Join(directory, "APKINDEX.tar.gz"), arch, trust)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(meta.Names) {
		return nil, fmt.Errorf("index does not cover the complete package set")
	}
	for _, name := range meta.Names {
		filename := name + "-" + meta.Version + ".apk"
		stanza, ok := entries[filename]
		if !ok {
			return nil, fmt.Errorf("missing indexed package: %s", filename)
		}
		info, err := apk.Check(filepath.Join(directory, filename), name, meta.Version, arch, trust)
		if err != nil {
			return nil, err
		}
		if err := apk.Match(info, stanza); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func restoreApplication(ctx context.Context, store Store, meta AppMetadata, work string, trust *rsa.PublicKey) (*AppManifest, error) {
	prefix := "builds/" + meta.Context + "/" + meta.Inputs + "/"
	path := filepath.Join(work, "release.json")
	etag, err := store.Fetch(ctx, prefix+"release.json", path)
	if err != nil || etag == "" {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest AppManifest
	if err := OpenEnvelope(data, trust, &manifest); err != nil {
		return nil, err
	}
	if manifest.Format != "werewolf-package-manifest/1" || !sameMetadata(manifest.AppMetadata, meta) || len(manifest.Architectures) != len(Arches) {
		return nil, fmt.Errorf("staged release differs from checkout")
	}
	for _, arch := range Arches {
		hashes, ok := manifest.Architectures[arch]
		if !ok || len(hashes) != len(meta.Names)+1 {
			return nil, fmt.Errorf("staged release is missing native artifacts")
		}
		names := []string{"APKINDEX.tar.gz"}
		for _, name := range meta.Names {
			names = append(names, name+"-"+meta.Version+".apk")
		}
		directory := filepath.Join(work, arch)
		if err := os.Mkdir(directory, 0755); err != nil {
			return nil, err
		}
		for _, name := range names {
			sum, ok := hashes[name]
			if !ok {
				return nil, fmt.Errorf("unexpected staged package files")
			}
			path := filepath.Join(directory, name)
			etag, err := store.Fetch(ctx, prefix+arch+"/"+name, path)
			if err != nil {
				return nil, err
			}
			if etag == "" {
				return nil, fmt.Errorf("missing staged artifact: %s", name)
			}
			actual, err := digest(path)
			if err != nil {
				return nil, err
			}
			if actual != sum {
				return nil, fmt.Errorf("altered staged artifact: %s", name)
			}
		}
		if _, err := verifyAppIndex(directory, meta, arch, trust); err != nil {
			return nil, err
		}
	}
	return &manifest, nil
}

func prepareApplication(artifacts, work string, meta AppMetadata, key *rsa.PrivateKey) (*AppManifest, error) {
	for _, arch := range Arches {
		if _, err := ValidateApplication(filepath.Join(artifacts, arch), meta, arch, os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_RUN_ID")); err != nil {
			return nil, err
		}
	}
	number, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_NUMBER"), 10, 64)
	if err != nil || number < 1 {
		return nil, fmt.Errorf("invalid workflow run number")
	}
	manifest := &AppManifest{AppMetadata: meta, Format: "werewolf-package-manifest/1", Commit: os.Getenv("GITHUB_SHA"), RunID: os.Getenv("GITHUB_RUN_ID"), RunNumber: number, Architectures: map[string]map[string]string{}}
	for _, arch := range Arches {
		directory := filepath.Join(work, arch)
		if err := os.Mkdir(directory, 0755); err != nil {
			return nil, err
		}
		for _, name := range meta.Names {
			filename := name + "-" + meta.Version + ".apk"
			path := filepath.Join(directory, filename)
			if err := apk.SignPackage(filepath.Join(artifacts, arch, filename), path, name, meta.Version, arch, key); err != nil {
				return nil, err
			}
		}
	}
	return manifest, nil
}

// PublishApplication resumes an authenticated snapshot when available. Native
// artifacts are validated together before signing or writing remote objects.
func PublishApplication(ctx context.Context, pkg Package, artifacts string) error {
	meta, err := Metadata(ctx, pkg)
	if err != nil {
		return err
	}
	keyPath, err := SigningKey(pkg.Root, true)
	if err != nil {
		return err
	}
	defer os.Remove(keyPath)
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	key, err := apk.PrivateKey(data)
	if err != nil {
		return err
	}
	trust, err := trustKey(pkg.Root)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "packages-publish-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	store := Store{Prefix: "apk/packages/"}
	manifest, err := restoreApplication(ctx, store, meta, work, trust)
	if err != nil {
		return err
	}
	if manifest == nil {
		manifest, err = prepareApplication(artifacts, work, meta, key)
		if err != nil {
			return err
		}
		tool, _, err := melange(ctx, pkg.Root)
		if err != nil {
			return err
		}
		for _, arch := range Arches {
			directory := filepath.Join(work, arch)
			args := []string{"index", "--arch", arch, "--signing-key", keyPath, "--output", filepath.Join(directory, "APKINDEX.tar.gz")}
			for _, name := range meta.Names {
				args = append(args, filepath.Join(directory, name+"-"+meta.Version+".apk"))
			}
			if err := Run(ctx, pkg.Root, tool, args...); err != nil {
				return err
			}
			if _, err := verifyAppIndex(directory, meta, arch, trust); err != nil {
				return err
			}
			hashes, _, err := hashFiles(directory, func(string) bool { return false })
			if err != nil {
				return err
			}
			manifest.Architectures[arch] = hashes
		}
		signed, err := Envelope(manifest, key)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(work, "release.json"), signed, 0644); err != nil {
			return err
		}
		prefix := "builds/" + meta.Context + "/" + meta.Inputs + "/"
		for _, arch := range Arches {
			for name := range manifest.Architectures[arch] {
				if err := store.Immutable(ctx, filepath.Join(work, arch, name), prefix+arch+"/"+name); err != nil {
					return err
				}
			}
		}
		if err := store.Immutable(ctx, filepath.Join(work, "release.json"), prefix+"release.json"); err != nil {
			return err
		}
	}
	for _, arch := range Arches {
		directory := filepath.Join(work, arch)
		for _, name := range meta.Names {
			filename := name + "-" + meta.Version + ".apk"
			if err := store.Immutable(ctx, filepath.Join(directory, filename), arch+"/"+filename); err != nil {
				return err
			}
		}
		entries, err := verifyAppIndex(directory, meta, arch, trust)
		if err != nil {
			return err
		}
		if err := store.MergeIndex(ctx, entries, arch, key, trust, work, false); err != nil {
			return err
		}
	}
	remote := "contexts/" + meta.Context + "/current.json"
	for attempt := 0; attempt < 12; attempt++ {
		previous := filepath.Join(work, "previous-release")
		etag, err := store.Fetch(ctx, remote, previous)
		if err != nil {
			return err
		}
		if etag != "" {
			data, err := os.ReadFile(previous)
			if err != nil {
				return err
			}
			var old AppManifest
			if err := OpenEnvelope(data, trust, &old); err != nil {
				return err
			}
			if old.Context != meta.Context {
				return fmt.Errorf("release pointer names another context")
			}
			if old.RunNumber >= manifest.RunNumber {
				return nil
			}
		}
		err = store.conditional(ctx, remote, filepath.Join(work, "release.json"), etag)
		if err == nil {
			slog.Info("published package", "package", pkg.Name, "version", meta.Version, "inputs", meta.Inputs)
			return nil
		}
		if !raced(err) {
			return err
		}
		if err := wait(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("release pointer kept changing")
}
