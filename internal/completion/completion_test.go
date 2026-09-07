package completion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShellCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []string
		want       Shell
	}{
		{"bash", []string{"/usr/bin/bash"}, Bash},
		{"zsh parent wins", []string{"zsh", "/bin/bash"}, Zsh},
		{"fish", []string{"fish"}, Fish},
		{"PowerShell", []string{"pwsh.exe"}, PowerShell},
		{"Windows PowerShell", []string{"powershell.exe"}, PowerShell},
		{"unknown", []string{"lookup", "nu"}, Unknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DetectCandidates(test.candidates...); got != test.want {
				t.Fatalf("DetectCandidates() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolvePaths(t *testing.T) {
	tests := []struct {
		name, goos, home, config, data string
		shell                          Shell
		wantSuffix                     string
	}{
		{"bash Linux", "linux", "/home/me", "/home/me/.config", "/home/me/.local/share", Bash, ".local/share/bash-completion/completions/lookup"},
		{"zsh macOS", "darwin", "/Users/me", "/Users/me/Library/Application Support", "/Users/me/.local/share", Zsh, ".local/share/zsh/site-functions/_lookup"},
		{"fish XDG", "linux", "/home/me", "/custom/config", "/custom/data", Fish, "/custom/config/fish/completions/lookup.fish"},
		{"PowerShell Windows", "windows", `C:\Users\me`, `C:\Users\me\AppData\Roaming`, "", PowerShell, `lookup/completions/lookup.ps1`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, err := ResolvePaths(test.shell, test.goos, test.home, test.config, test.data)
			if err != nil {
				t.Fatal(err)
			}
			got := filepath.ToSlash(paths.Script)
			if !strings.HasSuffix(strings.ToLower(got), strings.ToLower(filepath.ToSlash(test.wantSuffix))) {
				t.Fatalf("script path = %q, want suffix %q", got, test.wantSuffix)
			}
		})
	}
}

func TestInstallIsIdempotentAndUpdatesScript(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	dataDir := filepath.Join(home, ".local", "share")
	options := InstallOptions{Shell: Zsh, GOOS: "linux", Home: home, ConfigDir: configDir, DataDir: dataDir, Generate: func() ([]byte, error) { return []byte("first\n"), nil }}
	result, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Generate = func() ([]byte, error) { return []byte("second\n"), nil }
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(result.ScriptPath)
	if err != nil || string(content) != "second\n" {
		t.Fatalf("completion content = %q, error = %v", content, err)
	}
	profile, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(profile), profileMarkerStart) != 1 {
		t.Fatalf("profile integration is not idempotent:\n%s", profile)
	}
}

func TestInstallReportsNonWritableDestination(t *testing.T) {
	home := t.TempDir()
	blocker := filepath.Join(home, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Install(InstallOptions{Shell: Fish, GOOS: "linux", Home: home, ConfigDir: blocker, DataDir: filepath.Join(home, "data"), Generate: func() ([]byte, error) { return []byte("script"), nil }})
	if err == nil {
		t.Fatal("Install error = nil, want destination error")
	}
}
