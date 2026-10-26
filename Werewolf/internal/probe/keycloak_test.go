package probe

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeycloakProviderMtimes(t *testing.T) {
	for _, tc := range []struct {
		name, properties, want string
	}{
		{"matching", "kc.provider.file.provider.jar.last-modified=1700000001000\n", ""},
		{"drift", "kc.provider.file.provider.jar.last-modified=1700000000000\n", "provider.jar: build mtime=1700000000000, installed mtime=1700000001000"},
		{"missing metadata", "unrelated=value\n", "provider.jar: build mtime=, installed mtime=1700000001000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"lib/quarkus", "providers"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			provider := filepath.Join(root, "providers/provider.jar")
			if err := os.WriteFile(provider, nil, 0644); err != nil {
				t.Fatal(err)
			}
			stamp := time.Unix(1700000001, 0)
			if err := os.Chtimes(provider, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			var data bytes.Buffer
			archive := zip.NewWriter(&data)
			properties, err := archive.Create("META-INF/keycloak-persisted.properties")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := properties.Write([]byte(tc.properties)); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "lib/quarkus/generated-bytecode.jar"), data.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			err = keycloakProviderMtimes(root)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
		})
	}
}
