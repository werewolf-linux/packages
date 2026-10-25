package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/internal/overlay"
	"github.com/werewolf-linux/packages/internal/update"
)

func testTarMember(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestApplicationArtifacts(t *testing.T) {
	directory := t.TempDir()
	payload := testTarMember(t, "usr/bin/example", []byte("executable"))
	sum := sha256.Sum256(payload)
	control := testTarMember(t, ".PKGINFO", []byte("pkgname = example\npkgver = 1-r0\narch = x86_64\ndatahash = "+hex.EncodeToString(sum[:])+"\n"))
	path := filepath.Join(directory, "example-1-r0.apk")
	if err := os.WriteFile(path, append(control, payload...), 0644); err != nil {
		t.Fatal(err)
	}
	hash, err := digest(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := AppMetadata{Context: "example", Names: []string{"example"}, Version: "1-r0", Inputs: strings.Repeat("a", 64)}
	record := AppBuild{AppMetadata: meta, Format: "werewolf-package-build/1", Arch: "x86_64", Commit: strings.Repeat("b", 40), RunID: "123", SHA256: map[string]string{"example-1-r0.apk": hash}}
	if err := writeJSON(filepath.Join(directory, "build.json"), record); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateApplication(directory, meta, "x86_64", record.Commit, record.RunID); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AppBuild){"different source": func(r *AppBuild) { r.Commit = strings.Repeat("c", 40) }, "different run": func(r *AppBuild) { r.RunID = "124" }, "different inputs": func(r *AppBuild) { r.Inputs = "other" }, "different architecture": func(r *AppBuild) { r.Arch = "aarch64" }} {
		t.Run(name, func(t *testing.T) {
			changed := record
			mutate(&changed)
			if err := writeJSON(filepath.Join(directory, "build.json"), changed); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateApplication(directory, meta, "x86_64", record.Commit, record.RunID); err == nil {
				t.Fatal("accepted mismatched provenance")
			}
		})
	}
	if err := writeJSON(filepath.Join(directory, "build.json"), record); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "extra"), []byte("unrecorded"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateApplication(directory, meta, "x86_64", record.Commit, record.RunID); err == nil {
		t.Fatal("accepted extra artifact file")
	}
	if err := os.Remove(filepath.Join(directory, "extra")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateApplication(directory, meta, "x86_64", record.Commit, record.RunID); err == nil {
		t.Fatal("accepted symlinked artifact")
	}
}

func TestBuildEnvironmentHasNoReleaseCredentials(t *testing.T) {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "PACKAGES_APK_SIGNING_KEY"} {
		t.Setenv(key, "secret")
	}
	environment := buildEnv(map[string]string{"SOURCE_DATE_EPOCH": "123"})
	for _, item := range environment {
		if strings.Contains(item, "secret") {
			t.Fatal("release credentials reached package builder")
		}
	}
	if !strings.Contains(strings.Join(environment, "\n"), "SOURCE_DATE_EPOCH=123") {
		t.Fatal("source timestamp was lost")
	}
}

func TestPublisherRepairsDoNotChangeBuildPolicy(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	for _, candidate := range packages {
		if candidate.Name == "glibc" {
			pkg = candidate
		}
	}
	hashes, err := policyHashes(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"internal/packaging/store.go", "internal/packaging/notes.go", "internal/packaging/wolfi_release.go"} {
		if _, ok := hashes[name]; ok {
			t.Errorf("publisher-only file affects native inputs: %s", name)
		}
	}
	for _, name := range []string{"internal/packaging/build.go", "internal/overlay/recipe.go", ".github/workflows/wolfi-build.yml"} {
		if hashes[name] == "" {
			t.Errorf("build input is not bound: %s", name)
		}
	}
}

func TestIndependentUpstreamReleaseAdapters(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		version := map[string]string{"python": "3.16.1", "postgresql": "19.1"}[pkg.Name]
		if version == "" {
			continue
		}
		t.Run(pkg.Name, func(t *testing.T) {
			fixtures, err := filepath.Glob(filepath.Join(pkg.Directory, "tests/fixtures/*.yaml"))
			if err != nil || len(fixtures) == 0 {
				t.Fatal(err)
			}
			baseline, err := readRecipe(fixtures[0])
			if err != nil {
				t.Fatal(err)
			}
			lock := Inputs{Lock: overlay.Lock{Recipe: overlay.Value(overlay.Field(baseline, "package"), "name") + ".yaml", Version: version, Revision: pkg.Policy.Revision, SourceCommit: strings.Repeat("f", 40)}, Release: &update.Release{Version: version}}
			recipe, err := effective(pkg, baseline, lock)
			if err != nil {
				t.Fatal(err)
			}
			vars, err := overlay.Variables(recipe, lock.Lock, pkg.Policy.MaxFailPerMille, filepath.Join(root, "wolfi/pipelines"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := overlay.Derive(pkg.Spec, recipe, lock.Lock, pkg.Directory, vars)
			if err != nil {
				t.Fatal(err)
			}
			if got := overlay.Value(overlay.Field(result, "package"), "version"); got != version {
				t.Fatalf("version = %s, want %s", got, version)
			}
			if _, err := overlay.OutputNames(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReleasePageEscapesUntrustedText(t *testing.T) {
	page, err := htmltemplate.ParseFS(noteTemplates, "templates/release.html.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	notes := releaseNote{Title: `<script>alert("x")</script>`, Run: "javascript:alert(1)"}
	var output bytes.Buffer
	if err := page.Execute(&output, notes); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "<script>") || strings.Contains(output.String(), `href="javascript:`) {
		t.Fatal("release page contains executable untrusted text")
	}
}
