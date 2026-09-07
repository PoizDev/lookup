package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/scoring"
	"github.com/poizdev/lookup/internal/ui"
)

func TestRootFlagsAcceptIncludeTests(t *testing.T) {
	command := &cobra.Command{Use: "lookup"}
	var flags config.CLIFlags
	var debug bool
	bindRootFlags(command, &flags, &debug)

	if err := command.ParseFlags([]string{"--include-tests"}); err != nil {
		t.Fatalf("parse --include-tests: %v", err)
	}
	if !flags.IncludeTests {
		t.Fatal("IncludeTests = false after parsing --include-tests")
	}
}

func TestRootFlagsAcceptDetailed(t *testing.T) {
	command := &cobra.Command{Use: "lookup"}
	var flags config.CLIFlags
	var debug bool
	bindRootFlags(command, &flags, &debug)
	if err := command.ParseFlags([]string{"--detailed"}); err != nil {
		t.Fatal(err)
	}
	if !flags.Detailed {
		t.Fatal("Detailed = false after parsing --detailed")
	}
}

func TestRootFlagsAcceptAIReviewModesAndNoAIAlias(t *testing.T) {
	command := &cobra.Command{Use: "lookup"}
	var flags config.CLIFlags
	var debug bool
	bindRootFlags(command, &flags, &debug)

	if err := command.ParseFlags([]string{"--ai-review=all", "--no-ai"}); err != nil {
		t.Fatalf("parse AI flags: %v", err)
	}
	if flags.AIReview != "all" || !flags.NoAI {
		t.Fatalf("flags = %#v", flags)
	}
}

func TestRootFlagsAcceptReviewBudgetOverrides(t *testing.T) {
	command := &cobra.Command{Use: "lookup"}
	var flags config.CLIFlags
	var debug bool
	bindRootFlags(command, &flags, &debug)
	if err := command.ParseFlags([]string{"--ai-max-findings=9", "--ai-max-requests=2", "--ai-max-input-tokens=12000", "--ai-max-output-tokens=3000"}); err != nil {
		t.Fatal(err)
	}
	if flags.MaxReviewedFindings != 9 || flags.MaxProviderRequests != 2 || flags.MaxEstimatedInputTokens != 12000 || flags.MaxOutputTokens != 3000 {
		t.Fatalf("flags = %#v", flags)
	}
}

func TestPathErrorClassifiesPermissionAndMissingPath(t *testing.T) {
	if err := pathError("scan", os.ErrPermission); !apperror.IsKind(err, apperror.KindFilesystemPermission) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("permission error = %#v", err)
	}
	if err := pathError("scan", os.ErrNotExist); !apperror.IsKind(err, apperror.KindPathNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-path error = %#v", err)
	}
}

