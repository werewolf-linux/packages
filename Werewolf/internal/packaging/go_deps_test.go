package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/overlay"
)

func TestGoDependenciesResumeInterruptedDownloadsAndBoundFailures(t *testing.T) {
	data, err := os.ReadFile("../../pipelines/go-deps.yaml")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := overlay.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	runs := overlay.Value(overlay.Field(definition, "pipeline").Content[0], "runs")
	for _, tc := range []struct {
		name, target, tags, experiments string
		failures, attempts              int
		pass                            bool
	}{
		{"first download", "./cmd/caddy", "netgo,osusergo", "", 0, 1, true},
		{"interrupted downloads", "./cmd/zot", "", "jsonv2", 2, 3, true},
		{"persistent failure", "./cmd/caddy", "netgo,osusergo", "", 10, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tools := filepath.Join(root, "tools")
			if err := os.Mkdir(tools, 0755); err != nil {
				t.Fatal(err)
			}
			// A failed module transfer leaves completed downloads available to the
			// next invocation. Never retry compilation or fetch unrelated modules.
			fakeGo := `#!/bin/sh
set -eu
test "$CGO_ENABLED" = 0
test "$GOEXPERIMENT" = "$EXPECTED_EXPERIMENTS"
test "$*" = "$EXPECTED_ARGS"
count=0
if [ -f attempts ]; then
  count=$(cat attempts)
  test -f completed-download
fi
count=$((count + 1))
echo "$count" > attempts
touch completed-download
test "$count" -gt "$FAILURES"
`
			for name, script := range map[string]string{"go": fakeGo, "sleep": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.NewReplacer("${{inputs.packages}}", tc.target, "${{inputs.tags}}", tc.tags, "${{inputs.experiments}}", tc.experiments).Replace(runs)
			command := exec.CommandContext(t.Context(), "sh", "-eu", "-c", script)
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "FAILURES="+strconv.Itoa(tc.failures), "EXPECTED_EXPERIMENTS="+tc.experiments, "EXPECTED_ARGS=list -mod=readonly -deps -tags="+tc.tags+" "+tc.target)
			if output, err := command.CombinedOutput(); (err == nil) != tc.pass {
				t.Fatalf("download result: %v: %s", err, output)
			}
			attempts, err := os.ReadFile(filepath.Join(root, "attempts"))
			if err != nil || strings.TrimSpace(string(attempts)) != strconv.Itoa(tc.attempts) {
				t.Fatalf("download attempts = %q, %v", attempts, err)
			}
		})
	}
}
