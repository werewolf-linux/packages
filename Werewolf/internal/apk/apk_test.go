package apk

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func archiveBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for name, data := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0644, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := gzipBytes(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPackageAndIndexAuthentication(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	payload := archiveBytes(t, map[string][]byte{"usr/bin/example": []byte("executable payload")})
	sum := sha256.Sum256(payload)
	control := archiveBytes(t, map[string][]byte{".PKGINFO": []byte("pkgname = example\npkgver = 1-r0\narch = x86_64\ndatahash = " + hex.EncodeToString(sum[:]) + "\n")})
	unsigned := filepath.Join(directory, "unsigned.apk")
	if err := os.WriteFile(unsigned, append(append([]byte{}, control...), payload...), 0644); err != nil {
		t.Fatal(err)
	}
	signed := filepath.Join(directory, "signed.apk")
	if err := SignPackage(unsigned, signed, "example", "1-r0", "x86_64", key); err != nil {
		t.Fatal(err)
	}
	info, err := Check(signed, "example", "1-r0", "x86_64", &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	if err := Payload(signed, info, func(header *tar.Header, reader io.Reader) error {
		if header.Name == "usr/bin/example" {
			data, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			found = bytes.Equal(data, []byte("executable payload"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("payload was not preserved")
	}
	checksum := sha1.Sum(info.Control)
	stanza := "P:example\nV:1-r0\nA:x86_64\nC:Q1" + base64.StdEncoding.EncodeToString(checksum[:]) + "\nS:" + strconv.FormatInt(info.Size, 10)
	if err := Match(info, stanza); err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{"example-1-r0.apk": stanza}
	data, err := SignedIndex(entries, key)
	if err != nil {
		t.Fatal(err)
	}
	again, err := SignedIndex(entries, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("index is not deterministic")
	}
	index := filepath.Join(directory, "APKINDEX.tar.gz")
	if err := os.WriteFile(index, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Index(index, "x86_64", &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if got["example-1-r0.apk"] != stanza {
		t.Fatalf("index = %v", got)
	}
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"truncated", func(data []byte) []byte { return data[:len(data)-1] }},
		{"changed payload", func(data []byte) []byte { data[len(data)-10] ^= 1; return data }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(signed)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "bad.apk")
			if err := os.WriteFile(path, test.mutate(data), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Check(path, "example", "1-r0", "x86_64", &key.PublicKey); err == nil {
				t.Fatal("accepted tampered package")
			}
		})
	}
	if _, err := Check(signed, "other", "1-r0", "x86_64", &key.PublicKey); err == nil {
		t.Fatal("accepted wrong package identity")
	}
	if _, err := Index(index, "aarch64", &key.PublicKey); err == nil {
		t.Fatal("accepted wrong index architecture")
	}
	if err := os.WriteFile(index, append(data, 0), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(index, "x86_64", &key.PublicKey); err == nil {
		t.Fatal("accepted trailing index bytes")
	}
}

func TestSignatureMismatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := Sign([]byte("message"), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify([]byte("changed"), sig, &key.PublicKey); err == nil {
		t.Fatal("accepted changed signed bytes")
	}
}

func TestControlBounds(t *testing.T) {
	data, err := gzipBytes(bytes.Repeat([]byte{'a'}, controlLimit+1))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "oversized.apk")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(path, "example", "1", "x86_64", nil); err == nil {
		t.Fatal("accepted oversized control member")
	}
}

func TestPayloadPathCannotEscape(t *testing.T) {
	payload := archiveBytes(t, map[string][]byte{"../escape": []byte("bad")})
	path := filepath.Join(t.TempDir(), "bad.apk")
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	visited := false
	if err := Payload(path, Info{}, func(*tar.Header, io.Reader) error { visited = true; return nil }); err == nil || visited {
		t.Fatal("accepted unsafe payload path")
	}
}

func TestIndexRejectsAmbiguousOrIncompleteIdentities(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	good := "P:example\nV:1-r0\nA:x86_64\nC:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=\nS:1"
	for _, stanza := range []string{
		good + "\nP:example",
		strings.ReplaceAll(good, "P:example", "P:example with spaces"),
		strings.ReplaceAll(good, "S:1", "S:-1"),
		strings.ReplaceAll(good, "C:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=", "C:Q1bad"),
	} {
		data, err := SignedIndex(map[string]string{"example-1-r0.apk": stanza}, key)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "APKINDEX.tar.gz")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Index(path, "x86_64", &key.PublicKey); err == nil {
			t.Fatalf("accepted ambiguous index: %s", stanza)
		}
	}
}
