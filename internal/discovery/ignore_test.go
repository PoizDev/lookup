package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnorerMatchesGitSemantics(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, ".gitignore", "*.log\n/build/\n!important.log\ndocs/**/draft*.md\n")
	writeFixture(t, root, "src/.gitignore", "generated/\n!generated/\n!generated/keep.go\n*.tmp\n")

	ignorer, err := NewIgnorer(root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path    string
		dir     bool
		ignored bool
	}{
		{"debug.log", false, true},
		{"important.log", false, false},
		{"nested/debug.log", false, true},
		{"build", true, true},
		{"nested/build", true, false},
		{"docs/a/b/draft-one.md", false, true},
		{"src/generated", true, false},
		{"src/generated/keep.go", false, false},
		{"src/cache.tmp", false, true},
		{"other/cache.tmp", false, false},
		{".git", true, true},
	}
	for _, test := range tests {
		if got := ignorer.IsIgnored(test.path, test.dir); got != test.ignored {
			t.Errorf("IsIgnored(%q, %v) = %v, want %v", test.path, test.dir, got, test.ignored)
		}
	}
}

func TestWalkerHonorsNestedNegationAndParentPrecedence(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, ".gitignore", "*.go\n")
	writeFixture(t, root, "src/.gitignore", "!keep.go\n")
	writeFixture(t, root, "src/keep.go", "package keep\n")
	writeFixture(t, root, "src/drop.go", "package drop\n")

	walker, err := NewWalker(root, nil, 500, true)
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := walker.Walk()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.ToSlash(files[0].RelPath) != "src/keep.go" {
		t.Fatalf("files = %#v, want only src/keep.go", files)
	}
}

func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
