package completion

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Shell string

const (
	Unknown    Shell = "unknown"
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

const (
	profileMarkerStart = "# >>> lookup completion >>>"
	profileMarkerEnd   = "# <<< lookup completion <<<"
)

type Paths struct {
	Script      string
	Profiles    []string
	ProfileLine string
}

type InstallOptions struct {
	Shell     Shell
	GOOS      string
	Home      string
	ConfigDir string
	DataDir   string
	Generate  func() ([]byte, error)
}

type InstallResult struct {
	Shell      Shell
	ScriptPath string
	Profile    string
}

func ParseShell(value string) Shell {
	return DetectCandidates(value)
}

func DetectCandidates(candidates ...string) Shell {
	for _, candidate := range candidates {
		name := strings.ToLower(filepath.Base(strings.TrimSpace(candidate)))
		name = strings.TrimSuffix(name, ".exe")
		switch name {
		case "bash":
			return Bash
		case "zsh":
			return Zsh
		case "fish":
			return Fish
		case "pwsh", "powershell":
			return PowerShell
		}
	}
	return Unknown
}

func DetectShell() Shell {
	parent := ""
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", os.Getppid())); err == nil {
			parent = strings.TrimSpace(string(data))
		}
	}
	if parent == "" && runtime.GOOS != "windows" {
		if data, err := exec.Command("ps", "-p", strconv.Itoa(os.Getppid()), "-o", "comm=").Output(); err == nil {
			parent = strings.TrimSpace(string(data))
		}
	}
	return DetectCandidates(parent, os.Getenv("SHELL"), os.Getenv("COMSPEC"), shellFromEnvironment())
}

func shellFromEnvironment() string {
	if os.Getenv("PSModulePath") != "" || os.Getenv("POWERSHELL_DISTRIBUTION_CHANNEL") != "" {
		return "powershell"
	}
	return ""
}

func ResolvePaths(shell Shell, goos, home, configDir, dataDir string) (Paths, error) {
	if home == "" || configDir == "" {
		return Paths{}, errors.New("home and config directories are required")
	}
	if dataDir == "" {
		dataDir = filepath.Join(home, ".local", "share")
	}
	switch shell {
	case Bash:
		return Paths{Script: filepath.Join(dataDir, "bash-completion", "completions", "lookup")}, nil
	case Zsh:
		script := filepath.Join(dataDir, "zsh", "site-functions", "_lookup")
		line := fmt.Sprintf("fpath=(%q $fpath)\nautoload -Uz compinit && compinit", filepath.Dir(script))
		return Paths{Script: script, Profiles: []string{filepath.Join(home, ".zshrc")}, ProfileLine: line}, nil
	case Fish:
		return Paths{Script: filepath.Join(configDir, "fish", "completions", "lookup.fish")}, nil
	case PowerShell:
		script := filepath.Join(configDir, "lookup", "completions", "lookup.ps1")
		profileDirs := []string{filepath.Join(home, "Documents", "PowerShell")}
		if goos != "windows" {
			profileDirs = []string{filepath.Join(configDir, "powershell")}
		} else {
			profileDirs = append(profileDirs, filepath.Join(home, "Documents", "WindowsPowerShell"))
		}
		profiles := make([]string, len(profileDirs))
		for index, directory := range profileDirs {
			profiles[index] = filepath.Join(directory, "Microsoft.PowerShell_profile.ps1")
		}
		return Paths{Script: script, Profiles: profiles, ProfileLine: fmt.Sprintf(". '%s'", strings.ReplaceAll(script, "'", "''"))}, nil
	default:
		return Paths{}, fmt.Errorf("unsupported shell %q", shell)
	}
}

func Install(options InstallOptions) (InstallResult, error) {
	if options.Generate == nil {
		return InstallResult{}, errors.New("completion generator is required")
	}
	paths, err := ResolvePaths(options.Shell, options.GOOS, options.Home, options.ConfigDir, options.DataDir)
	if err != nil {
		return InstallResult{}, err
	}
	script, err := options.Generate()
	if err != nil {
		return InstallResult{}, fmt.Errorf("generate %s completion: %w", options.Shell, err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Script), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create completion directory: %w", err)
	}
	if err := os.WriteFile(paths.Script, script, 0o600); err != nil {
		return InstallResult{}, fmt.Errorf("write completion script: %w", err)
	}
	for _, profile := range paths.Profiles {
		if err := ensureProfileBlock(profile, paths.ProfileLine); err != nil {
			return InstallResult{}, err
		}
	}
	return InstallResult{Shell: options.Shell, ScriptPath: paths.Script, Profile: strings.Join(paths.Profiles, ", ")}, nil
}

func ensureProfileBlock(path, line string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read shell profile: %w", err)
	}
	block := profileMarkerStart + "\n" + line + "\n" + profileMarkerEnd
	existing := string(content)
	if start := strings.Index(existing, profileMarkerStart); start >= 0 {
		endRelative := strings.Index(existing[start:], profileMarkerEnd)
		if endRelative >= 0 {
			end := start + endRelative + len(profileMarkerEnd)
			existing = existing[:start] + block + existing[end:]
		} else {
			existing = strings.TrimRight(existing, "\n") + "\n" + block
		}
	} else {
		existing = strings.TrimRight(existing, "\n")
		if existing != "" {
			existing += "\n\n"
		}
		existing += block
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create shell profile directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(existing+"\n"), 0o600); err != nil {
		return fmt.Errorf("update shell profile: %w", err)
	}
	return nil
}
