package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/internal/apk"
)

func TestRecoveryProvenance(t *testing.T) {
	var run sourceRun
	if err := json.Unmarshal([]byte(`{"id":123,"run_number":456,"repository":{"full_name":"test/runtime"},"head_repository":{"full_name":"test/runtime"},"head_branch":"main","head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","event":"push","path":".github/workflows/wolfi-refresh.yml"}`), &run); err != nil {
		t.Fatal(err)
	}
	jobs := []buildJob{{"build (node) / build (x86_64, ubuntu-26.04)", "success"}, {"build (node) / build (aarch64, ubuntu-26.04-arm)", "success"}}
	sha, number, err := checkedRun("123", "test/runtime", "node", run, jobs)
	if err != nil || sha != run.HeadSHA || number != 456 {
		t.Fatalf("recovery = %s, %d, %v", sha, number, err)
	}
	for name, mutate := range map[string]func(*sourceRun){
		"different run":        func(run *sourceRun) { run.ID++ },
		"fork":                 func(run *sourceRun) { run.HeadRepository.FullName = "fork/runtime" },
		"different repository": func(run *sourceRun) { run.Repository.FullName = "other/runtime" },
		"different workflow":   func(run *sourceRun) { run.Path = ".github/workflows/build.yml" },
		"pull request":         func(run *sourceRun) { run.Event = "pull_request" },
		"feature branch":       func(run *sourceRun) { run.HeadBranch = "feature" },
		"mutable ref":          func(run *sourceRun) { run.HeadSHA = "main" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := run
			mutate(&changed)
			if _, _, err := checkedRun("123", "test/runtime", "node", changed, jobs); err == nil {
				t.Fatal("accepted ineligible source run")
			}
		})
	}
	for name, changed := range map[string][]buildJob{"missing": jobs[:1], "failed": {{jobs[0].Name, "failure"}, jobs[1]}, "duplicate": {jobs[0], jobs[1], jobs[0]}} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := checkedRun("123", "test/runtime", "node", run, changed); err == nil {
				t.Fatal("accepted unvalidated native builds")
			}
		})
	}
}

func TestSigningMaterial(t *testing.T) {
	root := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(root, "release/packages.pub"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}), 0644); err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKAGES_APK_SIGNING_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})))
	path, err := SigningKey(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PACKAGES_APK_SIGNING_KEY") != "" {
		t.Fatal("signing credential remains in subprocess environment")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatal("Melange key is not PKCS#1")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("private key permissions are too broad")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKAGES_APK_SIGNING_KEY", "")
	if _, err := SigningKey(root, true); err == nil {
		t.Fatal("created ephemeral key for production publication")
	}
}

func TestRunnerProfileStaysConfined(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(binary, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	template := `profile bwrap /usr/bin/bwrap flags=(unconfined) {
  change_profile -> &bwrap//&unpriv_bwrap,
  include if exists <local/bwrap-userns-restrict>
  profile unpriv_bwrap flags=(attach_disconnected) {
    change_profile -> &unpriv_bwrap,
    audit deny capability,
    include if exists <local/unpriv_bwrap>
  }
}`
	// Ubuntu's parent must remain confined as well.
	template = strings.Replace(template, "flags=(unconfined)", "flags=(attach_disconnected)", 1)
	result, err := RunnerProfile(template, binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"sys_chroot", "setuid", "setgid", "chown", "dac_override", "sys_admin", "setfcap", "net_admin", "net_bind_service", "setpcap"} {
		if !strings.Contains(result, "allow capability "+rule+",") {
			t.Errorf("missing %s", rule)
		}
	}
	for _, changed := range []string{template + "\nprofile unpriv_bwrap flags=", strings.Replace(template, "audit deny capability,", "allow capability sys_admin,", 1), template + "\nunconfined"} {
		if _, err := RunnerProfile(changed, binary); err == nil {
			t.Fatal("accepted changed or unconfined profile")
		}
	}
}

func TestArchiveSafetyAndDeterminism(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source")
	if err := writeFile(filepath.Join(source, "file"), []byte("content"), 0755); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(directory, "first.tar.gz"), filepath.Join(directory, "second.tar.gz")
	if err := Archive(source, first); err != nil {
		t.Fatal(err)
	}
	if err := Archive(source, second); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same artifacts produced different release archives")
	}
	target := filepath.Join(directory, "extracted")
	if err := Extract(first, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "file"))
	if err != nil || string(data) != "content" {
		t.Fatalf("extracted data = %q, %v", data, err)
	}
	for name, kind := range map[string]byte{"../escape": tar.TypeReg, "/absolute": tar.TypeReg, "link": tar.TypeSymlink} {
		var buffer bytes.Buffer
		gz := gzip.NewWriter(&buffer)
		writer := tar.NewWriter(gz)
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Linkname: "../outside", Mode: 0644}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "bad.tar.gz")
		if err := os.WriteFile(path, buffer.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		if err := Extract(path, filepath.Join(t.TempDir(), "out")); err == nil {
			t.Errorf("accepted archive member %s", name)
		}
	}
}

func TestRuntimePackagesCannotGrantTheirOwnPermissions(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	if err := writer.WriteHeader(&tar.Header{Name: "./etc/runtime-hardening/build-root", Mode: 0600, Size: 0}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "payload.apk")
	if err := os.WriteFile(path, buffer.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPayload(path, apk.Info{Name: "runtime"}, false, 0); err == nil {
		t.Fatal("accepted packaged hardening allowance")
	}
}

func TestAdvisoryVersions(t *testing.T) {
	for _, pair := range [][2]string{{"3.15.0-r0", "3.15.0_git20261010-r0"}, {"3.15.0_git20261010-r0", "3.15.0_p1-r0"}, {"3.15.0-r9", "3.15.0-r10"}, {"2.9-r0", "2.10-r0"}} {
		if order, ok := compareAPKVersion(pair[0], pair[1]); !ok || order >= 0 {
			t.Errorf("version order %v = %d, %t", pair, order, ok)
		}
	}
	database := []byte(`{"packages":[{"pkg":{"name":"runtime-3.15","secfixes":{"0":["not-affected"],"3.15.0-r1":["CVE-old"],"3.15.0_git20261010-r0":["CVE-new"],"3.15.0_p1-r0":["CVE-future"]}}}]}`)
	found, err := advisories(database, "runtime-3.15", "3.15.0-r1", "3.15.0_git20261010-r0")
	if err != nil {
		t.Fatal(err)
	}
	if found.Cumulative != 2 || found.NotAffected != 1 || len(found.Fixed) != 1 || !reflect.DeepEqual(found.Fixed[0].IDs, []string{"CVE-new"}) {
		t.Fatalf("advisories = %+v", found)
	}
}

func TestCopyKeepsInputPermissions(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.WriteFile(source, []byte("input"), 0751); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if unixMode(a) != unixMode(b) {
		t.Fatal("copy changed input modes")
	}
	input, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	data, err := io.ReadAll(input)
	if err != nil || string(data) != "input" {
		t.Fatalf("copy = %q, %v", data, err)
	}
}
