package packaging

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

func TestReleaseNotesPatchSeriesPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, policy, series          string
		present, directory, wantError bool
		want                          []string
	}{
		{name: "patchless", want: []string{}},
		{name: "declared series missing", policy: "werewolf/patches", wantError: true},
		{name: "empty series", present: true, want: []string{}},
		{name: "patches recorded", policy: "werewolf/patches", present: true,
			series: "0001-first.patch\n0002-second.patch\n", want: []string{"0001-first.patch", "0002-second.patch"}},
		{name: "unreadable series", directory: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := Package{Directory: t.TempDir(), Spec: &overlay.Spec{Patches: tc.policy}}
			path := filepath.Join(pkg.Directory, "patches/series")
			if tc.present || tc.directory {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if tc.directory {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte(tc.series), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := notePatches(pkg)
			if (err != nil) != tc.wantError {
				t.Fatalf("patches = %v, %v", got, err)
			}
			if err == nil && !slices.Equal(got, tc.want) {
				t.Fatalf("patches = %v; want %v", got, tc.want)
			}
		})
	}
}
