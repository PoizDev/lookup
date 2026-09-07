package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/poizdev/lookup/internal/completion"
	"github.com/spf13/cobra"
)

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate or install shell completion",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := completion.ParseShell(args[0])
			if shell == completion.Unknown {
				return fmt.Errorf("unsupported shell %q", args[0])
			}
			return generateCompletion(root, shell, cmd.OutOrStdout())
		},
	}
	command.AddCommand(newCompletionInstallCommand(root))
	return command
}

func newCompletionInstallCommand(root *cobra.Command) *cobra.Command {
	var requested string
	command := &cobra.Command{
		Use:   "install",
		Short: "Install completion for the active shell",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := completion.ParseShell(requested)
			if requested == "" {
				shell = completion.DetectShell()
			}
			if shell == completion.Unknown {
				var err error
				shell, err = promptShell(cmd.InOrStdin(), cmd.OutOrStdout())
				if err != nil {
					return err
				}
			}
			result, err := installShellCompletion(root, shell)
			if err != nil {
				return fmt.Errorf("install %s completion: %w\nmanual command: lookup completion %s", shell, err, shell)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s completion: %s\n", shell, result.ScriptPath)
			if result.Profile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Updated shell profile: %s\n", result.Profile)
			}
			return nil
		},
	}
	command.Flags().StringVar(&requested, "shell", "", "Shell to install for (bash, zsh, fish, powershell)")
	return command
}

func installShellCompletion(root *cobra.Command, shell completion.Shell) (completion.InstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return completion.InstallResult{}, fmt.Errorf("determine home directory: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return completion.InstallResult{}, fmt.Errorf("determine configuration directory: %w", err)
	}
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = filepath.Join(home, ".local", "share")
	}
	return completion.Install(completion.InstallOptions{
		Shell: shell, GOOS: runtime.GOOS, Home: home, ConfigDir: configDir, DataDir: dataDir,
		Generate: func() ([]byte, error) {
			var output bytes.Buffer
			err := generateCompletion(root, shell, &output)
			return output.Bytes(), err
		},
	})
}

func generateCompletion(root *cobra.Command, shell completion.Shell, writer io.Writer) error {
	switch shell {
	case completion.Bash:
		return root.GenBashCompletionV2(writer, true)
	case completion.Zsh:
		return root.GenZshCompletion(writer)
	case completion.Fish:
		return root.GenFishCompletion(writer, true)
	case completion.PowerShell:
		return root.GenPowerShellCompletionWithDesc(writer)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

func promptShell(reader io.Reader, writer io.Writer) (completion.Shell, error) {
	fmt.Fprintln(writer, "Unable to detect the active shell. Select one: bash, zsh, fish, powershell")
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return completion.Unknown, err
		}
		return completion.Unknown, errorsForUnknownShell()
	}
	shell := completion.ParseShell(strings.TrimSpace(scanner.Text()))
	if shell == completion.Unknown {
		return shell, errorsForUnknownShell()
	}
	return shell, nil
}

func errorsForUnknownShell() error {
	return fmt.Errorf("shell could not be detected; rerun with --shell bash|zsh|fish|powershell")
}
