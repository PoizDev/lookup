package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/completion"
	"github.com/spf13/cobra"
)

func TestManualCompletionStdoutIsPure(t *testing.T) {
	for _, shell := range []completion.Shell{completion.Bash, completion.Zsh, completion.Fish, completion.PowerShell} {
		t.Run(string(shell), func(t *testing.T) {
			root := &cobra.Command{Use: "lookup"}
			root.AddCommand(newCompletionCommand(root))
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"completion", string(shell)})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if output.Len() == 0 || strings.Contains(output.String(), "LOOKUP SCAN") || strings.Contains(output.String(), "Lookup —") {
				t.Fatalf("completion output is not a pure script: %q", output.String())
			}
		})
	}
}

func TestPromptShellSupportsPowerShellAndRejectsUnknown(t *testing.T) {
	if got, err := promptShell(strings.NewReader("pwsh\n"), &bytes.Buffer{}); err != nil || got != completion.PowerShell {
		t.Fatalf("promptShell = %q, %v", got, err)
	}
	if _, err := promptShell(strings.NewReader("nu\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("unknown shell error = nil")
	}
}
