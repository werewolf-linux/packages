package transform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"github.com/werewolf-linux/packages/internal/probe"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardedEditsAndRequirementBlocks(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("source.py", "old\nDict[str, List[int]]; typing.Dict[str, int]; NotDict[str, int]\n")
	write("requirements.txt", "keep==1 \\\n    --hash=sha256:abc\noptional==2 \\\n    --hash=sha256:def\n# optional annotation\nnext==3\n")
	plan := Plan{Actions: map[string]Action{"build": {
		Files:        []File{{Path: "source.py", RequireContains: "old", Replace: []Replacement{{Old: "old", New: "new"}}, Qualify: map[string]string{"Dict": "typing.Dict", "List": "typing.List"}}, {Path: "missing/optional.py", Optional: true}},
		Requirements: &Requirements{Input: "requirements.txt", Output: "minimal.txt", Omit: []string{"optional"}, Required: []string{"optional"}},
	}}}
	if err := Apply(plan, "build", root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "source.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\ntyping.Dict[str, typing.List[int]]; typing.Dict[str, int]; NotDict[str, int]\n" {
		t.Fatalf("qualification: %s", data)
	}
	data, err = os.ReadFile(filepath.Join(root, "minimal.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep==1 \\\n    --hash=sha256:abc\nnext==3\n" {
		t.Fatalf("requirement block: %s", data)
	}
	if _, err := filterRequirements(data, Requirements{Omit: []string{"optional"}, Required: []string{"optional"}}); err == nil {
		t.Fatal("missing expected optional package accepted")
	}
}

func TestFailedGuardLeavesAllInputsUnchanged(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	plan := Plan{Actions: map[string]Action{"build": {Files: []File{
		{Path: "a", Replace: []Replacement{{Old: "old", New: "new"}}},
		{Path: "b", RequireContains: "upstream text that changed"},
	}}}}
	if err := Apply(plan, "build", root); err == nil {
		t.Fatal("upstream drift accepted")
	}
	for _, name := range []string{"a", "b"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "old" {
			t.Fatalf("input %s changed before checks finished", name)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := localPath(root, "escape/output"); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := localPath(root, "../escape"); err == nil {
		t.Fatal("parent escape accepted")
	}
}

func TestWheelMetadataAndRecordStayConsistent(t *testing.T) {
	root := t.TempDir()
	info := "example-1.dist-info"
	if err := os.Mkdir(filepath.Join(root, info), 0755); err != nil {
		t.Fatal(err)
	}
	metadata := "Name: example\nRequires-Dist: keep>=1\nRequires-Dist: optional>=2\n"
	if err := os.WriteFile(filepath.Join(root, info, "METADATA"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	record := info + "/METADATA,sha256=old,0\n" + info + "/RECORD,,\n"
	if err := os.WriteFile(filepath.Join(root, info, "RECORD"), []byte(record), 0644); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Actions: map[string]Action{"metadata": {Wheels: []Wheel{{Distribution: "example", Remove: []string{"optional"}}}}}}
	if err := Apply(plan, "metadata", root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, info, "METADATA"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Name: example\nRequires-Dist: keep>=1\n" {
		t.Fatalf("metadata: %s", data)
	}
	dataRecord, err := os.ReadFile(filepath.Join(root, info, "RECORD"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(dataRecord))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if rows[0][1] != "sha256="+base64.RawURLEncoding.EncodeToString(sum[:]) || rows[0][2] != "37" {
		t.Fatalf("incorrect RECORD: %v", rows)
	}
	if rows[1][1] != "" || rows[1][2] != "" {
		t.Fatal("RECORD hashes itself")
	}
	if err := Apply(plan, "metadata", root); err == nil {
		t.Fatal("missing expected requirement accepted")
	}
}

func TestPlansRejectAmbiguousActions(t *testing.T) {
	for _, plan := range []Plan{{}, {Actions: map[string]Action{"empty": {}}}, {Actions: map[string]Action{"mixed": {Files: []File{{Path: "x"}}, Requirements: &Requirements{}, Curl: &probe.CurlOptions{}}}}} {
		if err := plan.Validate(); err == nil {
			t.Fatal("accepted empty or ambiguous action")
		}
	}
}
