package lookup_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptOnboarding(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pathAvailable bool
	}{
		{name: "path available", pathAvailable: true},
		{name: "path missing", pathAvailable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fakeBin := filepath.Join(root, "fake-bin")
			installDir := filepath.Join(root, "home", ".local", "bin")
			if err := os.MkdirAll(fakeBin, 0o755); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "$1" in -s) echo Linux;; -m) echo x86_64;; *) echo Linux;; esac
`)
			writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/bin/sh
for arg do target=$arg; done
case "$target" in
  */checksums.txt) printf 'fixture  lookup_0.1.0_linux_amd64_musl.tar.gz\n' > "$target" ;;
  *) : > "$target" ;;
esac
`)
			writeExecutable(t, filepath.Join(fakeBin, "sha256sum"), `#!/bin/sh
printf 'fixture  %s\n' "$1"
`)
			writeExecutable(t, filepath.Join(fakeBin, "tar"), `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then shift; destination=$1; fi
  shift
done
printf '#!/bin/sh\n' > "$destination/lookup"
`)

			pathValue := fakeBin + ":/usr/bin:/bin"
			if tc.pathAvailable {
				pathValue = installDir + ":" + pathValue
			}
			cmd := exec.Command("/bin/sh", "install.sh")
			cmd.Env = append(os.Environ(),
				"HOME="+filepath.Join(root, "home"),
				"PATH="+pathValue,
				"LOOKUP_VERSION=v0.1.0",
				"LOOKUP_INSTALL_DIR="+installDir,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, output)
			}
			got := string(output)
			for _, want := range []string{"lookup\n", "Lookup 0.1.0 installed", "Binary", installDir + "/lookup", "lookup init"} {
				if !strings.Contains(got, want) {
					t.Fatalf("installer output missing %q:\n%s", want, got)
				}
			}
			if tc.pathAvailable {
				for _, want := range []string{"Get started", "Then analyze a project", "lookup ."} {
					if !strings.Contains(got, want) {
						t.Fatalf("PATH-present output missing %q:\n%s", want, got)
					}
				}
			} else {
				for _, want := range []string{"is not in PATH", "Add it to your shell:", `export PATH="` + installDir + `:$PATH"`, "Then run:"} {
					if !strings.Contains(got, want) {
						t.Fatalf("PATH-missing output missing %q:\n%s", want, got)
					}
				}
				if strings.Contains(got, "lookup .") {
					t.Fatalf("PATH-missing output promotes scan before PATH repair:\n%s", got)
				}
			}
		})
	}
}

func TestPowerShellInstallerOnboardingContract(t *testing.T) {
	source, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	got := string(source)
	for _, want := range []string{
		"Lookup $releaseVersion installed",
		"Binary",
		"Get started",
		"lookup init",
		"Then analyze a project",
		"lookup .",
		"is not in PATH",
		"user PATH",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PowerShell installer missing onboarding contract %q", want)
		}
	}
	if strings.Contains(got, "export PATH=") {
		t.Fatal("PowerShell installer contains Unix PATH guidance")
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
