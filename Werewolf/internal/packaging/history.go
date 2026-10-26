package packaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var regexpBuildID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type historyEntry struct {
	Version         string `json:"version"`
	Date            string `json:"date"`
	BuildID         string `json:"build_id"`
	WolfiVersion    string `json:"wolfi_version"`
	AdvisoriesFixed int    `json:"advisories_fixed"`
	NewFailures     *int   `json:"new_test_failures"`
	Notes           string `json:"notes"`
}

// releaseHistory preserves the existing per-package releases.json and HTML
// entry points. Both writes use their observed ETags, retrying a concurrent merge.
func releaseHistory(ctx context.Context, store Store, notes releaseNote, work string) error {
	if !regexpBuildID.MatchString(notes.BuildID) {
		return fmt.Errorf("invalid release history build identity")
	}
	entry := historyEntry{Version: notes.Version, Date: notes.Date, BuildID: notes.BuildID, WolfiVersion: notes.Source.WolfiVersion, Notes: "builds/" + notes.BuildID + "/release-notes.html"}
	for _, fix := range notes.Security.Fixed {
		entry.AdvisoriesFixed += len(fix.IDs)
	}
	count, compared := 0, len(notes.Architectures) > 0
	for _, architecture := range notes.Architectures {
		count += len(architecture.NewFailures)
		compared = compared && architecture.Compared
	}
	if compared {
		entry.NewFailures = &count
	}
	page, err := template.ParseFS(noteTemplates, "templates/history.html.tmpl")
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 12; attempt++ {
		jsonPath, htmlPath := filepath.Join(work, "releases.json"), filepath.Join(work, "index.html")
		jsonETag, err := store.Fetch(ctx, "releases.json", jsonPath)
		if err != nil {
			return err
		}
		htmlETag, err := store.Fetch(ctx, "index.html", htmlPath)
		if err != nil {
			return err
		}
		var entries []historyEntry
		if jsonETag != "" {
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &entries); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		var merged []historyEntry
		for _, old := range entries {
			if !regexpBuildID.MatchString(old.BuildID) || seen[old.BuildID] {
				return fmt.Errorf("invalid or duplicate release history build")
			}
			seen[old.BuildID] = true
			old.Notes = "builds/" + old.BuildID + "/release-notes.html"
			if old.BuildID != entry.BuildID {
				merged = append(merged, old)
			}
		}
		merged = append(merged, entry)
		slices.SortFunc(merged, func(a, b historyEntry) int {
			if order, ok := compareAPKVersion(a.Version, b.Version); ok && order != 0 {
				return -order
			}
			if order := strings.Compare(b.Version, a.Version); order != 0 {
				return order
			}
			return strings.Compare(b.BuildID, a.BuildID)
		})
		if err := writeJSON(jsonPath, merged); err != nil {
			return err
		}
		var rendered bytes.Buffer
		if err := page.Execute(&rendered, struct {
			Title   string
			Entries []historyEntry
		}{notes.Title, merged}); err != nil {
			return err
		}
		if err := writeFile(htmlPath, rendered.Bytes(), 0644); err != nil {
			return err
		}
		err = store.conditional(ctx, "releases.json", jsonPath, jsonETag)
		if err == nil {
			err = store.conditional(ctx, "index.html", htmlPath, htmlETag)
		}
		if err == nil {
			return nil
		}
		if !raced(err) {
			return err
		}
		if err := wait(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("release history kept changing")
}
