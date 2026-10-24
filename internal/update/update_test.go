package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockClient(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	c := NewClient("secret")
	c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if (r.URL.Host == "api.github.com") != (r.Header.Get("Authorization") == "Bearer secret") {
			t.Fatal("wrong credential scope", r.URL)
		}
		body, ok := routes[r.URL.String()]
		status := 200
		if !ok {
			status = 404
			body = "missing fixture"
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	return c
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hash(s string) string                                        { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func noCommands(context.Context, string, string, ...string) error { return nil }

func TestStableSelection(t *testing.T) {
	for _, source := range []string{"github", "github-tags"} {
		t.Run(source, func(t *testing.T) {
			endpoint := "releases"
			if source == "github-tags" {
				endpoint = "tags"
			}
			var items []map[string]any
			for _, tag := range []string{"v1.9.0", "v2.0.0rc1", "v2.0.0-rc.2", "v2.0.0-beta.1", "v1.10.2", "wrong3.0.0"} {
				items = append(items, map[string]any{"name": tag, "tag_name": tag})
			}
			if source == "github" {
				items = append(items, map[string]any{"tag_name": "v8.0.0", "prerelease": true}, map[string]any{"tag_name": "v9.0.0", "draft": true})
			}
			data, _ := json.Marshal(items)
			routes := map[string]string{"https://api.github.com/repos/a/b/" + endpoint + "?per_page=100&page=1": string(data)}
			if source == "github" {
				routes["https://api.github.com/repos/a/b/releases/latest"] = `{"tag_name":"v1.10.2"}`
			}
			c := mockClient(t, routes)
			r, err := c.Discover(context.Background(), Policy{Source: source, Repo: "a/b", Prefix: "v"}, "1.0.0")
			if err != nil || r.Version != "1.10.2" {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	for _, pair := range [][2]string{{"26.3", "26.2.9"}, {"1.10", "1.9.999"}, {"2026.10", "2026.9"}, {"1.2.3+261008", "1.2.3+261007"}} {
		if Compare(pair[0], pair[1]) <= 0 {
			t.Fatal(pair)
		}
	}
	if Compare("5.3", "5.3.0") != 0 {
		t.Fatal("missing patch must be zero")
	}
}

func TestPaginationAndNonVersionTags(t *testing.T) {
	var page []map[string]string
	for range 100 {
		page = append(page, map[string]string{"name": "development"})
	}
	data, _ := json.Marshal(page)
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/tags?per_page=100&page=1": string(data),
		"https://api.github.com/repos/a/b/tags?per_page=100&page=2": `[{"name":"releases/solr/10.1.0"}]`,
	})
	r, err := c.Discover(context.Background(), Policy{Source: "github-tags", Repo: "a/b", Prefix: "releases/solr/"}, "10.0.0")
	if err != nil || r.Version != "10.1.0" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestNamedCompatibilityStreamStillTracksNewStablePatches(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/tags?per_page=100&page=1": `[{"name":"2026.09.0"},{"name":"5.0.0"},{"name":"4.40.0"},{"name":"4.4.44-rc1"},{"name":"4.4.43"},{"name":"4.4.42"}]`,
	})
	r, err := c.Discover(context.Background(), Policy{Source: "github-tags", Repo: "a/b", Series: "4.4"}, "4.4.42")
	if err != nil || r.Version != "4.4.43" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRegistriesAndMinecraft(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://pypi.org/pypi/app/json":                                  `{"releases":{"1.9.0":[{}],"1.10.0":[{}],"2.0.0":[{"yanked":true}],"3.0.0rc1":[{}],"4.0.0":[]}}`,
		"https://registry.npmjs.org/-/package/app/dist-tags":              `{"latest":"1.10.0","next":"2.0.0-beta.1"}`,
		"https://piston-meta.mojang.com/mc/game/version_manifest_v2.json": `{"latest":{"release":"26.3","snapshot":"26.4-snapshot-1"},"versions":[{"id":"26.4-snapshot-1","type":"snapshot","url":"https://example.org/snapshot"},{"id":"26.3","type":"release","url":"https://example.org/release"}]}`,
		"https://example.org/release":                                     `{"id":"26.3","type":"release","downloads":{"server":{"url":"https://example.org/server.jar"}}}`,
	})
	for _, source := range []string{"pypi", "npm", "minecraft"} {
		r, err := c.Discover(context.Background(), Policy{Source: source, Project: "app"}, "1.0")
		want := "1.10.0"
		if source == "minecraft" {
			want = "26.3"
		}
		if err != nil || r.Version != want {
			t.Fatalf("%s %+v %v", source, r, err)
		}
	}
}

func TestGitHubLatestRejectsDraftsAndPreviews(t *testing.T) {
	for _, item := range []string{`{"tag_name":"v2.0.0","draft":true}`, `{"tag_name":"v2.0.0","prerelease":true}`, `{"tag_name":"v2.0.0rc1"}`} {
		c := mockClient(t, map[string]string{"https://api.github.com/repos/a/b/releases/latest": item})
		if _, err := c.Discover(context.Background(), Policy{Source: "github", Repo: "a/b", Prefix: "v"}, "1.0.0"); err == nil {
			t.Fatal("unstable release accepted", item)
		}
	}
}

func TestChildProcessesDoNotReceiveCredentials(t *testing.T) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "PACKAGES_APK_SIGNING_KEY"} {
		t.Setenv(name, "private-sentinel")
	}
	err := RunCommand(context.Background(), t.TempDir(), "sh", "-c", `test -z "$GH_TOKEN$GITHUB_TOKEN$AWS_ACCESS_KEY_ID$AWS_SECRET_ACCESS_KEY$PACKAGES_APK_SIGNING_KEY" && test "$npm_config_ignore_scripts" = true && test "$UV_PYTHON_DOWNLOADS" = never`)
	if err != nil {
		t.Fatal(err)
	}
}

const fetchRecipe = `# Preserve this recipe's comments and scripts.
package:
  name: example
  version: 1.9.0
  epoch: 7
pipeline:
  - uses: fetch
    with:
      uri: https://example.org/${{package.version}}/arm64.tar.gz
      expected-sha256: old-arm
  - pipeline:
      - uses: fetch
        with:
          uri: https://example.org/${{package.version}}/amd64.tar.gz
          expected-sha256: old-amd
  - runs: |
      echo do-not-change
`

func TestAllSourcePinsAndEpochReset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "example", "recipe.yaml")
	mustWrite(t, path, fetchRecipe)
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/releases/latest": `{"tag_name":"v1.10.0"}`,
		"https://example.org/1.10.0/arm64.tar.gz":          "arm",
		"https://example.org/1.10.0/amd64.tar.gz":          "amd",
	})
	r := c.Update(context.Background(), root, "example", Policy{Source: "github", Repo: "a/b", Prefix: "v"}, true, noCommands)
	if r.Status != "updated" {
		t.Fatalf("%+v", r)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`version: "1.10.0"`, "epoch: 0", hash("arm"), hash("amd"), "# Preserve this recipe's comments and scripts.", "      echo do-not-change"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %s", want, data)
		}
	}
	var parsed struct{ Package struct{ Epoch uint64 } }
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestFailureLeavesContextUntouched(t *testing.T) {
	for _, failure := range []string{"download", "resolver", "parser"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "example", "recipe.yaml")
			mustWrite(t, path, fetchRecipe)
			mustWrite(t, filepath.Join(root, "example", "package-lock.json"), "old-lock")
			routes := map[string]string{"https://registry.npmjs.org/-/package/example/dist-tags": `{"latest":"1.10.0"}`, "https://example.org/1.10.0/arm64.tar.gz": "arm"}
			if failure != "download" {
				routes["https://example.org/1.10.0/amd64.tar.gz"] = "amd"
			}
			c := mockClient(t, routes)
			run := func(_ context.Context, dir, name string, _ ...string) error {
				mustWrite(t, filepath.Join(dir, "package-lock.json"), "new-lock")
				if (failure == "resolver" && name == "npm") || (failure == "parser" && name == "melange") {
					return errors.New("deliberate failure")
				}
				return nil
			}
			r := c.Update(context.Background(), root, "example", Policy{Source: "npm", Project: "example", Lock: "npm"}, true, run)
			if r.Status != "failed" {
				t.Fatalf("%+v", r)
			}
			data, _ := os.ReadFile(path)
			lock, _ := os.ReadFile(filepath.Join(root, "example", "package-lock.json"))
			if string(data) != fetchRecipe || string(lock) != "old-lock" {
				t.Fatal("partial update escaped scratch directory")
			}
		})
	}
}

func TestGitCommitAndUnversionedContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "example", "recipe.yaml")
	mustWrite(t, path, "package:\n  name: example\n  version: 1.0.0\n  epoch: 2\npipeline:\n  - uses: git-checkout\n    with:\n      repository: https://github.com/a/b\n      tag: v${{package.version}}\n      expected-commit: "+strings.Repeat("a", 40)+"\n")
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/releases/latest": `{"tag_name":"v1.1.0"}`,
		"https://api.github.com/repos/a/b/commits/v1.1.0":  `{"sha":"` + strings.Repeat("b", 40) + `"}`,
		"https://example.org/model":                        "model",
	})
	r := c.Update(context.Background(), root, "example", Policy{Source: "github", Repo: "a/b", Prefix: "v"}, true, noCommands)
	if r.Status != "updated" {
		t.Fatalf("%+v", r)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), strings.Repeat("b", 40)) {
		t.Fatal("tag was not dereferenced")
	}
	mustWrite(t, path, "package:\n  name: example\n  version: 1.0.0\n  epoch: 2\npipeline:\n  - uses: fetch\n    with:\n      uri: https://example.org/model\n      expected-sha256: old\n")
	r = c.Update(context.Background(), root, "example", Policy{Source: "content"}, true, noCommands)
	if r.Status != "updated" {
		t.Fatalf("%+v", r)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "epoch: 3") {
		t.Fatalf("%s", data)
	}
	r = c.Update(context.Background(), root, "example", Policy{Source: "content"}, true, noCommands)
	if r.Status != "current" {
		t.Fatal("unchanged model updated again", r)
	}
}

