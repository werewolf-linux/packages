package packaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseHistoryPreservesVersionsAndEscapesHTML(t *testing.T) {
	root := fakeStore(t)
	store := Store{Prefix: "apk/example/"}
	work := t.TempDir()
	var notes releaseNote
	notes.Title = "Example <script>"
	notes.Package = "example"
	notes.BuildID = strings.Repeat("a", 64)
	notes.Version = "2.44-r1"
	notes.Date = "2026-10-25"
	notes.Source.WolfiVersion = "2.44-r0"
	notes.Architectures = map[string]architectureNotes{"x86_64": {Compared: true}, "aarch64": {Compared: true}}
	if err := releaseHistory(t.Context(), store, notes, work); err != nil {
		t.Fatal(err)
	}
	if err := releaseHistory(t.Context(), store, notes, work); err != nil {
		t.Fatal(err)
	}
	notes.BuildID = strings.Repeat("b", 64)
	notes.Version = "2.44-r2"
	notes.Architectures["x86_64"] = architectureNotes{Compared: false}
	if err := releaseHistory(t.Context(), store, notes, work); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, store.Prefix, "releases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []historyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Version != "2.44-r2" || entries[0].NewFailures != nil || entries[1].NewFailures == nil || *entries[1].NewFailures != 0 {
		t.Fatalf("history: %s", data)
	}
	data, err = os.ReadFile(filepath.Join(root, store.Prefix, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<script>") || !strings.Contains(string(data), "&lt;script&gt;") || !strings.Contains(string(data), "not compared") || !strings.Contains(string(data), ">0</td>") {
		t.Fatalf("history HTML: %s", data)
	}
	t.Setenv("PACKAGES_TEST_FAIL_GET", store.Prefix+"releases.json")
	if err := releaseHistory(t.Context(), store, notes, work); err == nil {
		t.Fatal("history API failure treated as absent")
	}
}
