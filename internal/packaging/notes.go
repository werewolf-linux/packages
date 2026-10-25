package packaging

import (
	"archive/tar"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/werewolf-linux/packages/internal/apk"
	"github.com/werewolf-linux/packages/internal/update"
)

//go:embed templates/*
var noteTemplates embed.FS

type packageSize struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Size    int64  `json:"size"`
}
type commitNote struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}
type advisoryFix struct {
	Version string   `json:"version"`
	IDs     []string `json:"ids"`
}
type securityNotes struct {
	Stream      string        `json:"stream"`
	Previous    string        `json:"previous,omitempty"`
	Current     string        `json:"current"`
	Fixed       []advisoryFix `json:"fixed"`
	Through     []advisoryFix `json:"through"`
	Cumulative  int           `json:"cumulative"`
	NotAffected int           `json:"not_affected"`
}
type architectureNotes struct {
	Packages     []packageSize     `json:"packages"`
	TestStatuses map[string]string `json:"test_statuses,omitempty"`
	Counts       map[string]int    `json:"counts,omitempty"`
	NewFailures  []string          `json:"new_failures,omitempty"`
	Fixed        []string          `json:"fixed,omitempty"`
	Compared     bool              `json:"compared"`
	Ceiling      int               `json:"ceiling_per_mille,omitempty"`
	Report       json.RawMessage   `json:"report,omitempty"`
}
type releaseNote struct {
	Format  string `json:"format"`
	Title   string `json:"title"`
	Package string `json:"package"`
	Version string `json:"version"`
	Date    string `json:"date"`
	BuildID string `json:"build_id"`
	Commit  string `json:"commit"`
	Run     string `json:"run"`
	Source  struct {
		SourceCommit   string `json:"source_commit"`
		GlibcCommit    string `json:"glibc_commit,omitempty"`
		WolfiCommit    string `json:"wolfi_commit"`
		WolfiVersion   string `json:"wolfi_version"`
		Version        string `json:"version"`
		Recipe         string `json:"recipe"`
		PolicyRevision int    `json:"policy_revision"`
		Upstream       string `json:"upstream"`
	} `json:"source"`
	Repository struct {
		Stable    string `json:"stable"`
		Snapshot  string `json:"snapshot"`
		KeySHA256 string `json:"key_sha256"`
	} `json:"repository"`
	Previous *struct {
		Version       string `json:"version"`
		BuildID       string `json:"build_id"`
		Commit        string `json:"commit"`
		WolfiVersion  string `json:"wolfi_version"`
		SourceVersion string `json:"source_version"`
		SourceCommit  string `json:"source_commit"`
	} `json:"previous"`
	Patches          []string                     `json:"patches"`
	WolfiInputs      map[string]string            `json:"wolfi_inputs"`
	Commits          []commitNote                 `json:"commits"`
	CommitsAvailable bool                         `json:"commits_available"`
	Security         securityNotes                `json:"security"`
	Architectures    map[string]architectureNotes `json:"architectures"`
}

