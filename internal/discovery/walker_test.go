package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestWalkerExcludesGoTestFilesByDefault(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, root, "main.go", "package main")
	writeDiscoveryFile(t, root, "main_test.go", "package main")
	writeDiscoveryFile(t, root, "notes_test.md", "notes")

	walker, err := NewWalker(root, nil, 500, false)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	files, stats, err := walker.Walk()
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	assertRelativePaths(t, files, []string{"main.go", "notes_test.md"})
	if stats.SkippedByTest != 1 || stats.SkippedFiles != 1 {
		t.Fatalf("stats = %#v, want one test-file skip", stats)
	}
}

func TestWalkerIncludesGoTestFilesWhenEnabled(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, root, "main.go", "package main")
	writeDiscoveryFile(t, root, "main_test.go", "package main")

	walker, err := NewWalker(root, nil, 500, true)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	files, stats, err := walker.Walk()
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	assertRelativePaths(t, files, []string{"main.go", "main_test.go"})
	if stats.SkippedByTest != 0 || stats.SkippedFiles != 0 {
		t.Fatalf("stats = %#v, want no skipped files", stats)
	}
}

func writeDiscoveryFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func assertRelativePaths(t *testing.T, files []DiscoveredFile, want []string) {
	t.Helper()
	got := make([]string, len(files))
	for index, file := range files {
		got[index] = file.RelPath
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}
