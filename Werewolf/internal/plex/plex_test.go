package plex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedVendorInputs(t *testing.T) {
	root, err := filepath.Abs("../../../multimedia/plex")
	if err != nil {
		t.Fatal(err)
	}
	sources, err := Verify(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources.Architectures) != 2 {
		t.Fatal("missing native architecture")
	}
	for arch, native := range sources.Architectures {
		if len(native.Codecs) < 6 {
			t.Errorf("%s lost codec inputs", arch)
		}
	}
	for _, test := range []struct {
		name, file string
		change     func([]byte) []byte
	}{
		{"changed index", "Packages.arm64", func(data []byte) []byte { return append(data, []byte("Package: attacker\n")...) }},
		{"changed recipe pin", "package.yaml", func(data []byte) []byte {
			return []byte(strings.Replace(string(data), sources.Architectures["aarch64"].Codecs[0].SHA256, strings.Repeat("0", 64), 1))
		}},
		{"changed vendor pin", "sources.json", func(data []byte) []byte {
			return []byte(strings.Replace(string(data), sources.Architectures["x86_64"].Deb.SHA256, strings.Repeat("0", 64), 1))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			for _, name := range []string{"sources.json", "PlexSign.v2.key", "InRelease", "Packages.arm64", "Packages.amd64", "package.yaml"} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				if name == test.file {
					data = test.change(data)
				}
				if err := os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Verify(t.Context(), directory); err == nil {
				t.Fatal("accepted altered vendor provenance")
			}
		})
	}
}

func TestVendorFilenameIsConfined(t *testing.T) {
	for _, filename := range []string{"../escape.deb", "pool/main/p/plexmediaserver/../../escape.deb", "https://attacker.invalid/file.deb", "pool/main/p/plexmediaserver/file;exec.deb"} {
		text := "Package: plexmediaserver\nFilename: " + filename + "\nSHA256: " + strings.Repeat("a", 64) + "\n"
		if _, err := packageFields([]byte(text)); err == nil {
			t.Errorf("accepted %s", filename)
		}
	}
}
