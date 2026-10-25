package probe

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/werewolf-linux/packages/internal/apk"
)

func testAPK(t *testing.T, summary, commit, arch string, runtimeFiles ...tar.Header) string {
	t.Helper()
	archive := func(files map[string]string, extra []tar.Header) []byte {
		var buffer bytes.Buffer
		compressed := gzip.NewWriter(&buffer)
		writer := tar.NewWriter(compressed)
		for name, text := range files {
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(text))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(text)); err != nil {
				t.Fatal(err)
			}
		}
		for _, header := range extra {
			if err := writer.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	payload := archive(map[string]string{
		"usr/share/glibc-testresults/tests.sum":       summary,
		"usr/share/glibc-testresults/elf/example.out": "raw failing output\n",
	}, runtimeFiles)
	sum := sha256.Sum256(payload)
	control := archive(map[string]string{
		".PKGINFO":      "pkgname = glibc-testresults\npkgver = 2.44-r1\narch = " + arch + "\ndatahash = " + hex.EncodeToString(sum[:]) + "\n",
		".melange.yaml": "vars:\n  glibc-expected-commit: " + commit + "\n",
	}, nil)
	filename := filepath.Join(t.TempDir(), "results.apk")
	if err := os.WriteFile(filename, append(control, payload...), 0644); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestCompareRetainsFailuresAndRejectsUnrelatedBuilds(t *testing.T) {
	commit := strings.Repeat("a", 40)
	baseline := testAPK(t, "PASS: elf/example\nFAIL: old/failure\n", commit, "x86_64")
	candidate := testAPK(t, "FAIL: elf/example\nFAIL: old/failure\nUNSUPPORTED: new/test\n", commit, "x86_64")
	output := filepath.Join(t.TempDir(), "report.json")
	if err := CompareResults(baseline, candidate, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Candidate packageEvidence              `json:"candidate"`
		Failures  map[string]map[string]string `json:"new_failures"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 1 || report.Failures["elf/example"][".out"] != "raw failing output\n" || report.Candidate.Counts["FAIL"] != 2 || report.Candidate.Results["new/test"] != "UNSUPPORTED" {
		t.Fatalf("raw evidence changed: %s", data)
	}
	for _, other := range []string{testAPK(t, "PASS: elf/example\n", strings.Repeat("b", 40), "x86_64"), testAPK(t, "PASS: elf/example\n", commit, "aarch64")} {
		if err := CompareResults(baseline, other, output); err == nil {
			t.Fatal("unrelated build accepted")
		}
	}
	if _, err := resultEvidence(testAPK(t, "FAIL: duplicate\nPASS: duplicate\n", commit, "x86_64")); err == nil {
		t.Fatal("duplicate test accepted")
	}
}

func TestRuntimeExtractionRejectsUnsafeLinks(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "usr/lib/escape", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "usr/lib/escape", Typeflag: tar.TypeSymlink, Linkname: "../../../outside"},
		{Name: "usr/lib/escape", Typeflag: tar.TypeLink, Linkname: "etc/passwd"},
	} {
		filename := testAPK(t, "PASS: ok\n", strings.Repeat("a", 40), "x86_64", header)
		info, err := apk.Inspect(filename)
		if err != nil {
			t.Fatal(err)
		}
		if err := extractLibraries(filename, t.TempDir(), info); err == nil {
			t.Fatalf("unsafe runtime link accepted: %+v", header)
		}
	}
	filename := testAPK(t, "PASS: ok\n", strings.Repeat("a", 40), "x86_64",
		tar.Header{Name: "usr/lib/value.so.1", Typeflag: tar.TypeReg, Mode: 0755},
		tar.Header{Name: "usr/lib/value.so", Typeflag: tar.TypeSymlink, Linkname: "/usr/lib/value.so.1"})
	info, err := apk.Inspect(filename)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := extractLibraries(filename, root, info); err != nil {
		t.Fatal(err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, "usr/lib/value.so"))
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(root, "usr/lib/value.so.1") {
		t.Fatalf("runtime link escaped private libraries: %s", target)
	}
}

func TestCommandTimeoutIsNeverAnExpectedFailure(t *testing.T) {
	if _, err := execute(t.Context(), time.Millisecond, "", nil, "sleep", "10"); err == nil {
		t.Fatal("timed-out command was treated as a normal exit")
	}
	result, err := execute(t.Context(), 0, "", nil, "sh", "-c", "printf diagnostic >&2; exit 7")
	if err != nil || result.code != 7 || result.stderr != "diagnostic" {
		t.Fatalf("exit evidence was lost: %+v, %v", result, err)
	}
}

func TestKeycloakFlowsCheckAuthorizationAndRejectBadSecrets(t *testing.T) {
	for _, acceptBadSecret := range []bool{false, true} {
		t.Run(fmt.Sprint(acceptBadSecret), func(t *testing.T) {
			admin := false
			realm := false
			clientCreated := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/realms/master/protocol/openid-connect/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("grant_type") != "password" || r.Form.Get("username") != "admin" {
						t.Error("incorrect admin grant")
					}
					admin = true
					fmt.Fprint(w, `{"access_token":"admin-token"}`)
				case "/admin/realms", "/admin/realms/consumer/clients":
					if !admin || r.Header.Get("Authorization") != "Bearer admin-token" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing admin authorization")
					}
					var object map[string]any
					if err := json.NewDecoder(r.Body).Decode(&object); err != nil {
						t.Error(err)
					}
					if r.URL.Path == "/admin/realms" {
						realm = object["realm"] == "consumer"
					} else {
						clientCreated = realm && object["serviceAccountsEnabled"] == true
					}
					w.WriteHeader(http.StatusCreated)
				case "/realms/consumer/.well-known/openid-configuration":
					if !clientCreated {
						t.Error("discovery before client creation")
					}
					fmt.Fprint(w, `{"code_challenge_methods_supported":["S256"]}`)
				case "/realms/consumer/protocol/openid-connect/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("client_secret") == "wrong" && !acceptBadSecret {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					fmt.Fprint(w, `{"access_token":"a.b.c","token_type":"Bearer"}`)
				case "/realms/consumer/protocol/saml/descriptor":
					fmt.Fprint(w, `<EntityDescriptor/>`)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			err := keycloakFlows(t.Context(), server.Client(), server.URL)
			if (err != nil) != acceptBadSecret {
				t.Fatalf("probe result: %v", err)
			}
		})
	}
}