func TestDryRunPinnedAndDowngrade(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "example", "recipe.yaml")
	mustWrite(t, path, fetchRecipe)
	c := mockClient(t, map[string]string{"https://registry.npmjs.org/-/package/example/dist-tags": `{"latest":"1.10.0"}`})
	run := func(context.Context, string, string, ...string) error { t.Fatal("unexpected command"); return nil }
	r := c.Update(context.Background(), root, "example", Policy{Source: "npm", Project: "example"}, false, run)
	if r.Status != "available" {
		t.Fatalf("%+v", r)
	}
	data, _ := os.ReadFile(path)
	if string(data) != fetchRecipe {
		t.Fatal("dry run wrote files")
	}
	r = c.Update(context.Background(), root, "does-not-exist", Policy{Source: "pinned"}, true, run)
	if r.Status != "pinned" {
		t.Fatalf("%+v", r)
	}
	c = mockClient(t, map[string]string{"https://registry.npmjs.org/-/package/example/dist-tags": `{"latest":"1.8.0"}`})
	r = c.Update(context.Background(), root, "example", Policy{Source: "npm", Project: "example"}, true, run)
	if r.Status != "failed" {
		t.Fatal("downgrade accepted", r)
	}
}

func TestRepositoryCoverage(t *testing.T) {
	policies, err := LoadPolicies("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 32 {
		t.Logf("tracking %d contexts", len(policies))
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "new-package", "recipe.yaml"), "package: {}")
	mustWrite(t, filepath.Join(root, "updates.json"), "{}")
	if _, err := LoadPolicies(root); err == nil {
		t.Fatal("untracked package accepted")
	}
	mustWrite(t, filepath.Join(root, "updates.json"), `{"new-package":{"source":"pinned","reason":"forgot to track it"}}`)
	if _, err := LoadPolicies(root); err == nil {
		t.Fatal("production pin accepted")
	}
}

func TestComponentUpdateIsTransactionalAndBumpsParentEpoch(t *testing.T) {
	for _, outcome := range []string{"success", "download", "parser", "downgrade", "dry-run"} {
		t.Run(outcome, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "example", "recipe.yaml")
			text := fetchRecipe + "vars:\n  web-version: 1.0.0\nsubpackages:\n  - name: web\n    pipeline:\n      - uses: fetch\n        with:\n          uri: https://example.org/web-${{vars.web-version}}.tar.gz\n          expected-sha256: old-web\n"
			mustWrite(t, path, text)
			latest := "1.1.0"
			if outcome == "downgrade" {
				latest = "0.9.0"
			}
			routes := map[string]string{
				"https://api.github.com/repos/a/b/releases/latest":   `{"tag_name":"1.9.0"}`,
				"https://api.github.com/repos/a/web/releases/latest": `{"tag_name":"` + latest + `"}`,
			}
			if outcome != "download" {
				routes["https://example.org/web-1.1.0.tar.gz"] = "web"
			}
			p := Policy{Source: "github", Repo: "a/b", Lock: "npm", Components: map[string]Policy{"web-version": {Source: "github", Repo: "a/web"}}}
			run := func(_ context.Context, _, command string, _ ...string) error {
				if command != "melange" {
					t.Fatalf("component-only update ran %s", command)
				}
				if outcome == "parser" {
					return errors.New("invalid recipe")
				}
				return nil
			}
			result := mockClient(t, routes).Update(context.Background(), root, "example", p, outcome != "dry-run", run)
			data, _ := os.ReadFile(path)
			if outcome == "success" {
				if result.Status != "updated" || !strings.Contains(string(data), "epoch: 8") || !strings.Contains(string(data), hash("web")) || !strings.Contains(string(data), "expected-sha256: old-arm") {
					t.Fatalf("%+v\n%s", result, data)
				}
				if again := mockClient(t, routes).Update(context.Background(), root, "example", p, true, run); again.Status != "current" {
					t.Fatalf("repeat: %+v", again)
				}
			} else {
				want := "failed"
				if outcome == "dry-run" {
					want = "available"
				}
				if result.Status != want || string(data) != text {
					t.Fatalf("partial update: %+v\n%s", result, data)
				}
			}
		})
	}
}

