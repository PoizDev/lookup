package ui

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

func TestTerminalWordmarkMatchesCanonicalArt(t *testing.T) {
	canonical, err := os.ReadFile("../../docs/ascii.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := normalizeArt(string(canonical))
	got := normalizeArt(TerminalWordmark)
	if got != want {
		t.Fatalf("embedded wordmark drifted from docs/ascii.txt\nwant: %q\n got: %q", want, got)
	}
	if !utf8.ValidString(got) {
		t.Fatal("embedded wordmark is not valid UTF-8")
	}
	if !strings.ContainsRune(got, '\u2800') {
		t.Fatal("embedded wordmark lost BRAILLE PATTERN BLANK geometry")
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 9 || rows[8] != "" {
		t.Fatalf("embedded wordmark rows = %d, want eight plus final newline", len(rows))
	}
	for i, row := range rows[:8] {
		if width := lipgloss.Width(row); width != WordmarkWidth() {
			t.Fatalf("row %d display width = %d, want %d", i+1, width, WordmarkWidth())
		}
	}
}

func TestInstallerWordmarksMatchCanonicalArt(t *testing.T) {
	canonical, err := os.ReadFile("../../docs/ascii.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := normalizeArt(string(canonical))
	fixtures := []struct {
		name, path, start, end string
	}{
		{name: "install.sh", path: "../../install.sh", start: "# LOOKUP_WORDMARK_BEGIN\n      cat <<'EOF'\n", end: "\nEOF\n      # LOOKUP_WORDMARK_END"},
		{name: "install.ps1", path: "../../install.ps1", start: "# LOOKUP_WORDMARK_BEGIN\n    $wordmark = @'\n", end: "\n'@\n    # LOOKUP_WORDMARK_END"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			source, readErr := os.ReadFile(fixture.path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			contents := strings.ReplaceAll(string(source), "\r\n", "\n")
			start := strings.Index(contents, fixture.start)
			if start < 0 {
				t.Fatalf("wordmark start marker not found in %s", fixture.path)
			}
			start += len(fixture.start)
			end := strings.Index(contents[start:], fixture.end)
			if end < 0 {
				t.Fatalf("wordmark end marker not found in %s", fixture.path)
			}
			if got := normalizeArt(contents[start : start+end]); got != want {
				t.Fatalf("%s wordmark drifted from docs/ascii.txt", fixture.name)
			}
		})
	}
}

func normalizeArt(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimSuffix(value, "\n") + "\n"
}
