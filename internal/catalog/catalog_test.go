package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryCatalog(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 {
		t.Fatal("no packages")
	}
	for _, pkg := range packages {
		if pkg.Engine == "wolfi" && pkg.Update.Upstream == nil {
			t.Errorf("%s has no upstream release source", pkg.Name)
		}
	}
}

func TestEvidence(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "proof"), []byte("proof"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := evidence(directory, []string{"proof"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("proof", filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../proof", "link", "missing"} {
		if err := evidence(directory, []string{path}); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
}

func TestGitTagsUpstream(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "libcap")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"overlay.yaml", "policy.json", "proof"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	valid := func(upstream Policy) Metadata {
		var m Metadata
		yes := true
		m.Update = Policy{Source: "wolfi", Recipe: "libcap", Upstream: &upstream}
		m.Hardening.Minimized, m.Hardening.Pledge = &yes, &yes
		m.Hardening.MinimizedReason, m.Hardening.PledgeReason = "reason", "reason"
		m.Hardening.Evidence = []string{"proof"}
		m.Wolfi.Relationship, m.Wolfi.Recipes, m.Wolfi.RemovalCondition = "derived", []string{"libcap"}, "never"
		m.Wolfi.Changes = append(m.Wolfi.Changes, struct {
			What     string   `json:"what"`
			Why      string   `json:"why"`
			Evidence []string `json:"evidence"`
		}{"what", "why", []string{"proof"}})
		return m
	}
	url := "https://git.kernel.org/pub/scm/libs/libcap/libcap.git"
	if err := validate(directory, valid(Policy{Source: "git-tags", URL: url, Prefix: "v1."})); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Policy{
		{Source: "git-tags"},
		{Source: "git-tags", URL: "http://git.kernel.org/libcap.git"},
		{Source: "git-tags", URL: "https://git.kernel.org/libcap.git --upload-pack=x"},
		{Source: "git-tags", URL: url, Repo: "kernel/libcap"},
		{Source: "github-tags", Repo: "kernel/libcap", URL: url},
	} {
		if err := validate(directory, valid(bad)); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	m := valid(Policy{Source: "git-tags", URL: url})
	m.Update.URL = url
	if err := validate(directory, m); err == nil {
		t.Error("accepted a URL outside the upstream policy")
	}
}
