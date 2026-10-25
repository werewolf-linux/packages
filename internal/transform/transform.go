// Package transform applies package-specific file changes described in YAML.
package transform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/werewolf-linux/packages/internal/probe"
	"gopkg.in/yaml.v3"
)

// Plan groups changes by build phase so each action has an explicit root.
type Plan struct {
	Actions map[string]Action `yaml:"actions"`
}
type Action struct {
	Glibc        *probe.GlibcOptions  `yaml:"glibc,omitempty"`
	Locales      *probe.LocaleOptions `yaml:"locales,omitempty"`
	Curl         *probe.CurlOptions   `yaml:"curl,omitempty"`
	Files        []File               `yaml:"files,omitempty"`
	Requirements *Requirements        `yaml:"requirements,omitempty"`
	Wheels       []Wheel              `yaml:"wheels,omitempty"`
}
type File struct {
	Path            string            `yaml:"path"`
	Optional        bool              `yaml:"optional,omitempty"`
	WhenContains    string            `yaml:"when-contains,omitempty"`
	RequireContains string            `yaml:"require-contains,omitempty"`
	Replace         []Replacement     `yaml:"replace,omitempty"`
	Qualify         map[string]string `yaml:"qualify,omitempty"`
}
type Replacement struct {
	Old   string `yaml:"old"`
	New   string `yaml:"new"`
	Count int    `yaml:"count,omitempty"`
}
type Requirements struct {
	Input            string        `yaml:"input"`
	Output           string        `yaml:"output"`
	Omit             []string      `yaml:"omit"`
	OmitPrefixes     []string      `yaml:"omit-prefixes,omitempty"`
	Required         []string      `yaml:"required,omitempty"`
	RequiredPrefixes []string      `yaml:"required-prefixes,omitempty"`
	Replace          []Replacement `yaml:"replace,omitempty"`
}
type Wheel struct {
	Distribution string   `yaml:"distribution"`
	Remove       []string `yaml:"remove-requirements"`
	Records      []string `yaml:"record-files,omitempty"`
}

// Read rejects misspelled fields before any package file is changed.
func Read(path string) (Plan, error) {
	var plan Plan
	file, err := os.Open(path)
	if err != nil {
		return plan, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return plan, fmt.Errorf("expected one transformation plan")
	}
	if len(plan.Actions) == 0 {
		return plan, fmt.Errorf("plan has no actions")
	}
	for name, action := range plan.Actions {
		probes := 0
		for _, present := range []bool{action.Curl != nil, action.Glibc != nil, action.Locales != nil} {
			if present {
				probes++
			}
		}
		edits := len(action.Files) != 0 || action.Requirements != nil || len(action.Wheels) != 0
		if probes > 1 || probes == 1 && edits || probes == 0 && !edits {
			return plan, fmt.Errorf("action %q must select one probe or file transformations", name)
		}
	}
	return plan, nil
}

