package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/ui"
)

func renderResult(output config.OutputConfig, result *reporter.Result, stdout io.Writer, capabilities ...ui.Capability) error {
	cap := ui.Detect(stdout)
	if len(capabilities) > 0 {
		cap = capabilities[0]
	}
	return renderResultWithPresentation(output, result, stdout, cap, presentationSummary{})
}

func renderResultWithPresentation(output config.OutputConfig, result *reporter.Result, stdout io.Writer, cap ui.Capability, summary presentationSummary) error {
	format := strings.ToLower(strings.TrimSpace(output.Format))
	humanOptions := reporter.HumanOptions{
		Detailed:              output.Detailed,
		CanonicalFindingCount: summary.CanonicalFindingCount,
		VisibleFindingCount:   summary.VisibleFindingCount,
		FiltersActive:         summary.FiltersActive,
		RepositoryIdentity:    filepath.Base(filepath.Clean(result.RepoPath)),
		CountsExplicit:        true,
	}
	switch format {
	case "json":
		if err := reporter.NewJSON(stdout).Report(result); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	case "markdown":
		if summary.CanonicalFindingCount == 0 {
			humanOptions.RepositoryRevision = gitRevision(result.RepoPath)
		}
		if strings.TrimSpace(output.ReportPath) == "" {
			return fmt.Errorf("markdown report path is empty")
		}
		reportFile, err := os.Create(output.ReportPath)
		if err != nil {
			return fmt.Errorf("create markdown report: %w", err)
		}
		reportErr := reporter.NewMarkdown(reportFile, humanOptions).Report(result)
		closeErr := reportFile.Close()
		if reportErr != nil {
			return fmt.Errorf("write markdown report: %w", reportErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close markdown report: %w", closeErr)
		}
		return renderCompletion(stdout, summary)
	case "sarif":
		if err := reporter.NewSARIF(stdout, result.RepoPath).Report(result); err != nil {
			return fmt.Errorf("write SARIF report: %w", err)
		}
		return nil
	case "terminal":
		if !output.Quiet && cap.IsTTY && summary.CanonicalFindingCount == 0 {
			humanOptions.RepositoryRevision = gitRevision(result.RepoPath)
		}
		if err := reporter.NewTerminalWithOptions(stdout, cap, output.Quiet, humanOptions).Report(result); err != nil {
			return fmt.Errorf("write terminal report: %w", err)
		}
		return renderCompletion(stdout, summary)
	default:
		return fmt.Errorf("unsupported output format %q (want terminal, markdown, json, or sarif)", output.Format)
	}
}

func gitRevision(repoPath string) string {
	output, err := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