var apkVersion = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)(?:_(git|p)([0-9]+))?-r([0-9]+)$`)

func compareAPKVersion(a, b string) (int, bool) {
	aa, bb := apkVersion.FindStringSubmatch(a), apkVersion.FindStringSubmatch(b)
	if aa == nil || bb == nil {
		return 0, false
	}
	if order := update.Compare(aa[1], bb[1]); order != 0 {
		return order, true
	}
	if order := strings.Count(aa[1], ".") - strings.Count(bb[1], "."); order != 0 {
		return order, true
	}
	rank := map[string]int{"": 0, "git": 1, "p": 2}
	if rank[aa[2]] < rank[bb[2]] {
		return -1, true
	}
	if rank[aa[2]] > rank[bb[2]] {
		return 1, true
	}
	if order := update.Compare(aa[3], bb[3]); order != 0 {
		return order, true
	}
	return update.Compare(aa[4], bb[4]), true
}

func advisories(data []byte, stream, previous, current string) (securityNotes, error) {
	found := securityNotes{Stream: stream, Previous: previous, Current: current, Fixed: []advisoryFix{}, Through: []advisoryFix{}}
	if _, ok := compareAPKVersion(current, current); !ok {
		return found, fmt.Errorf("unsupported effective source version: %s", current)
	}
	var database struct {
		Packages []struct {
			Pkg struct {
				Name     string
				Secfixes map[string][]string
			}
		}
	}
	if err := json.Unmarshal(data, &database); err != nil {
		return found, err
	}
	for _, pkg := range database.Packages {
		if pkg.Pkg.Name != stream {
			continue
		}
		found.NotAffected = len(pkg.Pkg.Secfixes["0"])
		for version, ids := range pkg.Pkg.Secfixes {
			order, ok := compareAPKVersion(version, current)
			if !ok || order > 0 {
				continue
			}
			sorted := slices.Clone(ids)
			slices.Sort(sorted)
			fix := advisoryFix{Version: version, IDs: sorted}
			found.Through = append(found.Through, fix)
			found.Cumulative += len(ids)
			if previous != "" {
				if order, ok := compareAPKVersion(version, previous); ok && order > 0 {
					found.Fixed = append(found.Fixed, fix)
				}
			}
		}
	}
	for _, entries := range [][]advisoryFix{found.Fixed, found.Through} {
		slices.SortFunc(entries, func(a, b advisoryFix) int { order, _ := compareAPKVersion(a.Version, b.Version); return order })
	}
	return found, nil
}

func releaseNotes(ctx context.Context, pkg Package, artifacts, assets string, lock Inputs, manifest WolfiManifest) error {
	notes := releaseNote{Format: "hardened-packages-release-notes/2", Title: pkg.Spec.Title, Package: pkg.Name, Version: manifest.Version, BuildID: lock.BuildID, Commit: manifest.SourceSHA, Date: time.Now().UTC().Format("2006-01-02"), Run: env("GITHUB_SERVER_URL", "https://github.com") + "/" + os.Getenv("GITHUB_REPOSITORY") + "/actions/runs/" + os.Getenv("GITHUB_RUN_ID"), WolfiInputs: lock.SHA256, Architectures: map[string]architectureNotes{}}
	notes.Source.SourceCommit = lock.SourceCommit
	notes.Source.WolfiCommit = lock.WolfiCommit
	notes.Source.Version = lock.Version + "-r" + strconv.Itoa(lock.Epoch)
	notes.Source.Recipe = lock.Recipe
	notes.Source.PolicyRevision = lock.Revision
	notes.Source.WolfiVersion = notes.Source.Version
	if lock.Wolfi != nil {
		notes.Source.WolfiVersion = lock.Wolfi.Version + "-r" + strconv.Itoa(lock.Wolfi.Epoch)
	}
	if lock.Release != nil {
		// A git-tags release names its repository by URL; GitHub's by owner/name.
		notes.Source.Upstream = lock.Release.Repository
		if !strings.HasPrefix(notes.Source.Upstream, "https://") {
			notes.Source.Upstream = "https://github.com/" + notes.Source.Upstream
		}
	}
	notes.Repository.Stable = hardenedURL
	notes.Repository.Snapshot = manifest.Repository
	var err error
	notes.Repository.KeySHA256, err = digest(filepath.Join(pkg.Root, "release/packages.pub"))
	if err != nil {
		return err
	}
	series, err := os.ReadFile(filepath.Join(pkg.Directory, "patches/series"))
	if err != nil {
		return err
	}
	notes.Patches = strings.Fields(string(series))
	base := "https://dist.werewolf-linux.org/apk/" + pkg.Name + "/"
	data, err := download(ctx, base+"current.json")
	if err != nil {
		return err
	}
	var old releaseNote
	if data != nil {
		var pointer WolfiManifest
		if err := json.Unmarshal(data, &pointer); err != nil {
			return err
		}
		if pointer.BuildID == lock.BuildID {
			return fmt.Errorf("current.json already names this build")
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(pointer.BuildID) {
			return fmt.Errorf("invalid previous build identity")
		}
		data, err := download(ctx, base+"builds/"+pointer.BuildID+"/release-notes.json")
		if err != nil {
			return err
		}
		if data != nil {
			if err := json.Unmarshal(data, &old); err != nil {
				return err
			}
		}
		notes.Previous = &struct {
			Version       string `json:"version"`
			BuildID       string `json:"build_id"`
			Commit        string `json:"commit"`
			WolfiVersion  string `json:"wolfi_version"`
			SourceVersion string `json:"source_version"`
			SourceCommit  string `json:"source_commit"`
		}{Version: pointer.Version, BuildID: pointer.BuildID, Commit: pointer.SourceSHA, WolfiVersion: old.Source.WolfiVersion, SourceVersion: old.Source.Version, SourceCommit: old.Source.SourceCommit}
		if notes.Previous.SourceVersion == "" {
			notes.Previous.SourceVersion = old.Source.WolfiVersion
		}
		if notes.Previous.SourceCommit == "" {
			notes.Previous.SourceCommit = old.Source.GlibcCommit
		}
		repository, err := githubRepository()
		if err != nil {
			return err
		}
		var comparison struct {
			Commits []struct {
				SHA     string
				Parents []json.RawMessage
				Commit  struct{ Message string }
			}
		}
		if err := githubJSON(ctx, &comparison, repository+"/compare/"+pointer.SourceSHA+"..."+manifest.SourceSHA); err == nil {
			notes.CommitsAvailable = true
			notes.Commits = []commitNote{}
			for _, commit := range comparison.Commits {
				if len(commit.Parents) == 1 {
					notes.Commits = append(notes.Commits, commitNote{SHA: commit.SHA, Subject: strings.SplitN(commit.Commit.Message, "\n", 2)[0]})
				}
			}
		} else {
			slog.Warn("commit comparison unavailable", "package", pkg.Name, "error", err)
		}
	}
	trust, err := trustKey(pkg.Root)
	if err != nil {
		return err
	}
	for _, arch := range Arches {
		directory := filepath.Join(artifacts, pkg.Name+"-"+arch, arch)
		entries, err := apk.Index(filepath.Join(directory, "APKINDEX.tar.gz"), arch, trust)
		if err != nil {
			return err
		}
		native := architectureNotes{Ceiling: pkg.Policy.MaxFailPerMille}
		for filename, stanza := range entries {
			fields := apk.Fields(stanza)
			size, err := strconv.ParseInt(fields["S"], 10, 64)
			if err != nil {
				return err
			}
			native.Packages = append(native.Packages, packageSize{Name: fields["P"], Version: fields["V"], Size: size})
			if pkg.Spec.ResultsSuffix == "" || !strings.HasSuffix(fields["P"], pkg.Spec.ResultsSuffix) {
				continue
			}
			path := filepath.Join(directory, filename)
			info, err := apk.Check(path, fields["P"], fields["V"], arch, trust)
			if err != nil {
				return err
			}
			native.TestStatuses = map[string]string{}
			native.Counts = map[string]int{}
			err = apk.Payload(path, info, func(header *tar.Header, reader io.Reader) error {
				if !strings.HasSuffix(header.Name, "/tests.sum") && !strings.HasSuffix(header.Name, "/policy-report.json") {
					return nil
				}
				data, err := io.ReadAll(io.LimitReader(reader, 16<<20+1))
				if err != nil {
					return err
				}
				if len(data) > 16<<20 {
					return fmt.Errorf("oversized test evidence")
				}
				if strings.HasSuffix(header.Name, "/policy-report.json") {
					if !json.Valid(data) {
						return fmt.Errorf("invalid policy report")
					}
					native.Report = data
					return nil
				}
				for _, line := range strings.Split(string(data), "\n") {
					status, name, ok := strings.Cut(line, ": ")
					if ok && slices.Contains([]string{"PASS", "FAIL", "XFAIL", "XPASS", "UNSUPPORTED"}, status) {
						native.TestStatuses[name] = status
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			before := old.Architectures[arch].TestStatuses
			native.Compared = before != nil
			for name, status := range native.TestStatuses {
				native.Counts[status]++
				if before != nil && status == "FAIL" && before[name] != "FAIL" {
					native.NewFailures = append(native.NewFailures, name)
				}
				if before != nil && status == "PASS" && before[name] == "FAIL" {
					native.Fixed = append(native.Fixed, name)
				}
			}
			slices.Sort(native.NewFailures)
			slices.Sort(native.Fixed)
		}
		slices.SortFunc(native.Packages, func(a, b packageSize) int { return strings.Compare(a.Name, b.Name) })
		notes.Architectures[arch] = native
	}
	data, err = download(ctx, "https://packages.wolfi.dev/os/security.json")
	if err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("Wolfi security database is unavailable")
	}
	stream := strings.TrimSuffix(lock.EffectiveRecipe, ".yaml")
	if stream == "" {
		stream = strings.TrimSuffix(lock.Recipe, ".yaml")
	}
	previous := ""
	if notes.Previous != nil {
		previous = notes.Previous.SourceVersion
	}
	notes.Security, err = advisories(data, stream, previous, notes.Source.Version)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(assets, "release-notes.json"), notes); err != nil {
		return err
	}
	markdown, err := template.ParseFS(noteTemplates, "templates/release.md.tmpl")
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := markdown.Execute(&rendered, notes); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(assets, "release-notes.md"), rendered.Bytes(), 0644); err != nil {
		return err
	}
	page, err := htmltemplate.ParseFS(noteTemplates, "templates/release.html.tmpl")
	if err != nil {
		return err
	}
	rendered.Reset()
	if err := page.Execute(&rendered, notes); err != nil {
		return err
	}
	return writeFile(filepath.Join(assets, "release-notes.html"), rendered.Bytes(), 0644)
}
