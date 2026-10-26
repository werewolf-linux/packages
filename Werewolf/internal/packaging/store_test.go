package packaging

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/werewolf-linux/packages/Werewolf/internal/apk"
)

// TestMain lets the test binary stand in for aws without a real account, network,
// or extra test dependency. Each subprocess operates only inside a test directory.
func TestMain(m *testing.M) {
	if os.Getenv("PACKAGES_TEST_AWS") == "1" {
		os.Exit(testAWS(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func testAWS(args []string) int {
	operation := ""
	values := map[string]string{}
	destination := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "s3api" {
			if i+1 >= len(args) {
				return 2
			}
			operation = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--") {
			if i+1 >= len(args) {
				return 2
			}
			values[args[i]] = args[i+1]
			i++
		} else {
			destination = args[i]
		}
	}
	key := values["--key"]
	if !filepath.IsLocal(key) {
		return 2
	}
	path := filepath.Join(os.Getenv("PACKAGES_TEST_STORE"), key)
	if operation == "get-object" {
		if os.Getenv("PACKAGES_TEST_FAIL_GET") == key {
			fmt.Fprintln(os.Stderr, "API unavailable")
			return 1
		}
		if err := copyFile(path, destination); err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, "(NoSuchKey)")
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
		sum, err := digest(path)
		if err != nil {
			return 1
		}
		fmt.Printf("{\"ETag\":%q}\n", sum)
		return 0
	}
	if operation != "put-object" {
		return 2
	}
	if race := os.Getenv("PACKAGES_TEST_RACE"); race != "" && strings.HasSuffix(key, "APKINDEX.tar.gz") {
		if _, err := os.Stat(race); err == nil {
			if err := copyFile(race, path); err != nil {
				return 1
			}
			if err := os.Remove(race); err != nil {
				return 1
			}
			fmt.Fprintln(os.Stderr, "(PreconditionFailed)")
			return 1
		}
	}
	previous, err := digest(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if (values["--if-none-match"] == "*" && err == nil) || (values["--if-match"] != "" && (err != nil || previous != values["--if-match"])) {
		fmt.Fprintln(os.Stderr, "(PreconditionFailed)")
		return 1
	}
	if err := copyFile(values["--body"], path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(`{"ETag":"written"}`)
	return 0
}

func fakeStore(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(directory, "aws")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PACKAGES_TEST_AWS", "1")
	t.Setenv("PACKAGES_TEST_STORE", directory)
	return directory
}

func TestImmutableStorage(t *testing.T) {
	root := fakeStore(t)
	store := Store{Prefix: "apk/example/"}
	source := filepath.Join(t.TempDir(), "package.apk")
	if err := os.WriteFile(source, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Immutable(t.Context(), source, "x86_64/example.apk"); err != nil {
		t.Fatal(err)
	}
	if err := store.Immutable(t.Context(), source, "x86_64/example.apk"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Immutable(t.Context(), source, "x86_64/example.apk"); err == nil {
		t.Fatal("overwrote an immutable object")
	}
	data, err := os.ReadFile(filepath.Join(root, "apk/example/x86_64/example.apk"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("original")) {
		t.Fatal("published bytes changed")
	}
	t.Setenv("PACKAGES_TEST_FAIL_GET", "apk/example/unavailable")
	if _, err := store.Fetch(t.Context(), "unavailable", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("API failure looked like missing data")
	}
}

func TestConcurrentIndexMerge(t *testing.T) {
	root := fakeStore(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	stanza := func(name string) string {
		return "P:" + name + "\nV:1-r0\nA:x86_64\nC:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=\nS:1"
	}
	winner, err := apk.SignedIndex(map[string]string{"other-1-r0.apk": stanza("other")}, key)
	if err != nil {
		t.Fatal(err)
	}
	race := filepath.Join(root, "winner-index")
	if err := os.WriteFile(race, winner, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKAGES_TEST_RACE", race)
	store := Store{Prefix: "apk/example/"}
	current := map[string]string{"example-1-r0.apk": stanza("example")}
	if err := store.MergeIndex(t.Context(), current, "x86_64", key, &key.PublicKey, t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	entries, err := apk.Index(filepath.Join(root, "apk/example/x86_64/APKINDEX.tar.gz"), "x86_64", &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries["other-1-r0.apk"] == "" || entries["example-1-r0.apk"] == "" {
		t.Fatalf("lost concurrent publication: %v", entries)
	}
	current["example-1-r0.apk"] += "\nD:changed"
	if err := store.MergeIndex(t.Context(), current, "x86_64", key, &key.PublicKey, t.TempDir(), false); err == nil {
		t.Fatal("replaced an existing published package")
	}
	if err := store.MergeIndex(t.Context(), current, "x86_64", key, &key.PublicKey, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatedManifest(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	manifest := AppManifest{AppMetadata: AppMetadata{Context: "example", Version: "1-r0", Inputs: "build"}, RunNumber: 7}
	data, err := Envelope(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	var got AppManifest
	if err := OpenEnvelope(data, &key.PublicKey, &got); err != nil {
		t.Fatal(err)
	}
	if got.Context != manifest.Context || got.RunNumber != 7 {
		t.Fatal("manifest changed")
	}
	var envelope map[string]string
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["payload"] = "Y2hhbmdlZA=="
	changed, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := OpenEnvelope(changed, &key.PublicKey, &got); err == nil {
		t.Fatal("accepted changed signed manifest")
	}
}

func TestActionsOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", path)
	if err := ActionsOutput(map[string]any{"state": "build", "source_run_number": 123, "build": []string{"example"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"state=build\n", "source_run_number=" + strconv.Itoa(123) + "\n", `build=["example"]` + "\n"} {
		if !strings.Contains(string(data), line) {
			t.Errorf("missing %q in %q", line, data)
		}
	}
	if err := ActionsOutput(map[string]any{"state": "build\nmalicious=1"}); err == nil {
		t.Fatal("accepted an injected output record")
	}
}
