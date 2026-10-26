package smoke

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func apkMember(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	archive := tar.NewWriter(gz)
	for _, header := range headers {
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestRestoreAPKTimestampAfterImageLoaderDropsIt(t *testing.T) {
	dir := t.TempDir()
	provider := filepath.Join(dir, "provider's $HOME `uname`.jar")
	if err := os.WriteFile(provider, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000001, 0)
	data := apkMember(t, &tar.Header{Name: ".SIGN.RSA.test", Mode: 0o644})
	data = append(data, apkMember(t, &tar.Header{Name: ".PKGINFO", Mode: 0o644})...)
	data = append(data, apkMember(t, &tar.Header{Name: strings.TrimPrefix(provider, "/"), Mode: 0o644, ModTime: stamp})...)
	apk := filepath.Join(dir, "test.apk")
	if err := os.WriteFile(apk, data, 0o644); err != nil {
		t.Fatal(err)
	}
	command, err := restoreMtimes([]string{apk}, []string{provider})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		// Native runners use BusyBox touch. GNU touch supports the same syntax.
		touch, err := exec.LookPath("gtouch")
		if err != nil {
			t.Skip("GNU touch is required on macOS")
		}
		if err := os.Symlink(touch, filepath.Join(dir, "touch")); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	}
	if out, err := exec.Command("sh", "-ec", command).CombinedOutput(); err != nil {
		t.Fatalf("restoring APK metadata: %v: %s", err, out)
	}
	info, err := os.Stat(provider)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatalf("mtime = %v; want APK timestamp %v", info.ModTime(), stamp)
	}
}

func TestRestoreMtimesRejectsUnreliableMetadata(t *testing.T) {
	header := &tar.Header{Name: "provider.jar", Mode: 0o644, ModTime: time.Unix(1700000001, 0)}
	for _, tc := range []struct {
		name  string
		data  []byte
		paths []string
	}{
		{"missing", apkMember(t), []string{"/provider.jar"}},
		{"duplicate entry", apkMember(t, header, header), []string{"/provider.jar"}},
		{"symlink", apkMember(t, &tar.Header{Name: "provider.jar", Typeflag: tar.TypeSymlink, Linkname: "elsewhere"}), []string{"/provider.jar"}},
		{"corrupt gzip", []byte("broken"), []string{"/provider.jar"}},
		{"truncated gzip", apkMember(t, header)[:20], []string{"/provider.jar"}},
		{"relative path", nil, []string{"provider.jar"}},
		{"unclean path", nil, []string{"/a/../provider.jar"}},
		{"duplicate path", nil, []string{"/provider.jar", "/provider.jar"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apk := filepath.Join(t.TempDir(), "test.apk")
			if err := os.WriteFile(apk, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := restoreMtimes([]string{apk}, tc.paths); err == nil {
				t.Fatal("accepted unreliable timestamp metadata")
			}
		})
	}
}