func TestWheelHashAndRequirementBlocks(t *testing.T) {
	c := mockClient(t, map[string]string{
		"https://pypi.org/pypi/lfx/1.0.0/json": `{"info":{"version":"1.0.0"},"urls":[{"filename":"lfx-1.0.0-py3-none-any.whl","url":"https://example.org/wheel","digests":{"sha256":"wrong"}}]}`,
		"https://example.org/wheel":            "bytes",
	})
	if _, err := c.wheel(context.Background(), "lfx", "1.0.0"); err == nil {
		t.Fatal("altered wheel accepted")
	}
	a, b := splitRequirements("first==1 \\\n    --hash=sha256:a\nfastuuid==2 \\\n    --hash=sha256:b\nlast==3\n", map[string]bool{"fastuuid": true})
	if strings.Contains(a, "fastuuid") || !strings.Contains(b, "--hash=sha256:b") || strings.Contains(b, "last") {
		t.Fatalf("%q %q", a, b)
	}
}

func TestLangflowMovesWheelURLsCommandsAndLocksTogether(t *testing.T) {
	dir := t.TempDir()
	text := "package:\n  name: langflow\n  version: 1.13.0\n  epoch: 0\npipeline:\n"
	routes := map[string]string{}
	for _, name := range []string{"langflow", "langflow-base", "lfx"} {
		stem := strings.ReplaceAll(name, "-", "_")
		text += fmt.Sprintf("  - uses: fetch\n    with:\n      uri: https://example.org/%s-1.12.6-py3-none-any.whl\n      expected-sha256: old\n", stem)
		address := "https://example.org/" + stem + "-1.13.0-py3-none-any.whl"
		routes["https://pypi.org/pypi/"+name+"/1.13.0/json"] = fmt.Sprintf(`{"info":{"version":"1.13.0"},"urls":[{"filename":"%s-1.13.0-py3-none-any.whl","url":"%s","digests":{"sha256":"%s"}}]}`, stem, address, hash(name))
		routes[address] = name
	}
	text += "  - runs: |\n      pip install langflow-1.12.6-py3-none-any.whl langflow_base-1.12.6-py3-none-any.whl lfx-1.12.6-py3-none-any.whl\n"
	path := filepath.Join(dir, "langflow.yaml")
	mustWrite(t, path, text)
	c := mockClient(t, routes)
	run := func(_ context.Context, dir, command string, args ...string) error {
		joined := strings.Join(args, " ")
		if command != "uv" || !strings.Contains(joined, "--no-emit-package onnxruntime") || !strings.Contains(joined, "--no-emit-package ibm-db") {
			t.Fatalf("unsafe resolver: %s %s", command, joined)
		}
		mustWrite(t, filepath.Join(dir, "requirements.txt"), "langflow==1.13.0 \\\n    --hash=sha256:a\nlangflow-base==1.13.0 \\\n    --hash=sha256:b\nlfx==1.13.0 \\\n    --hash=sha256:c\nfastuuid==0.14.0 \\\n    --hash=sha256:d\nother==2 \\\n    --hash=sha256:e\n")
		return nil
	}
	if err := c.locks(context.Background(), dir, Policy{Lock: "langflow"}, Release{Version: "1.13.0"}, run); err != nil {
		t.Fatal(err)
	}
	inputs, err := os.ReadFile(filepath.Join(dir, "requirements.in"))
	if err != nil || !strings.Contains(string(inputs), "\nchromadb\n") {
		t.Fatalf("embedding dependency lost during update: %s (%v)", inputs, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "1.12.6") {
		t.Fatalf("stale wheel pin: %s", data)
	}
	for _, name := range []string{"langflow", "langflow-base", "lfx"} {
		if !strings.Contains(string(data), hash(name)) {
			t.Fatal("missing new hash", name)
		}
	}
	lock, _ := os.ReadFile(filepath.Join(dir, "requirements.txt"))
	uuid, _ := os.ReadFile(filepath.Join(dir, "fastuuid-requirements.txt"))
	if strings.Contains(string(lock), "langflow==") || strings.Contains(string(lock), "fastuuid==") || !strings.Contains(string(lock), "other==2") || !strings.Contains(string(uuid), "--hash=sha256:d") || strings.Contains(string(uuid), "lfx==") {
		t.Fatalf("locks: %s / %s", lock, uuid)
	}
}

func TestCategorizedUpdatesAndExplicitTrainingHold(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "wolfi", ".keep"), "")
	mustWrite(t, filepath.Join(root, "www", "example", "metadata.json"), `{"update":{"source":"github","repo":"a/b","prefix":"v"}}`)
	path := filepath.Join(root, "www", "example", "recipe.yaml")
	mustWrite(t, path, fetchRecipe)
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/a/b/releases/latest": `{"tag_name":"v1.10.0"}`,
		"https://example.org/1.10.0/arm64.tar.gz":          "arm", "https://example.org/1.10.0/amd64.tar.gz": "amd",
	})
	policies, err := LoadPolicies(root)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Update(context.Background(), root, "example", policies["example"], true, noCommands)
	if r.Status != "updated" {
		t.Fatalf("%+v", r)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "1.10.0") {
		t.Fatal("categorized context did not update")
	}
	if _, err := os.Stat(filepath.Join(root, "example")); !os.IsNotExist(err) {
		t.Fatal("flat context unexpectedly created")
	}
	c = mockClient(t, nil)
	r = c.Update(context.Background(), root, "example-vuln-demo", Policy{Source: "github", Repo: "a/b", Hold: "Training fixture"}, true, noCommands)
	if r.Status != "pinned" {
		t.Fatalf("training hold accessed upstream: %+v", r)
	}
}

