package packaging

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/werewolf-linux/packages/internal/catalog"
	"gopkg.in/yaml.v3"
)

var inputID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func packageBuildRoot(pkg Package) string { return filepath.Join(pkg.Root, "build/packages", pkg.Name) }

// definitionDigest excludes explanatory records and YAML presentation. Changes to
// executable actions, tests, source selection within recipes or derivation policy
// remain inputs. The complete source file is retained separately in provenance.
func definitionDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return definitionDataDigest(data, false)
}

func definitionDataDigest(data []byte, includeUpdate bool) (string, error) {
	var fields map[string]any
	if err := yaml.Unmarshal(data, &fields); err != nil {
		return "", err
	}
	relevant := map[string]any{}
	if includeUpdate {
		relevant["update"] = fields["update"]
	}
	for _, key := range []string{"melange", "wolfi", "actions", "test"} {
		if value, ok := fields[key]; ok {
			relevant[key] = value
		}
	}
	if wolfi, ok := relevant["wolfi"].(map[string]any); ok {
		delete(wolfi, "title")
	}
	data, err := json.Marshal(relevant)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// InputID binds a direct recipe to its build inputs and framework implementation.
func InputID(pkg Package) (string, error) {
	hashes, err := policyHashes(pkg)
	if err != nil {
		return "", err
	}
	_, modes, err := hashFiles(pkg.Directory, excludedInput)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Hashes map[string]string
		Modes  map[string]uint32
	}{hashes, modes})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func excludedInput(path string) bool {
	first := strings.Split(filepath.ToSlash(path), "/")[0]
	return first == "README.md" || first == "docs" || first == "__pycache__" || strings.HasSuffix(path, ".pyc")
}

func selectBuild(pkg *Package) error {
	base := packageBuildRoot(*pkg)
	if pkg.Engine != "wolfi" {
		id, err := InputID(*pkg)
		if err != nil {
			return err
		}
		pkg.Build = filepath.Join(base, id)
		return nil
	}
	data, err := os.ReadFile(filepath.Join(base, "current"))
	if os.IsNotExist(err) {
		pkg.Build = filepath.Join(base, "pending")
		return nil
	}
	if err != nil {
		return err
	}
	id := strings.TrimSpace(string(data))
	if !inputID.MatchString(id) {
		return fmt.Errorf("invalid captured input ID for %s", pkg.Name)
	}
	pkg.Build = filepath.Join(base, id)
	return nil
}

func selectCaptured(pkg Package, id string) error {
	if !inputID.MatchString(id) {
		return fmt.Errorf("invalid captured input identity")
	}
	return writeFile(filepath.Join(packageBuildRoot(pkg), "current"), []byte(id+"\n"), 0644)
}

// NativeWorkspace gives each architecture its own mutable context. The capture
// shared by both jobs remains immutable; copies are verified before use.
func NativeWorkspace(pkg Package, arch string) (Package, error) {
	if err := ensureBuildRoot(pkg.Root); err != nil {
		return pkg, err
	}
	if arch != "x86_64" && arch != "aarch64" {
		return pkg, fmt.Errorf("unsupported architecture %q", arch)
	}
	if pkg.Engine != "wolfi" {
		return pkg, nil
	}
	if _, err := VerifyInputs(pkg); err != nil {
		return pkg, err
	}
	native := pkg
	native.Build = filepath.Join(pkg.Build, arch)
	if _, err := os.Stat(filepath.Join(native.Build, "inputs")); os.IsNotExist(err) {
		if err := os.MkdirAll(native.Build, 0755); err != nil {
			return pkg, err
		}
		stage, err := os.MkdirTemp(native.Build, ".inputs-")
		if err != nil {
			return pkg, err
		}
		defer os.RemoveAll(stage)
		if err := copyTree(filepath.Join(pkg.Build, "inputs"), filepath.Join(stage, "inputs")); err != nil {
			return pkg, err
		}
		if err := os.Rename(filepath.Join(stage, "inputs"), filepath.Join(native.Build, "inputs")); err != nil {
			return pkg, err
		}
	} else if err != nil {
		return pkg, err
	}
	_, err := VerifyInputs(native)
	return native, err
}

// Paths is the machine-readable contract between the CLI and workflows.
func Paths(pkg Package, arch string) map[string]string {
	work := filepath.Join(pkg.Build, arch)
	artifacts := filepath.Join(work, "packages")
	if pkg.Engine != "wolfi" {
		artifacts = filepath.Join(artifacts, arch)
	}
	return map[string]string{"context": pkg.Directory, "build": pkg.Build, "inputs": filepath.Join(pkg.Build, "inputs"), "work": work, "artifacts": artifacts, "logs": filepath.Join(work, "logs")}
}

// Render produces native Melange YAML from the currently selected inputs.
func Render(pkg Package) ([]byte, error) {
	if pkg.Engine != "wolfi" {
		return pkg.Config.Recipe()
	}
	recipe, err := Derive(pkg)
	if err != nil {
		return nil, err
	}
	return catalog.YAML(recipe)
}

func ensureBuildRoot(root string) error {
	path := filepath.Join(root, "build/go.mod")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFile(path, []byte("module werewolf.generated\n\ngo 1.27.0\n"), 0644)
}
