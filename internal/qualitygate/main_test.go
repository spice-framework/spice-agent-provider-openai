package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTotalCoverage(t *testing.T) {
	t.Parallel()
	percentage, err := totalCoverage("example.go:1:\tFunction\t100.0%\ntotal:\t(statements)\t87.5%\n")
	if err != nil || percentage != 87.5 {
		t.Fatalf("totalCoverage() = %v, %v", percentage, err)
	}
	for _, content := range []string{"", "total: no-percentage"} {
		if _, err := totalCoverage(content); err == nil {
			t.Fatalf("totalCoverage(%q) error = nil", content)
		}
	}
}

func TestGoFilesExcludesToolsAndVendor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"main.go", "nested/value.go", "vendor/ignored.go", "tools/ignored.go"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := goFiles(root)
	if err != nil || len(files) != 2 {
		t.Fatalf("goFiles() = %v, %v", files, err)
	}
	joined := strings.Join(files, " ")
	if strings.Contains(joined, "ignored.go") {
		t.Fatalf("goFiles() included excluded files: %v", files)
	}
}

func TestTreeDigests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := treeDigests(root)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "value"), []byte("two"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	second, err := treeDigests(root)
	if err != nil || first["value"] == second["value"] {
		t.Fatalf("treeDigests() did not detect change: %v", err)
	}
}
