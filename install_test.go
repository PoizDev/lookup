package lookup_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type unixInstallerFixture struct {
	t          *testing.T
	root       string
	home       string
	fakeBin    string
	installDir string
}

func newUnixInstallerFixture(t *testing.T) *unixInstallerFixture {
	t.Helper()
	root := t.TempDir()
	f := &unixInstallerFixture{t: t, root: root, home: filepath.Join(root, "home"), fakeBin: filepath.Join(root, "fake-bin"), installDir: filepath.Join(root, "home", ".local", "bin")}
	if err := os.MkdirAll(f.fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(f.fakeBin, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo x86_64;; *) echo Linux;; esac\n")
	writeExecutable(t, filepath.Join(f.fakeBin, "curl"), "#!/bin/sh\nfor arg do target=$arg; done\ncase \"$target\" in\n  */checksums.txt) printf 'fixture  lookup_0.1.0_linux_amd64_musl.tar.gz\\n' > \"$target\" ;;\n  *) : > \"$target\" ;;\nesac\n")
	writeExecutable(t, filepath.Join(f.fakeBin, "sha256sum"), "#!/bin/sh\nprintf 'fixture  %s\\n' \"$1\"\n")
	writeExecutable(t, filepath.Join(f.fakeBin, "tar"), "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"-C\" ]; then shift; destination=$1; fi\n  shift\ndone\nprintf '#!/bin/sh\\n' > \"$destination/lookup\"\n")
	return f
}

func (f *unixInstallerFixture) run(extraEnv ...string) ([]byte, error) {
	f.t.Helper()
	env := []string{
		"HOME=" + f.home,
		"PATH=" + f.fakeBin + ":/usr/bin:/bin",
		"LOOKUP_VERSION=v0.1.0",
		"LOOKUP_INSTALL_DIR=" + f.installDir,
		"SHELL=/bin/zsh",
		"USER=lookupfixture",
		"TERM=xterm-256color",
	}
	env = append(env, extraEnv...)
	cmd := exec.Command("/bin/sh", "install.sh")
	cmd.Env = env
	return cmd.CombinedOutput()
}

func TestInstallScriptOnboarding(t *testing.T) {
	f := newUnixInstallerFixture(t)
	output, err := f.run("PATH=" + f.installDir + ":" + f.fakeBin + ":/usr/bin:/bin")
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}
	got := string(output)
	for _, want := range []string{"lookup\n", "Lookup 0.1.0 installed", "Binary", f.installDir + "/lookup", "lookup init", "Get started", "Then analyze a project", "lookup ."} {
		if !strings.Contains(got, want) {
			t.Fatalf("installer output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(f.home, ".config", "lookup", "install.json")); err != nil {
		t.Fatalf("installer receipt missing: %v", err)
	}
}

func TestInstallScriptPathPersistence(t *testing.T) {
	t.Run("PATH already contains directory", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		rc := filepath.Join(f.home, ".zshrc")
		writeFile(t, rc, "# keep exactly\n")
		output, err := f.run("PATH=" + f.installDir + ":" + f.fakeBin + ":/usr/bin:/bin")
		if err != nil {
			t.Fatalf("install.sh failed: %v\n%s", err, output)
		}
		assertFileContent(t, rc, "# keep exactly\n")
		if strings.Contains(string(output), "Added ") {
			t.Fatalf("PATH-present install claimed config change:\n%s", output)
		}
	})

	for _, tc := range []struct {
		name, shell, rc, line string
	}{
		{"zsh", "/bin/zsh", ".zshrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"bash", "/bin/bash", ".bashrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"fish", "/usr/bin/fish", ".config/fish/config.fish", `fish_add_path "$HOME/.local/bin"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnixInstallerFixture(t)
			rc := filepath.Join(f.home, tc.rc)
			writeFile(t, rc, "# existing")
			output, err := f.run("SHELL=" + tc.shell)
			if err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, output)
			}
			assertFileContent(t, rc, "# existing\n"+tc.line+"\n")
			for _, want := range []string{"Added ", tc.rc, "Restart your terminal"} {
				if !strings.Contains(string(output), want) {
					t.Fatalf("output missing %q:\n%s", want, output)
				}
			}
		})
	}

	t.Run("repeated install", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		for range 2 {
			if output, err := f.run(); err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, output)
			}
		}
		data := readFile(t, filepath.Join(f.home, ".zshrc"))
		if strings.Count(data, `export PATH="$HOME/.local/bin:$PATH"`) != 1 {
			t.Fatalf("PATH line count != 1:\n%s", data)
		}
	})

	t.Run("unsupported shell", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		output, err := f.run("SHELL=/bin/tcsh")
		if err != nil {
			t.Fatalf("install.sh failed: %v\n%s", err, output)
		}
		for _, rc := range []string{".zshrc", ".bashrc", ".config/fish/config.fish"} {
			if _, err := os.Stat(filepath.Join(f.home, rc)); !os.IsNotExist(err) {
				t.Fatalf("unsupported shell modified %s", rc)
			}
		}
		if !strings.Contains(string(output), `export PATH="`+f.installDir+`:$PATH"`) {
			t.Fatalf("manual PATH guidance missing:\n%s", output)
		}
	})

	t.Run("ambiguous shell", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		writeExecutable(t, filepath.Join(f.fakeBin, "getent"), "#!/bin/sh\nprintf 'lookupfixture:x:1:1::/tmp:/bin/zsh\\n'\n")
		output, err := f.run("SHELL=/bin/bash")
		if err != nil {
			t.Fatalf("install.sh failed: %v\n%s", err, output)
		}
		if _, err := os.Stat(filepath.Join(f.home, ".zshrc")); !os.IsNotExist(err) {
			t.Fatal("ambiguous shell modified .zshrc")
		}
		if !strings.Contains(string(output), "Add it to your shell") {
			t.Fatalf("manual guidance missing:\n%s", output)
		}
	})

	t.Run("custom install directory", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		f.installDir = filepath.Join(f.home, "custom tools", "bin")
		output, err := f.run()
		if err != nil {
			t.Fatalf("install.sh failed: %v\n%s", err, output)
		}
		assertFileContent(t, filepath.Join(f.home, ".zshrc"), `export PATH="$HOME/custom tools/bin:$PATH"`+"\n")
	})
}

func TestInstallScriptNonTTYHasNoSpinnerNoise(t *testing.T) {
	f := newUnixInstallerFixture(t)
	output, err := f.run()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}
	if bytes.ContainsAny(output, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏\r\x1b") {
		t.Fatalf("non-TTY output contains spinner/control noise: %q", output)
	}
}

func TestInstallScriptFailuresPreserveErrors(t *testing.T) {
	t.Run("download", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		writeExecutable(t, filepath.Join(f.fakeBin, "curl"), "#!/bin/sh\necho 'fixture download failed' >&2\nexit 23\n")
		output, err := f.run()
		assertExitCode(t, err, 23)
		if !strings.Contains(string(output), "fixture download failed") {
			t.Fatalf("download diagnostic hidden: %s", output)
		}
	})
	t.Run("checksum", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		writeExecutable(t, filepath.Join(f.fakeBin, "sha256sum"), "#!/bin/sh\nprintf 'wrong  %s\\n' \"$1\"\n")
		output, err := f.run()
		if err == nil || !strings.Contains(string(output), "checksum verification failed") {
			t.Fatalf("checksum failure=(%v, %s)", err, output)
		}
	})
	t.Run("extraction", func(t *testing.T) {
		f := newUnixInstallerFixture(t)
		writeExecutable(t, filepath.Join(f.fakeBin, "tar"), "#!/bin/sh\necho 'fixture extraction failed' >&2\nexit 19\n")
		output, err := f.run()
		assertExitCode(t, err, 19)
		if !strings.Contains(string(output), "fixture extraction failed") {
			t.Fatalf("extraction diagnostic hidden: %s", output)
		}
	})
}

func TestInstallScriptInteractiveFailureStopsSpinner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("test uses the util-linux script command")
	}
	scriptCommand, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script command is not installed")
	}
	f := newUnixInstallerFixture(t)
	writeExecutable(t, filepath.Join(f.fakeBin, "curl"), "#!/bin/sh\nsleep 0.3\necho 'interactive download failed' >&2\nexit 23\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, scriptCommand, "-qefc", "/bin/sh install.sh", "/dev/null")
	cmd.Env = []string{
		"HOME=" + f.home,
		"PATH=" + f.fakeBin + ":/usr/bin:/bin",
		"LOOKUP_VERSION=v0.1.0",
		"LOOKUP_INSTALL_DIR=" + f.installDir,
		"SHELL=/bin/zsh",
		"USER=lookupfixture",
		"TERM=xterm-256color",
	}
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("installer did not terminate after spinner failure: %v\n%s", ctx.Err(), output)
	}
	assertExitCode(t, runErr, 23)
	if !bytes.ContainsAny(output, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("interactive output did not animate: %q", output)
	}
	if !strings.Contains(string(output), "interactive download failed") {
		t.Fatalf("interactive diagnostic hidden: %s", output)
	}
}

func TestPowerShellInstallerOnboardingContract(t *testing.T) {
	source, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	got := string(source)
	for _, want := range []string{"Lookup $releaseVersion installed", "Binary", "lookup init", "Then analyze a project", "lookup .", "GetEnvironmentVariable", "SetEnvironmentVariable", "'User'", "$env:Path"} {
		if !strings.Contains(got, want) {
			t.Fatalf("PowerShell installer missing contract %q", want)
		}
	}
	if strings.Contains(got, "'Machine'") || strings.Contains(got, "export PATH=") {
		t.Fatal("PowerShell installer contains forbidden machine/Unix PATH behavior")
	}
}

func TestPowerShellPathBehavior(t *testing.T) {
	pwsh := findPowerShell()
	if pwsh == "" {
		t.Skip("PowerShell is not installed on this test host")
	}
	temp := t.TempDir()
	productionSource, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("if ($Version -eq \"latest\")")
	mainStart := bytes.Index(productionSource, marker)
	if mainStart < 0 {
		t.Fatal("PowerShell installer main marker not found")
	}
	helpers := filepath.Join(temp, "helpers.ps1")
	if err := os.WriteFile(helpers, productionSource[:mainStart], 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(temp, "test.ps1")
	source := `. '` + strings.ReplaceAll(helpers, `'`, `''`) + `' -InstallDir 'C:\Test'
$script:userPath = 'C:\Existing;;c:\KeepCase'
$script:writes = 0
$get = { $script:userPath }
$set = { param($value) $script:userPath = $value; $script:writes++ }
$env:Path = 'C:\Process'
$changed = Add-LookupToPath 'D:\Lookup Tools\bin' $get $set
if (-not $changed -or $script:userPath -cne 'C:\Existing;;c:\KeepCase;D:\Lookup Tools\bin' -or $script:writes -ne 1) { exit 10 }
if (-not (Test-LookupPathContains $env:Path 'D:\Lookup Tools\bin')) { exit 11 }
$changed = Add-LookupToPath 'd:\lookup tools\bin\' $get $set
if ($changed -or $script:writes -ne 1 -or $script:userPath -cne 'C:\Existing;;c:\KeepCase;D:\Lookup Tools\bin') { exit 12 }
`
	writeFile(t, script, source)
	cmd := exec.Command(pwsh, "-NoProfile", "-File", script)
	cmd.Dir = "."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell PATH behavior failed: %v\n%s", err, output)
	}
}

func findPowerShell() string {
	for _, name := range []string{"pwsh", "powershell"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error=%v, want exit %d", err, want)
	}
	if exitErr.ExitCode() != want {
		t.Fatalf("exit=%d, want %d", exitErr.ExitCode(), want)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	if got := readFile(t, path); got != want {
		t.Fatalf("%s content:\n got %q\nwant %q", path, got, want)
	}
}
