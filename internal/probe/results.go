package probe

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/werewolf-linux/packages/internal/apk"
	"gopkg.in/yaml.v3"
)

var glibcCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)

type packageEvidence struct {
	SHA256   string                       `json:"sha256"`
	Source   string                       `json:"source_commit"`
	Arch     string                       `json:"arch"`
	Counts   map[string]int               `json:"counts,omitempty"`
	Results  map[string]string            `json:"results,omitempty"`
	Failures map[string]map[string]string `json:"failures,omitempty"`
}

func packageIdentity(filename string, info apk.Info) (packageEvidence, error) {
	var evidence packageEvidence
	file, err := os.Open(filename)
	if err != nil {
		return evidence, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return evidence, copyErr
	}
	if closeErr != nil {
		return evidence, closeErr
	}
	files, err := apk.ControlFiles(info)
	if err != nil {
		return evidence, err
	}
	var config struct {
		Vars map[string]string `yaml:"vars"`
	}
	if err := yaml.Unmarshal(files[".melange.yaml"], &config); err != nil {
		return evidence, err
	}
	evidence.Source = config.Vars["glibc-expected-commit"]
	if !glibcCommit.MatchString(evidence.Source) {
		return evidence, fmt.Errorf("missing exact glibc source commit in %s", filename)
	}
	evidence.Arch = info.Arch
	evidence.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return evidence, nil
}

func resultEvidence(filename string) (packageEvidence, error) {
	info, err := apk.Inspect(filename)
	if err != nil {
		return packageEvidence{}, err
	}
	evidence, err := packageIdentity(filename, info)
	if err != nil {
		return evidence, err
	}
	const prefix = "usr/share/glibc-testresults/"
	files := map[string]string{}
	total := int64(0)
	err = apk.Payload(filename, info, func(header *tar.Header, reader io.Reader) error {
		if !strings.HasPrefix(header.Name, prefix) || header.Typeflag != tar.TypeReg {
			return nil
		}
		name := strings.TrimPrefix(header.Name, prefix)
		if name != "tests.sum" && !strings.HasSuffix(name, ".out") && !strings.HasSuffix(name, ".test-result") {
			return nil
		}
		total += header.Size
		if header.Size > 32<<20 || total > 256<<20 {
			return fmt.Errorf("oversized upstream test evidence")
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if _, exists := files[name]; exists {
			return fmt.Errorf("duplicate result file: %s", name)
		}
		files[name] = strings.ToValidUTF8(string(data), "\uFFFD")
		return nil
	})
	if err != nil {
		return evidence, err
	}
	summary, ok := files["tests.sum"]
	if !ok {
		return evidence, fmt.Errorf("missing raw upstream tests.sum")
	}
	evidence.Counts = map[string]int{}
	evidence.Results = map[string]string{}
	evidence.Failures = map[string]map[string]string{}
	for _, line := range strings.Split(summary, "\n") {
		status, name, ok := strings.Cut(line, ": ")
		if !ok || !slices.Contains([]string{"PASS", "FAIL", "UNSUPPORTED", "XFAIL", "XPASS"}, status) {
			continue
		}
		if _, duplicate := evidence.Results[name]; duplicate {
			return evidence, fmt.Errorf("duplicate upstream test: %s", name)
		}
		evidence.Results[name] = status
		evidence.Counts[status]++
		if status == "FAIL" {
			outputs := map[string]string{}
			for _, extension := range []string{".out", ".test-result"} {
				if text, ok := files[name+extension]; ok {
					outputs[extension] = text
				}
			}
			evidence.Failures[name] = outputs
		}
	}
	if len(evidence.Results) == 0 {
		return evidence, fmt.Errorf("empty upstream test summary")
	}
	return evidence, nil
}

// CompareResults retains raw upstream statuses; failures are never reclassified.
func CompareResults(baselinePath, candidatePath, output string) error {
	baseline, err := resultEvidence(baselinePath)
	if err != nil {
		return err
	}
	candidate, err := resultEvidence(candidatePath)
	if err != nil {
		return err
	}
	if baseline.Source != candidate.Source || baseline.Arch != candidate.Arch {
		return fmt.Errorf("source commits or architectures differ")
	}
	changed := map[string]map[string]*string{}
	newFailures := map[string]map[string]string{}
	names := map[string]bool{}
	for name := range baseline.Results {
		names[name] = true
	}
	for name := range candidate.Results {
		names[name] = true
	}
	for name := range names {
		a, aOK := baseline.Results[name]
		b, bOK := candidate.Results[name]
		if a != b || aOK != bOK {
			entry := map[string]*string{"baseline": nil, "candidate": nil}
			if aOK {
				entry["baseline"] = &a
			}
			if bOK {
				entry["candidate"] = &b
			}
			changed[name] = entry
		}
		if b == "FAIL" && a != "FAIL" {
			newFailures[name] = candidate.Failures[name]
		}
	}
	report := map[string]any{"source_commit": baseline.Source, "baseline": baseline, "candidate": candidate, "changed": changed, "new_failures": newFailures}
	if err := writeReport(output, report); err != nil {
		return err
	}
	slog.Info("compared raw upstream results", "baseline", baseline.Counts, "candidate", candidate.Counts, "new_failures", len(newFailures), "output", output)
	return nil
}

func writeReport(filename string, value any) error {
	if filename == "" {
		return fmt.Errorf("report output is required")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return os.WriteFile(filename, append(data, '\n'), 0644)
}
