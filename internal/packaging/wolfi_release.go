package packaging

import (
	"archive/tar"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/werewolf-linux/packages/internal/apk"
	"github.com/werewolf-linux/packages/internal/overlay"
)

const hardenedURL = "https://dist.werewolf-linux.org/apk/hardened/"

// Release is a GitHub draft or completed immutable build publication.
type Release struct {
	ID        int64  `json:"id"`
	Tag       string `json:"tag_name"`
	Draft     bool   `json:"draft"`
	Body      string `json:"body"`
	UploadURL string `json:"upload_url"`
	Assets    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"assets"`
}

// WolfiManifest ties both native archives to the captured input identity.
type WolfiManifest struct {
	Package    string            `json:"package"`
	BuildID    string            `json:"build_id"`
	Version    string            `json:"version"`
	SourceSHA  string            `json:"source_sha"`
	RunNumber  int64             `json:"run_number"`
	Repository string            `json:"repository"`
	SHA256     map[string]string `json:"sha256"`
}

func githubRepository() (string, error) {
	repository := os.Getenv("GITHUB_REPOSITORY")
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) {
		return "", fmt.Errorf("GITHUB_REPOSITORY must name an owner and repository")
	}
	return "repos/" + repository, nil
}

func githubJSON(ctx context.Context, value any, args ...string) error {
	text, err := output(ctx, "", "gh", append([]string{"api"}, args...)...)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(text), value)
}

// ReleaseStatus distinguishes missing, partially staged, staged and completed builds.
func ReleaseStatus(ctx context.Context, tag string) (string, *Release, error) {
	repository, err := githubRepository()
	if err != nil {
		return "", nil, err
	}
	var pages [][]Release
	if err := githubJSON(ctx, &pages, "--paginate", "--slurp", repository+"/releases?per_page=100"); err != nil {
		return "", nil, err
	}
	var found *Release
	for _, page := range pages {
		for _, release := range page {
			if release.Tag == tag {
				if found != nil {
					return "", nil, fmt.Errorf("multiple releases use tag %s", tag)
				}
				copy := release
				found = &copy
			}
		}
	}
	if found == nil {
		return "build", nil, nil
	}
	names := map[string]bool{}
	for _, asset := range found.Assets {
		if names[asset.Name] {
			return "", nil, fmt.Errorf("duplicate release asset")
		}
		names[asset.Name] = true
	}
	if !names["release.json"] {
		if !found.Draft {
			return "", nil, fmt.Errorf("published release lacks completion manifest")
		}
		return "build", found, nil
	}
	if found.Draft {
		return "publish", found, nil
	}
	return "complete", found, nil
}

func downloadAsset(ctx context.Context, release *Release, name, destination string) error {
	repository, err := githubRepository()
	if err != nil {
		return err
	}
	var id int64
	for _, asset := range release.Assets {
		if asset.Name == name {
			if id != 0 {
				return fmt.Errorf("duplicate release asset: %s", name)
			}
			id = asset.ID
		}
	}
	if id == 0 {
		return fmt.Errorf("missing release asset: %s", name)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "-H", "Accept: application/octet-stream", repository+"/releases/assets/"+strconv.FormatInt(id, 10))
	cmd.Stdout = file
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	closeErr := file.Close()
	if runErr != nil {
		return runErr
	}
	return closeErr
}

func uploadAsset(ctx context.Context, release *Release, path string) error {
	address, err := url.Parse(strings.SplitN(release.UploadURL, "{", 2)[0])
	if err != nil {
		return err
	}
	if address.Scheme != "https" || address.Host != "uploads.github.com" {
		return fmt.Errorf("unexpected release upload endpoint")
	}
	query := address.Query()
	query.Set("name", filepath.Base(path))
	address.RawQuery = query.Encode()
	var response struct{ ID int64 }
	return githubJSON(ctx, &response, "--method", "POST", address.String(), "-H", "Content-Type: application/octet-stream", "--input", path)
}

func tagTarget(ctx context.Context, tag, source string) (map[string]any, error) {
	if !exactCommit.MatchString(source) {
		return nil, fmt.Errorf("invalid validated source commit")
	}
	repository, err := githubRepository()
	if err != nil {
		return nil, err
	}
	var reference struct{ Object struct{ Type, SHA string } }
	err = githubJSON(ctx, &reference, repository+"/git/ref/tags/"+tag)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return map[string]any{"target_commitish": source}, nil
		}
		return nil, err
	}
	if reference.Object.Type != "commit" || reference.Object.SHA != source {
		return nil, fmt.Errorf("release tag does not point to validated source")
	}
	return map[string]any{}, nil
}

// ValidateWolfi checks hashes, recipe, signing root, complete package coverage,
// and the policy evidence carried by a successful native build.
func ValidateWolfi(pkg Package, directory, arch string, lock Inputs, sourceSHA string) (WolfiBuild, error) {
	var record WolfiBuild
	data, err := os.ReadFile(filepath.Join(directory, "build.json"))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	expected := lock.Version + "-r" + strconv.Itoa(lock.Revision*pkg.Spec.EpochMultiplier+lock.Epoch)
	if record.Package != pkg.Name || record.BuildID != lock.BuildID || record.Arch != arch || record.Version != expected || !exactCommit.MatchString(sourceSHA) || record.SourceSHA != sourceSHA {
		return record, fmt.Errorf("mixed build identity, architecture, version or downstream source")
	}
	hashes, _, err := hashFiles(directory, func(path string) bool { return path == "build.json" })
	if err != nil {
		return record, err
	}
	if !reflect.DeepEqual(hashes, record.SHA256) {
		return record, fmt.Errorf("altered, missing, or unexpected artifact files")
	}
	trust, err := trustKey(pkg.Root)
	if err != nil {
		return record, err
	}
	data, err = os.ReadFile(filepath.Join(directory, "werewolf-packages.rsa.pub"))
	if err != nil {
		return record, err
	}
	key, err := apk.PublicKey(data)
	if err != nil {
		return record, err
	}
	if key.E != trust.E || key.N.Cmp(trust.N) != 0 {
		return record, fmt.Errorf("ephemeral signing key cannot be published")
	}
	data, err = os.ReadFile(filepath.Join(directory, "upstream.lock.json"))
	if err != nil {
		return record, err
	}
	var captured Inputs
	if err := json.Unmarshal(data, &captured); err != nil {
		return record, err
	}
	if captured.BuildID != lock.BuildID || !reflect.DeepEqual(captured.SHA256, lock.SHA256) || !reflect.DeepEqual(captured.Mode, lock.Mode) || !reflect.DeepEqual(captured.PolicySHA256, lock.PolicySHA256) {
		return record, fmt.Errorf("upstream provenance mismatch")
	}
	recipe, err := Derive(pkg)
	if err != nil {
		return record, err
	}
	artifactRecipe, err := readRecipe(filepath.Join(directory, pkg.Name+".yaml"))
	if err != nil {
		return record, err
	}
	if !overlay.Equal(recipe, artifactRecipe) {
		return record, fmt.Errorf("artifact recipe differs from verified derivation")
	}
	names, err := overlay.OutputNames(recipe)
	if err != nil {
		return record, err
	}
	entries, err := apk.Index(filepath.Join(directory, arch, "APKINDEX.tar.gz"), arch, trust)
	if err != nil {
		return record, err
	}
	apks, err := filepath.Glob(filepath.Join(directory, arch, "*.apk"))
	if err != nil {
		return record, err
	}
	if len(apks) != len(names) || len(entries) != len(names) {
		return record, fmt.Errorf("incomplete or unexpected APK set")
	}
	results := 0
	for _, name := range names {
		filename := name + "-" + expected + ".apk"
		stanza, ok := entries[filename]
		if !ok {
			return record, fmt.Errorf("missing indexed package: %s", filename)
		}
		path := filepath.Join(directory, arch, filename)
		info, err := apk.Check(path, name, expected, arch, trust)
		if err != nil {
			return record, err
		}
		if err := apk.Match(info, stanza); err != nil {
			return record, err
		}
		isResults := pkg.Spec.ResultsSuffix != "" && strings.HasSuffix(name, pkg.Spec.ResultsSuffix)
		if isResults {
			results++
		}
		if err := checkPayload(path, info, isResults, pkg.Policy.MaxFailPerMille); err != nil {
			return record, err
		}
	}
	if pkg.Spec.ResultsSuffix != "" && results != 1 {
		return record, fmt.Errorf("missing or duplicate upstream test-results package")
	}
	return record, nil
}

func checkPayload(path string, info apk.Info, results bool, ceiling int) error {
	passed, failed, report := 0, 0, false
	err := apk.Payload(path, info, func(header *tar.Header, reader io.Reader) error {
		name := header.Name
		if name == "etc/runtime-hardening" || strings.HasPrefix(name, "etc/runtime-hardening/") {
			return fmt.Errorf("package grants its own runtime-hardening exception: %s", info.Name)
		}
		if !results || header.Typeflag != tar.TypeReg {
			return nil
		}
		if strings.HasSuffix(name, "/policy-report.json") {
			report = true
		}
		if strings.HasSuffix(name, ".test-result") {
			data, err := io.ReadAll(io.LimitReader(reader, 256))
			if err != nil {
				return err
			}
			line := strings.SplitN(string(data), "\n", 2)[0]
			if strings.HasPrefix(line, "PASS:") {
				passed++
			}
			if strings.HasPrefix(line, "FAIL:") {
				failed++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if results && (passed+failed == 0 || failed*1000 > (passed+failed)*ceiling || !report) {
		return fmt.Errorf("raw test evidence gate failed: PASS=%d FAIL=%d policy-report=%t", passed, failed, report)
	}
	return nil
}

// StageWolfi validates both native builds before completing a GitHub draft.
func StageWolfi(ctx context.Context, pkg Package, artifacts, assets string) error {
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	tag := pkg.Name + "/build-" + lock.BuildID
	if err := os.MkdirAll(assets, 0755); err != nil {
		return err
	}
	number, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_NUMBER"), 10, 64)
	if err != nil || number < 1 {
		return fmt.Errorf("invalid source workflow run number")
	}
	manifest := WolfiManifest{Package: pkg.Name, BuildID: lock.BuildID, SourceSHA: os.Getenv("GITHUB_SHA"), RunNumber: number, Repository: "https://dist.werewolf-linux.org/apk/" + pkg.Name + "/builds/" + lock.BuildID, SHA256: map[string]string{}}
	for _, arch := range Arches {
		directory := filepath.Join(artifacts, pkg.Name+"-"+arch)
		record, err := ValidateWolfi(pkg, directory, arch, lock, manifest.SourceSHA)
		if err != nil {
			return err
		}
		if manifest.Version != "" && manifest.Version != record.Version {
			return fmt.Errorf("native builds have different versions")
		}
		manifest.Version = record.Version
		name := pkg.Name + "-" + arch + ".tar.gz"
		if err := Archive(directory, filepath.Join(assets, name)); err != nil {
			return err
		}
		sum, err := digest(filepath.Join(assets, name))
		if err != nil {
			return err
		}
		manifest.SHA256[name] = sum
	}
	if err := writeJSON(filepath.Join(assets, "release.json"), manifest); err != nil {
		return err
	}
	var sums strings.Builder
	for _, arch := range Arches {
		name := pkg.Name + "-" + arch + ".tar.gz"
		fmt.Fprintf(&sums, "%s  %s\n", manifest.SHA256[name], name)
	}
	if err := writeFile(filepath.Join(assets, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	_, release, err := ReleaseStatus(ctx, tag)
	if err != nil {
		return err
	}
	noteFiles := []string{"release-notes.md", "release-notes.html", "release-notes.json"}
	noted := false
	if release != nil {
		for _, asset := range release.Assets {
			noted = noted || asset.Name == "release-notes.json"
		}
	}
	if noted {
		for _, name := range noteFiles {
			if err := downloadAsset(ctx, release, name, filepath.Join(assets, name)); err != nil {
				return err
			}
		}
	} else {
		if err := releaseNotes(ctx, pkg, artifacts, assets, lock, manifest); err != nil {
			return err
		}
	}
	body, err := os.ReadFile(filepath.Join(assets, "release-notes.md"))
	if err != nil {
		return err
	}
	repository, err := githubRepository()
	if err != nil {
		return err
	}
	request := map[string]any{"body": string(body)}
	method, endpoint := "PATCH", repository+"/releases/"
	if release == nil {
		target, err := tagTarget(ctx, tag, manifest.SourceSHA)
		if err != nil {
			return err
		}
		for key, value := range target {
			request[key] = value
		}
		request["tag_name"] = tag
		request["draft"] = true
		request["make_latest"] = "false"
		request["name"] = pkg.Spec.Title + " " + manifest.Version
		method, endpoint = "POST", repository+"/releases"
	} else {
		if !release.Draft {
			return fmt.Errorf("refusing to change a published release")
		}
		endpoint += strconv.FormatInt(release.ID, 10)
	}
	requestPath := filepath.Join(assets, "release-request.json")
	if err := writeJSON(requestPath, request); err != nil {
		return err
	}
	if err := githubJSON(ctx, &release, "--method", method, endpoint, "--input", requestPath); err != nil {
		return err
	}
	names := []string{}
	for _, arch := range Arches {
		names = append(names, pkg.Name+"-"+arch+".tar.gz")
	}
	names = append(names, "SHA256SUMS")
	names = append(names, noteFiles...)
	names = append(names, "release.json")
	temporary, err := os.MkdirTemp("", "packages-staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	for _, name := range names {
		exists := false
		for _, asset := range release.Assets {
			exists = exists || asset.Name == name
		}
		if exists {
			previous := filepath.Join(temporary, name)
			if err := downloadAsset(ctx, release, name, previous); err != nil {
				return err
			}
			old, err := digest(previous)
			if err != nil {
				return err
			}
			next, err := digest(filepath.Join(assets, name))
			if err != nil {
				return err
			}
			if old != next {
				return fmt.Errorf("release asset already has different bytes: %s", name)
			}
		} else if err := uploadAsset(ctx, release, filepath.Join(assets, name)); err != nil {
			return err
		}
	}
	return nil
}

// IncludeFeed repairs the shared hardened feed from signed published history.
func IncludeFeed(ctx context.Context, pkg Package, key *rsa.PrivateKey, trust *rsa.PublicKey) error {
	source := Store{Prefix: "apk/" + pkg.Name + "/"}
	feed := Store{Prefix: "apk/hardened/"}
	work, err := os.MkdirTemp("", "packages-feed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	indexes := map[string]map[string]string{}
	for _, arch := range Arches {
		path := filepath.Join(work, arch+"-source-index")
		etag, err := source.Fetch(ctx, arch+"/APKINDEX.tar.gz", path)
		if err != nil {
			return err
		}
		if etag != "" {
			entries, err := apk.Index(path, arch, trust)
			if err != nil {
				return err
			}
			indexes[arch] = entries
		}
	}
	if len(indexes) == 0 {
		return nil
	}
	if len(indexes) != len(Arches) {
		return fmt.Errorf("published package history is missing an architecture")
	}
	for _, arch := range Arches {
		oldPath := filepath.Join(work, arch+"-feed-index")
		etag, err := feed.Fetch(ctx, arch+"/APKINDEX.tar.gz", oldPath)
		if err != nil {
			return err
		}
		previous := map[string]string{}
		if etag != "" {
			previous, err = apk.Index(oldPath, arch, trust)
			if err != nil {
				return err
			}
		}
		for name, stanza := range indexes[arch] {
			if _, ok := previous[name]; ok {
				continue
			}
			fields := apk.Fields(stanza)
			if name != fields["P"]+"-"+fields["V"]+".apk" || !filepath.IsLocal(name) || strings.ContainsAny(name, "/\\") {
				return fmt.Errorf("unsafe indexed APK name")
			}
			path := filepath.Join(work, name)
			etag, err := source.Fetch(ctx, arch+"/"+name, path)
			if err != nil {
				return err
			}
			if etag == "" {
				return fmt.Errorf("published index names missing APK: %s", name)
			}
			info, err := apk.Check(path, fields["P"], fields["V"], arch, trust)
			if err != nil {
				return err
			}
			if err := apk.Match(info, stanza); err != nil {
				return err
			}
			if err := feed.Immutable(ctx, path, arch+"/"+name); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		if err := feed.MergeIndex(ctx, indexes[arch], arch, key, trust, work, true); err != nil {
			return err
		}
	}
	return nil
}

// PublishWolfi makes both native package sets available in both signed feeds
// before advancing the release pointer and making the GitHub draft public.
func PublishWolfi(ctx context.Context, pkg Package, assets string) error {
	lock, err := VerifyInputs(pkg)
	if err != nil {
		return err
	}
	tag := pkg.Name + "/build-" + lock.BuildID
	_, release, err := ReleaseStatus(ctx, tag)
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("release has not been staged")
	}
	if err := os.MkdirAll(assets, 0755); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, asset := range release.Assets {
		name := asset.Name
		if !filepath.IsLocal(name) || strings.ContainsAny(name, "/\\") || seen[name] {
			return fmt.Errorf("unsafe or duplicate release asset")
		}
		seen[name] = true
		if err := downloadAsset(ctx, release, name, filepath.Join(assets, name)); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(assets, "release.json"))
	if err != nil {
		return err
	}
	var manifest WolfiManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Package != pkg.Name || manifest.BuildID != lock.BuildID || !exactCommit.MatchString(manifest.SourceSHA) || len(manifest.SHA256) != len(Arches) || manifest.RunNumber < 1 {
		return fmt.Errorf("release manifest differs from captured inputs")
	}
	keyPath, err := SigningKey(filepath.Join(pkg.Root, "wolfi"), true)
	if err != nil {
		return err
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	key, err := apk.PrivateKey(keyBytes)
	if err != nil {
		return err
	}
	trust, err := trustKey(pkg.Root)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "packages-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	source := Store{Prefix: "apk/" + pkg.Name + "/"}
	snapshot := "builds/" + lock.BuildID + "/"
	roots := map[string]string{}
	indexes := map[string]map[string]string{}
	for _, arch := range Arches {
		name := pkg.Name + "-" + arch + ".tar.gz"
		sum, ok := manifest.SHA256[name]
		if !ok {
			return fmt.Errorf("release is missing native architecture")
		}
		actual, err := digest(filepath.Join(assets, name))
		if err != nil {
			return err
		}
		if actual != sum {
			return fmt.Errorf("release archive hash mismatch")
		}
		root := filepath.Join(work, arch)
		if err := Extract(filepath.Join(assets, name), root); err != nil {
			return err
		}
		record, err := ValidateWolfi(pkg, root, arch, lock, manifest.SourceSHA)
		if err != nil {
			return err
		}
		if record.Version != manifest.Version {
			return fmt.Errorf("release version differs from validated artifacts")
		}
		roots[arch] = root
		indexes[arch], err = apk.Index(filepath.Join(root, arch, "APKINDEX.tar.gz"), arch, trust)
		if err != nil {
			return err
		}
	}
	for _, arch := range Arches {
		for name := range indexes[arch] {
			if err := source.Immutable(ctx, filepath.Join(roots[arch], arch, name), snapshot+arch+"/"+name); err != nil {
				return err
			}
		}
		if err := source.Immutable(ctx, filepath.Join(roots[arch], arch, "APKINDEX.tar.gz"), snapshot+arch+"/APKINDEX.tar.gz"); err != nil {
			return err
		}
		name := pkg.Name + "-" + arch + ".tar.gz"
		if err := source.Immutable(ctx, filepath.Join(assets, name), snapshot+name); err != nil {
			return err
		}
	}
	for _, name := range []string{"SHA256SUMS", "release-notes.md", "release-notes.html", "release-notes.json", "release.json"} {
		if !seen[name] {
			continue
		}
		if err := source.Immutable(ctx, filepath.Join(assets, name), snapshot+name); err != nil {
			return err
		}
	}
	for _, arch := range Arches {
		// Preserve the first published build for an existing version.
		old := map[string]string{}
		previous := filepath.Join(work, arch+"-stable-index")
		etag, err := source.Fetch(ctx, arch+"/APKINDEX.tar.gz", previous)
		if err != nil {
			return err
		}
		if etag != "" {
			old, err = apk.Index(previous, arch, trust)
			if err != nil {
				return err
			}
		}
		for name := range indexes[arch] {
			if _, ok := old[name]; ok {
				continue
			}
			if err := source.Immutable(ctx, filepath.Join(roots[arch], arch, name), arch+"/"+name); err != nil {
				return err
			}
		}
		if err := source.MergeIndex(ctx, indexes[arch], arch, key, trust, work, true); err != nil {
			return err
		}
	}
	if err := IncludeFeed(ctx, pkg, key, trust); err != nil {
		return err
	}
	if seen["release-notes.json"] {
		data, err := os.ReadFile(filepath.Join(assets, "release-notes.json"))
		if err != nil {
			return err
		}
		var notes releaseNote
		if err := json.Unmarshal(data, &notes); err != nil {
			return err
		}
		if notes.BuildID != manifest.BuildID || notes.Version != manifest.Version || notes.Package != pkg.Name {
			return fmt.Errorf("release notes differ from staged manifest")
		}
		if err := releaseHistory(ctx, source, notes, work); err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 12; attempt++ {
		previous := filepath.Join(work, "current.json")
		etag, err := source.Fetch(ctx, "current.json", previous)
		if err != nil {
			return err
		}
		if etag != "" {
			data, err := os.ReadFile(previous)
			if err != nil {
				return err
			}
			var old WolfiManifest
			if err := json.Unmarshal(data, &old); err != nil {
				return err
			}
			if old.RunNumber >= manifest.RunNumber {
				break
			}
		}
		err = source.conditional(ctx, "current.json", filepath.Join(assets, "release.json"), etag)
		if err == nil {
			break
		}
		if !raced(err) {
			return err
		}
		if attempt == 11 {
			return fmt.Errorf("release pointer kept changing")
		}
		if err := wait(ctx); err != nil {
			return err
		}
	}
	repository, err := githubRepository()
	if err != nil {
		return err
	}
	request := filepath.Join(work, "publish.json")
	if err := writeJSON(request, map[string]any{"draft": false, "make_latest": "false"}); err != nil {
		return err
	}
	var updated Release
	if err := githubJSON(ctx, &updated, "--method", "PATCH", repository+"/releases/"+strconv.FormatInt(release.ID, 10), "--input", request); err != nil {
		return err
	}
	slog.Info("published hardened package", "package", pkg.Name, "version", manifest.Version, "build_id", lock.BuildID, "repository", hardenedURL)
	return nil
}

// Feed backfills selected repositories independently from authenticated history.
func Feed(ctx context.Context, packages []Package) error {
	if len(packages) == 0 {
		return nil
	}
	root := packages[0].Root
	keyPath, err := SigningKey(filepath.Join(root, "wolfi"), true)
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
	trust, err := trustKey(root)
	if err != nil {
		return err
	}
	var failed []string
	for _, pkg := range packages {
		if pkg.Engine != "wolfi" {
			continue
		}
		if err := IncludeFeed(ctx, pkg, key, trust); err != nil {
			failed = append(failed, pkg.Name)
			slog.Error("cannot include package in hardened feed", "package", pkg.Name, "error", err)
		} else {
			slog.Info("hardened feed includes published history", "package", pkg.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("feed updates failed: %v", failed)
	}
	return nil
}
