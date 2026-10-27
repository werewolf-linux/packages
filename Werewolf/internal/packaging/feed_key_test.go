package packaging

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestFeedReusesRepositorySigningKeyFromPreviousStep(t *testing.T) {
	root := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(root, "Werewolf/keys/packages.pub"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKAGES_APK_SIGNING_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	// Match the workflow: one process installs the credential, and the next
	// signs the feed using the persisted key with no credential in its env.
	if _, err := SigningKey(root, true); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PACKAGES_APK_SIGNING_KEY") != "" {
		t.Fatal("signing credential remains in environment")
	}
	pkg := Package{Root: root}
	// No derivative history needs fetching in this regression; Feed must still
	// locate and authenticate the production signing key before selecting work.
	pkg.Engine = "upstream"
	if err := Feed(context.Background(), []Package{pkg}); err != nil {
		t.Fatalf("reuse installed signing key: %v", err)
	}
}