func TestVersionShortIsOnePlainLine(t *testing.T) {
	for _, version := range []string{"dev", "0.1.0"} {
		t.Run(version, func(t *testing.T) {
			var output bytes.Buffer
			cmd := newVersionCommand(version, "abc123", "2026-08-23")
			cmd.SetOut(&output)
			cmd.SetArgs([]string{"--short"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if got, want := output.String(), version+"\n"; got != want {
				t.Fatalf("short version = %q, want %q", got, want)
			}
			if strings.Contains(output.String(), "\x1b[") || strings.Contains(output.String(), "lookup") || strings.ContainsRune(output.String(), '⣶') {
				t.Fatalf("short version contains decoration: %q", output.String())
			}
		})
	}
}

func TestVersionHumanNonTTYIsCompactAndComplete(t *testing.T) {
	var output bytes.Buffer
	cmd := newVersionCommand("dev", "none", "unknown")
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"lookup\n\n", "Version   dev", "Commit    none", "Built     unknown", "Source    github.com/poizdev/lookup"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human version missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "██╗") || strings.Contains(got, "vdev") || strings.Contains(got, "\x1b[") {
		t.Fatalf("human version contains legacy or unsafe decoration: %q", got)
	}
}

func TestRenderResultRejectsUnknownOutputFormat(t *testing.T) {
	err := renderResult(config.OutputConfig{Format: "xml"}, &reporter.Result{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("renderResult error = %v", err)
	}
}

func TestRenderResultWritesJSONToProvidedOutput(t *testing.T) {
	var output bytes.Buffer
	result := &reporter.Result{RepoPath: "/repo", Scores: scoring.Scores{Overall: 91}}
	if err := renderResult(config.OutputConfig{Format: "json"}, result, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"repo_path": "/repo"`) || !strings.Contains(output.String(), `"overall": 91`) {
		t.Fatalf("JSON output incomplete: %s", output.String())
	}
}

func TestRenderResultWritesMarkdownToConfiguredPathOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	var output bytes.Buffer
	result := &reporter.Result{RepoPath: "/repo", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	if err := renderResult(config.OutputConfig{Format: "markdown", ReportPath: path}, result, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", output.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Lookup Report") {
		t.Fatalf("markdown report incomplete: %s", data)
	}
}

func TestRenderResultSurfacesMarkdownPathOnlyAfterSuccessfulWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	var output bytes.Buffer
	result := &reporter.Result{RepoPath: "/repo", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	summary := presentationSummary{Interactive: true, Capability: ui.Capability{IsTTY: true, Unicode: true, Width: 200}, Format: "markdown", ReportPath: path, ReviewState: reviewDisabled}
	if err := renderResultWithPresentation(config.OutputConfig{Format: "markdown", ReportPath: path}, result, &output, summary.Capability, summary); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "Report written to "+path) || !strings.Contains(got, "Scan complete") {
		t.Fatalf("completion output = %q", got)
	}
}

func TestRenderResultMarkdownFailureHasNoSuccessCopy(t *testing.T) {
	var output bytes.Buffer
	path := filepath.Join(t.TempDir(), "missing", "report.md")
	result := &reporter.Result{RepoPath: "/repo", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	summary := presentationSummary{Interactive: true, Capability: ui.Capability{IsTTY: true, Unicode: true, Width: 80}, Format: "markdown", ReportPath: path, ReviewState: reviewDisabled}
	err := renderResultWithPresentation(config.OutputConfig{Format: "markdown", ReportPath: path}, result, &output, summary.Capability, summary)
	if err == nil {
		t.Fatal("renderResultWithPresentation error = nil")
	}
	if got := output.String(); strings.Contains(got, "Report written") || strings.Contains(got, "Scan complete") {
		t.Fatalf("success copy followed report failure: %q", got)
	}
}

func TestRenderResultPropagatesDetailedToMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	result := &reporter.Result{RepoPath: "/repo", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	for i := 0; i < 30; i++ {
		result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1, Title: "Complexity"})
	}
	if err := renderResult(config.OutputConfig{Format: "markdown", ReportPath: path, Detailed: true}, result, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "GO-MNT-002 · Complexity"); got != 30 {
		t.Fatalf("detailed blocks = %d, want 30", got)
	}
}

func TestDetailedDoesNotChangeMachineReadableFindingCounts(t *testing.T) {
	result := &reporter.Result{RepoPath: "/repo", ToolVersion: "0.1.0"}
	for i := 0; i < 30; i++ {
		result.Findings = append(result.Findings, finding.Finding{ID: fmt.Sprintf("f-%d", i), RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Title: "Complexity"})
	}
	for _, format := range []string{"json", "sarif"} {
		var compact, detailed bytes.Buffer
		if err := renderResult(config.OutputConfig{Format: format}, result, &compact); err != nil {
			t.Fatal(err)
		}
		if err := renderResult(config.OutputConfig{Format: format, Detailed: true}, result, &detailed); err != nil {
			t.Fatal(err)
		}
		if compact.String() != detailed.String() {
			t.Fatalf("--detailed changed %s output", format)
		}
		needle := `"rule_id"`
		if format == "sarif" {
			needle = `"ruleId"`
		}
		if got := strings.Count(detailed.String(), needle); got != 30 {
			t.Fatalf("%s finding count = %d, want 30", format, got)
		}
	}
}

func TestMachineOutputsExcludeHumanPresentation(t *testing.T) {
	result := &reporter.Result{RepoPath: "/repo", ToolVersion: "0.1.0", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	for _, format := range []string{"json", "sarif"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			capability := ui.Capability{IsTTY: true, Unicode: true, Color: true, Width: 80}
			summary := presentationSummary{Interactive: true, Capability: capability, Format: format, CanonicalFindingCount: 0, ReviewState: reviewUnnecessary}
			if err := renderResultWithPresentation(config.OutputConfig{Format: format}, result, &output, capability, summary); err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
				t.Fatalf("invalid %s: %v\n%s", format, err, output.String())
			}
			for _, forbidden := range []string{"No findings detected", "Suspiciously clean", "Scan complete", "AI review unnecessary", "\x1b[", "Analyzing..."} {
				if strings.Contains(output.String(), forbidden) {
					t.Fatalf("%s contains human presentation %q: %s", format, forbidden, output.String())
				}
			}
		})
	}
}