func TestPlexUsesTransactionalVendorPreparation(t *testing.T) {
	for _, failure := range []bool{false, true} {
		root := t.TempDir()
		path := filepath.Join(root, "plex", "plex.yaml")
		original := "package:\n  name: plexmediaserver\n  version: 1.0.0\n  epoch: 0\n"
		mustWrite(t, path, original)
		mustWrite(t, filepath.Join(root, "plex", "sources.json"), `{"version":"1.0.0-old"}`)
		c := mockClient(t, map[string]string{"https://repo.plex.tv/deb/dists/public/main/binary-amd64/Packages": "Package: plexmediaserver\nVersion: 1.1.0-new\n"})
		calls := 0
		run := func(_ context.Context, dir, name string, _ ...string) error {
			calls++
			if name == "python3" {
				mustWrite(t, filepath.Join(dir, "plex.yaml"), strings.Replace(original, "1.0.0", "1.1.0", 1))
				if failure {
					return errors.New("codec ABI changed")
				}
			}
			return nil
		}
		r := c.Update(context.Background(), root, "plex", Policy{Source: "plex"}, true, run)
		data, _ := os.ReadFile(path)
		if failure {
			if r.Status != "failed" || string(data) != original {
				t.Fatalf("failed vendor preparation escaped scratch: %+v", r)
			}
		} else if r.Status != "updated" || !strings.Contains(string(data), "1.1.0") || calls != 2 {
			t.Fatalf("%+v", r)
		}
	}
}

func TestUnderscoreReleaseTagsKeepTheExactSourcePin(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "example", "recipe.yaml")
	mustWrite(t, path, "package:\n  name: example\n  version: 8.23.0\n  epoch: 0\npipeline:\n  - uses: git-checkout\n    with:\n      repository: https://github.com/curl/curl.git\n      tag: curl-8_23_0\n      expected-commit: "+strings.Repeat("a", 40)+"\n")
	c := mockClient(t, map[string]string{
		"https://api.github.com/repos/curl/curl/releases/latest":     `{"tag_name":"curl-8_24_0"}`,
		"https://api.github.com/repos/curl/curl/commits/curl-8_24_0": `{"sha":"` + strings.Repeat("b", 40) + `"}`,
	})
	r := c.Update(context.Background(), root, "example", Policy{Source: "github", Repo: "curl/curl", Prefix: "curl-", Separator: "_"}, true, noCommands)
	data, _ := os.ReadFile(path)
	if r.Status != "updated" || !strings.Contains(string(data), `version: "8.24.0"`) || !strings.Contains(string(data), `tag: "curl-8_24_0"`) || !strings.Contains(string(data), strings.Repeat("b", 40)) {
		t.Fatalf("%+v\n%s", r, data)
	}
}
