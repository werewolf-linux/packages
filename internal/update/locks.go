package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type distribution struct {
	Info struct {
		Version string `json:"version"`
	}
	URLs []struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Yanked   bool   `json:"yanked"`
		Digests  struct {
			SHA256 string `json:"sha256"`
		} `json:"digests"`
	} `json:"urls"`
}

func (c *Client) wheel(ctx context.Context, project, version string) (Release, error) {
	var data distribution
	if err := c.json(ctx, "https://pypi.org/pypi/"+project+"/"+version+"/json", &data); err != nil {
		return Release{}, err
	}
	if data.Info.Version != version {
		return Release{}, fmt.Errorf("PyPI returned another version")
	}
	for _, file := range data.URLs {
		if file.Yanked || !strings.HasSuffix(file.Filename, "-py3-none-any.whl") {
			continue
		}
		digest, err := c.Download(ctx, file.URL, io.Discard)
		if err != nil {
			return Release{}, err
		}
		if digest != file.Digests.SHA256 {
			return Release{}, fmt.Errorf("PyPI wheel checksum mismatch: %s", file.Filename)
		}
		return Release{Version: version, URL: file.URL, SHA256: digest}, nil
	}
	return Release{}, fmt.Errorf("no non-yanked universal wheel for %s %s", project, version)
}

func (c *Client) locks(ctx context.Context, dir string, p Policy, release Release, run Runner) error {
	switch p.Lock {
	case "":
		return nil
	case "npm":
		// npm resolves metadata only. Lifecycle scripts and package installation
		// stay disabled even if an upstream dependency requests them.
		return run(ctx, dir, "npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund", "--save-exact", p.Project+"@"+release.Version)
	case "bugsink":
		if err := os.WriteFile(filepath.Join(dir, "requirements.in"), []byte("bugsink=="+release.Version+"\n"), 0o644); err != nil {
			return err
		}
		return run(ctx, dir, "uv", "pip", "compile", "--no-build", "--python-version", "3.15", "--python-platform", "linux", "--generate-hashes", "--no-header", "--output-file", "requirements.txt", "requirements.in")
	case "langflow":
		// Resolve the release's dependency metadata, including the exact
		// langflow-base and lfx it requires, against our interpreter overrides.
		if err := os.WriteFile(filepath.Join(dir, "requirements.in"), []byte("langflow=="+release.Version+"\n"), 0o644); err != nil {
			return err
		}
		// Resolve against upstream's supported Python range. The recipe
		// deliberately builds for 3.15 with our overrides and import checks.
		if err := run(ctx, dir, "uv", "pip", "compile", "--no-build", "--python-version", "3.14", "--python-platform", "linux", "--generate-hashes", "--no-header", "--override", "overrides.txt", "--no-emit-package", "onnxruntime", "--output-file", "requirements.txt", "requirements.in"); err != nil {
			return err
		}
		lockPath := filepath.Join(dir, "requirements.txt")
		lock, err := os.ReadFile(lockPath)
		if err != nil {
			return err
		}
		versions := map[string]string{}
		for _, name := range []string{"langflow", "langflow-base", "lfx", "fastuuid"} {
			m := regexp.MustCompile(`(?m)^` + name + `==([^\s]+)`).FindStringSubmatch(string(lock))
			if len(m) != 2 {
				return fmt.Errorf("resolved lock lacks %s", name)
			}
			versions[name] = m[1]
		}
		paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if len(paths) != 1 {
			return fmt.Errorf("missing Langflow recipe")
		}
		r, err := readRecipe(paths[0])
		if err != nil {
			return err
		}
		for _, project := range []string{"langflow", "langflow-base", "lfx"} {
			wheel, err := c.wheel(ctx, project, versions[project])
			if err != nil {
				return err
			}
			stem := strings.ReplaceAll(project, "-", "_")
			found := false
			err = walk(r.root, func(n *yaml.Node) error {
				if value(n, "uses") != "fetch" {
					return nil
				}
				with := field(n, "with")
				uri := field(with, "uri")
				if uri == nil || !strings.HasPrefix(filepath.Base(uri.Value), stem+"-") {
					return nil
				}
				found = true
				old := filepath.Base(uri.Value)
				if err := r.set(uri, wheel.URL); err != nil {
					return err
				}
				if err := r.set(field(with, "expected-sha256"), wheel.SHA256); err != nil {
					return err
				}
				// Change the filename in the pip command as well as the fetch.
				r.text = strings.ReplaceAll(r.text, old, filepath.Base(wheel.URL))
				return nil
			})
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("missing wheel fetch: %s", project)
			}
		}
		if err := os.WriteFile(r.path, []byte(r.render()), 0o644); err != nil {
			return err
		}
		// These three wheels are installed separately. fastuuid is built with
		// its narrowly scoped PyO3 forward-ABI setting before this lock.
		mainLock, uuidLock := splitRequirements(string(lock), map[string]bool{"langflow": true, "langflow-base": true, "lfx": true, "fastuuid": true})
		_, uuidLock = splitRequirements(uuidLock, map[string]bool{"fastuuid": true})
		if err := os.WriteFile(lockPath, []byte(mainLock), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "fastuuid-requirements.txt"), []byte(uuidLock), 0o644)
	}
	return fmt.Errorf("unknown lock handler %s", p.Lock)
}

// splitRequirements preserves complete multi-line pins and their hashes.
func splitRequirements(text string, selected map[string]bool) (rest, chosen string) {
	var a, b strings.Builder
	include := false
	name := regexp.MustCompile(`^([A-Za-z0-9_.-]+)==`)
	for _, line := range strings.SplitAfter(text, "\n") {
		if m := name.FindStringSubmatch(line); len(m) == 2 {
			include = selected[m[1]]
		}
		if include {
			b.WriteString(line)
		} else {
			a.WriteString(line)
		}
	}
	return a.String(), b.String()
}
