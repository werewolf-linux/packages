package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionalNativePruning(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required for its package pruning tests")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	lock, err := os.ReadFile(filepath.Join(root, "www/n8n/package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "package-lock.json"), lock, 0644); err != nil {
		t.Fatal(err)
	}
	removed := []string{"@sentry/node-native", "@sentry/node-native-stacktrace", "@sentry/profiling-node", "@parcel/watcher", "@parcel/watcher-linux-arm64-glibc", "n8n-core/node_modules/@sentry/profiling-node"}
	kept := []string{"sqlite3", "isolated-vm", "@napi-rs/canvas", "n8n", "n8n-core", "@sentry/node"}
	for _, name := range append(append([]string{}, removed...), kept...) {
		packageDir := filepath.Join(directory, "node_modules", name)
		if err := os.MkdirAll(packageDir, 0755); err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(name, "node_modules/")
		data, err := json.Marshal(map[string]string{"name": parts[len(parts)-1]})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDir, "package.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDir, "native.node"), []byte("native code"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "node", filepath.Join(root, "www/n8n/prune-optional.cjs"))
	command.Dir = directory
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pruning: %v: %s", err, out)
	}
	for _, name := range removed {
		if _, err := os.Stat(filepath.Join(directory, "node_modules", name)); !os.IsNotExist(err) {
			t.Errorf("optional native package remains: %s", name)
		}
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(directory, "node_modules", name, "native.node")); err != nil {
			t.Errorf("required native package removed: %s", name)
		}
	}
}

func TestNewOptionalNativeConsumerRequiresReview(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required for its package pruning tests")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "package-lock.json"), []byte(`{"packages":{"node_modules/workflow":{"dependencies":{"@parcel/watcher":"2"}}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "node", filepath.Join(root, "www/n8n/prune-optional.cjs"))
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "review new consumer") {
		t.Fatalf("new consumer was not rejected: %v: %s", err, output)
	}
}

func TestProductionPruningPreservesRuntimeAssets(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required for its package pruning tests")
	}
	script, err := filepath.Abs("../port-node.cjs")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	packages := map[string]any{}
	for _, name := range []string{"n8n", "n8n-core", "@n8n/config", "n8n/node_modules/third-party", "pdfjs-dist", "isolated-vm", "agent-browser", "resolve", "tedious"} {
		packages["node_modules/"+name] = map[string]any{}
	}
	lock, err := json.Marshal(map[string]any{"packages": packages})
	if err != nil {
		t.Fatal(err)
	}
	kept := []string{"n8n/dist/main.js.map", "n8n-core/dist/main.js.map", "@n8n/config/dist/main.js.map", "@n8n/config/dist/node-definitions/node.ts", "@n8n/config/skills/knowledge.md", "pdfjs-dist/legacy/build/pdf.mjs", "pdfjs-dist/standard_fonts/font.pfb", "pdfjs-dist/wasm/decoder.wasm", "resolve/lib/async.js", "tedious/lib/connection.js", "agent-browser/bin/agent-browser.js"}
	removed := []string{"n8n/node_modules/third-party/dist/lib.js.map", "@n8n/config/dist/config.d.ts", "n8n/dist/main.d.ts.map", "pdfjs-dist/legacy/build/pdf.mjs.map", "isolated-vm/prebuilds/binding.node", "resolve/test/test.js", "tedious/benchmarks/bench.js", "agent-browser/bin/agent-browser-darwin-arm64"}
	files := map[string][]byte{
		"package-lock.json":                        lock,
		"node_modules/n8n/package.json":            []byte(`{"engines":{"node":">=22"}}`),
		"node_modules/isolated-vm/src/lib/timer.h": []byte("#pragma once\n#include <memory>\n"),
	}
	for _, name := range append(append([]string{}, kept...), removed...) {
		files["node_modules/"+name] = []byte("runtime asset")
	}
	for name, data := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "node", script)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("production pruning: %v: %s", err, output)
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(directory, "node_modules", name)); err != nil {
			t.Errorf("runtime asset removed: %s: %v", name, err)
		}
	}
	for _, name := range removed {
		if _, err := os.Stat(filepath.Join(directory, "node_modules", name)); !os.IsNotExist(err) {
			t.Errorf("unused asset remains: %s: %v", name, err)
		}
	}
}