// Apply checks an entire action before writing its changed files.
func Apply(plan Plan, action, root string) error {
	selected, ok := plan.Actions[action]
	if !ok {
		return fmt.Errorf("unknown action %q", action)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	changes := map[string][]byte{}
	for _, edit := range selected.Files {
		if edit.Optional && filepath.IsLocal(edit.Path) {
			if _, err := os.Lstat(filepath.Join(root, edit.Path)); os.IsNotExist(err) {
				continue
			}
		}
		path, err := localPath(root, edit.Path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if edit.Optional && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		text := string(data)
		if edit.WhenContains != "" && !strings.Contains(text, edit.WhenContains) {
			continue
		}
		if edit.RequireContains != "" && !strings.Contains(text, edit.RequireContains) {
			return fmt.Errorf("%s: required upstream text changed", edit.Path)
		}
		for _, replacement := range edit.Replace {
			var err error
			text, err = replace(text, replacement)
			if err != nil {
				return fmt.Errorf("%s: %w", edit.Path, err)
			}
		}
		for identifier, qualified := range edit.Qualify {
			text = qualify(text, identifier, qualified)
		}
		changes[edit.Path] = []byte(text)
	}
	if selected.Requirements != nil {
		spec := selected.Requirements
		path, err := localPath(root, spec.Input)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data, err = filterRequirements(data, *spec)
		if err != nil {
			return err
		}
		changes[spec.Output] = data
	}
	for _, wheel := range selected.Wheels {
		if err := wheelChanges(root, wheel, changes); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(changes))
	for name := range changes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		path, err := localPath(root, name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".package-edit-")
		if err != nil {
			return err
		}
		if err := file.Chmod(mode); err != nil {
			file.Close()
			os.Remove(file.Name())
			return err
		}
		_, writeErr := file.Write(changes[name])
		closeErr := file.Close()
		if writeErr != nil {
			os.Remove(file.Name())
			return writeErr
		}
		if closeErr != nil {
			os.Remove(file.Name())
			return closeErr
		}
		if err := os.Rename(file.Name(), path); err != nil {
			os.Remove(file.Name())
			return err
		}
		slog.Info("updated package input", "action", action, "file", name)
	}
	return nil
}

func localPath(root, name string) (string, error) {
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("unsafe package file path %q", name)
	}
	path := filepath.Join(root, name)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative != "." && !filepath.IsLocal(relative) {
		return "", fmt.Errorf("file escapes package root: %s", name)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlinked package file: %s", name)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func replace(text string, replacement Replacement) (string, error) {
	count := replacement.Count
	if count == 0 {
		count = 1
	}
	if replacement.Old == "" || count < 0 || strings.Count(text, replacement.Old) != count {
		return "", fmt.Errorf("expected %d occurrences of %q", count, replacement.Old)
	}
	return strings.ReplaceAll(text, replacement.Old, replacement.New), nil
}

func qualify(text, identifier, qualified string) string {
	token := identifier + "["
	var result strings.Builder
	for {
		at := strings.Index(text, token)
		if at < 0 {
			result.WriteString(text)
			break
		}
		before, _ := utf8.DecodeLastRuneInString(text[:at])
		if at == 0 || before != '.' && before != '_' && !unicode.IsLetter(before) && !unicode.IsDigit(before) {
			result.WriteString(text[:at])
			result.WriteString(qualified + "[")
		} else {
			result.WriteString(text[:at+len(token)])
		}
		text = text[at+len(token):]
	}
	return result.String()
}

var requirement = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*`)

func filterRequirements(data []byte, spec Requirements) ([]byte, error) {
	lines := strings.SplitAfter(string(data), "\n")
	var kept strings.Builder
	seen := map[string]bool{}
	omit := false
	for _, line := range lines {
		if name := requirement.FindString(line); name != "" {
			name = strings.ToLower(name)
			omit = slices.Contains(spec.Omit, name)
			for _, prefix := range spec.OmitPrefixes {
				omit = omit || strings.HasPrefix(name, prefix)
			}
			if omit {
				seen[name] = true
			}
		}
		if !omit {
			kept.WriteString(line)
		}
	}
	for _, name := range spec.Required {
		if !seen[name] {
			return nil, fmt.Errorf("review missing optional requirement: %s", name)
		}
	}
	for _, prefix := range spec.RequiredPrefixes {
		found := false
		for name := range seen {
			found = found || strings.HasPrefix(name, prefix)
		}
		if !found {
			return nil, fmt.Errorf("review missing optional requirement prefix: %s", prefix)
		}
	}
	text := kept.String()
	for _, replacement := range spec.Replace {
		var err error
		text, err = replace(text, replacement)
		if err != nil {
			return nil, err
		}
	}
	return []byte(text), nil
}

func wheelChanges(root string, spec Wheel, changes map[string][]byte) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(spec.Distribution) {
		return fmt.Errorf("invalid wheel distribution")
	}
	infos, err := filepath.Glob(filepath.Join(root, spec.Distribution+"-*.dist-info"))
	if err != nil || len(infos) != 1 {
		return fmt.Errorf("expected one %s distribution", spec.Distribution)
	}
	relative, err := filepath.Rel(root, infos[0])
	if err != nil {
		return err
	}
	metadata := filepath.Join(relative, "METADATA")
	path, err := localPath(root, metadata)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var kept strings.Builder
	removed := map[string]int{}
	for _, line := range strings.SplitAfter(string(data), "\n") {
		header, value, ok := strings.Cut(line, ": ")
		if ok && header == "Requires-Dist" {
			name := strings.ToLower(requirement.FindString(value))
			if slices.Contains(spec.Remove, name) {
				removed[name]++
				continue
			}
		}
		kept.WriteString(line)
	}
	for _, name := range spec.Remove {
		if removed[name] != 1 {
			return fmt.Errorf("review changed %s requirement: %s", spec.Distribution, name)
		}
	}
	changes[metadata] = []byte(kept.String())
	recordName := filepath.Join(relative, "RECORD")
	recordPath, err := localPath(root, recordName)
	if err != nil {
		return err
	}
	record, err := os.Open(recordPath)
	if err != nil {
		return err
	}
	rows, readErr := csv.NewReader(record).ReadAll()
	closeErr := record.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	changed := map[string]bool{filepath.ToSlash(metadata): true}
	for _, path := range spec.Records {
		changed[filepath.ToSlash(path)] = true
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if len(row) != 3 || seen[row[0]] {
			return fmt.Errorf("invalid or duplicate wheel RECORD row")
		}
		seen[row[0]] = true
		if !changed[row[0]] {
			continue
		}
		path, err := localPath(root, row[0])
		if err != nil {
			return err
		}
		data, ok := changes[filepath.FromSlash(row[0])]
		if !ok {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		sum := sha256.Sum256(data)
		row[1] = "sha256=" + base64.RawURLEncoding.EncodeToString(sum[:])
		row[2] = fmt.Sprint(len(data))
		delete(changed, row[0])
	}
	if len(changed) != 0 {
		return fmt.Errorf("patched files are missing from wheel RECORD")
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	if err := writer.WriteAll(rows); err != nil {
		return err
	}
	if err := writer.Error(); err != nil {
		return err
	}
	changes[recordName] = buffer.Bytes()
	return nil
}
