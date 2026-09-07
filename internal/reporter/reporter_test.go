package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/scoring"
	"github.com/poizdev/lookup/internal/ui"
)

func sampleResult() *Result {
	return &Result{
		RepoPath:  "/work/api",
		ScannedAt: time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC),
		Scores: scoring.Scores{
			Overall: 87.5,
			ByCategory: map[finding.Category]float64{
				finding.CategorySecurity: 75,
			},
		},
		Stats: ScanStats{FilesFound: 12, FilesParsed: 10, FilesSkipped: 2, GraphNodes: 30, GraphEdges: 25, RulesRun: 8},
		Findings: []finding.Finding{{
			ID: "f-1", RuleID: "SEC-001", Category: finding.CategorySecurity,
			Severity: finding.SeverityCritical, Confidence: .9, Title: "Hardcoded API key",
			Reason: "The credential is committed.", Recommendation: "Read it from the environment.", AIReviewed: true,
			AIReview: &finding.AIAssessment{Verdict: finding.AIVerdictLikelyReal, Confidence: .96, Reason: "Request input reaches the sink.", Recommendation: "Use parameter binding.", Provider: "fake", Model: "m"},
			Evidence: finding.Evidence{
				AffectedFiles: []string{"internal/client.go:42"}, AffectedSymbols: []string{"NewClient"},
				CodeSnippet: `token := "secret"`, CallChain: []string{"main", "NewClient"},
			},
		}},
	}
}

func coverageResult() *Result {
	result := sampleResult()
	result.LanguageCoverage = []language.LangCoverage{
		{Language: "Go", Files: 2, Parse: language.StageCoverage{Supported: 2, Attempted: 2, Succeeded: 2}, Structural: language.StageCoverage{Supported: 2, Attempted: 2, Succeeded: 2}, Semantic: language.StageCoverage{Supported: 2, Attempted: 2, Succeeded: 2}},
		{Language: "Python", Files: 3},
		{Language: "TypeScript", Files: 4, Parse: language.StageCoverage{Supported: 4, Attempted: 3, Succeeded: 2}, Structural: language.StageCoverage{Supported: 4, Attempted: 2, Succeeded: 1}},
	}
	return result
}

func TestTerminalReporterRendersLanguageCoverageFromExecution(t *testing.T) {
	var output bytes.Buffer
	if err := NewTerminal(&output, ui.Capability{Unicode: true, Width: 100}, false).Report(coverageResult()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Languages detected", "Language", "Files", "Parse", "Structural", "Semantic", "Go", "Python", "TypeScript", "2/3", "1/2", "—"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("terminal coverage missing %q:\n%s", want, output.String())
		}
	}
}

func TestMarkdownReporterRendersLanguageCoverageFromExecution(t *testing.T) {
	var output bytes.Buffer
	if err := NewMarkdown(&output).Report(coverageResult()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Analysis coverage", "| Supported language | Files | Parsed | Structural | Semantic |", "| Go | 2 | 2 | 2 | 2 |", "3 candidate files used unsupported language adapters", "| TypeScript | 4 | 2/3 | 1/2 | — |"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("markdown coverage missing %q:\n%s", want, output.String())
		}
	}
}

func TestMachineReadableContractsExcludeLanguageCoverage(t *testing.T) {
	var jsonOutput bytes.Buffer
	if err := NewJSON(&jsonOutput).Report(coverageResult()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jsonOutput.String(), "language_coverage") || strings.Contains(jsonOutput.String(), "LanguageCoverage") {
		t.Fatalf("JSON contract contains language coverage: %s", jsonOutput.String())
	}

	var sarifOutput bytes.Buffer
	if err := NewSARIF(&sarifOutput, "/work/api").Report(coverageResult()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sarifOutput.String(), "language_coverage") || strings.Contains(sarifOutput.String(), "LanguageCoverage") {
		t.Fatalf("SARIF contract contains language coverage: %s", sarifOutput.String())
	}
}

func TestTerminalReporterRendersScoresSummaryAndEvidence(t *testing.T) {
	var output bytes.Buffer
	reporter := NewTerminal(&output, ui.Capability{Unicode: true, Width: 80}, false)
	if err := reporter.Report(sampleResult()); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"LOOKUP", "api", "87.5 / 100", "Security", "1 Critical", "CRITICAL", "SEC-001", "internal/client.go:42", "main → NewClient", `token := "********"`, "Read it from the environment."} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("terminal output missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), `token := "secret"`) {
		t.Fatalf("terminal output leaked a detected secret:\n%s", output.String())
	}
}

func TestHumanReportersRenderCompactAIAssessmentAndSummary(t *testing.T) {
	result := sampleResult()
	result.AIReview = &ai.ReviewStats{FindingsTotal: 10, Selected: 4, CacheHits: 3, ProviderReviews: 1, Confirmed: 1, LikelyReal: 1}
	var terminal bytes.Buffer
	if err := NewTerminal(&terminal, ui.Capability{Unicode: true, Width: 100}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Final confidence", "90%", "AI assessment", "Confirmed", "96%", "AI: Request input reaches the sink.", "AI Review", "Selected", "4 / 10", "Cached", "3", "Reviewed", "1"} {
		if !strings.Contains(terminal.String(), want) {
			t.Errorf("terminal missing %q:\n%s", want, terminal.String())
		}
	}
	var markdown bytes.Buffer
	if err := NewMarkdown(&markdown).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## AI Review", "| Selected | 4 / 10 |", "**Contextual assessment:** Confirmed · 96%", "**AI:** Request input reaches the sink.", "**AI recommendation:** Use parameter binding."} {
		if !strings.Contains(markdown.String(), want) {
			t.Errorf("markdown missing %q:\n%s", want, markdown.String())
		}
	}
}

func TestNonReviewedFindingDoesNotRenderNoisyAIText(t *testing.T) {
	result := sampleResult()
	result.Findings[0].AIReviewed = false
	result.Findings[0].AIReview = nil
	var output bytes.Buffer
	if err := NewTerminal(&output, ui.Capability{Width: 80}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(output.String()), "not reviewed") || strings.Contains(output.String(), "AI assessment") {
		t.Fatalf("noisy AI text: %s", output.String())
	}
}

func TestHumanReportersExposeNonFinalCandidateStates(t *testing.T) {
	result := sampleResult()
	result.ScoreScope = ScoreScopePartialReview
	result.Findings = nil
	result.Candidates = []finding.PotentialFinding{
		{Finding: finding.Finding{ID: "n1", RuleID: "CTX-1", Title: "Needs context"}, Decision: finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}},
		{Finding: finding.Finding{ID: "u1", RuleID: "CTX-2", Title: "Unsupported claim"}, Decision: finding.ReviewDecision{Status: finding.AdjudicationUnsupported}},
	}
	var terminal, markdown bytes.Buffer
	if err := NewTerminal(&terminal, ui.Capability{Width: 100}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	if err := NewMarkdown(&markdown).Report(result); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"terminal": terminal.String(), "markdown": markdown.String()} {
		for _, want := range []string{"NOT_REVIEWED", "UNSUPPORTED"} {
			if !strings.Contains(output, want) {
				t.Fatalf("%s output missing non-final state %q: %s", name, want, output)
			}
		}
		if strings.Contains(output, "Overall Score") || strings.Contains(output, "Overall score") {
			t.Fatalf("%s misleadingly labels a partial score as overall: %s", name, output)
		}
	}
}

func TestQuietReporterLabelsPartialReviewScore(t *testing.T) {
	result := sampleResult()
	result.ScoreScope = ScoreScopePartialReview
	var output bytes.Buffer
	if err := NewTerminal(&output, ui.Capability{}, true).Report(result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Lookup score:") || !strings.Contains(output.String(), "Static Risk:") {
		t.Fatalf("misleading quiet output: %s", output.String())
	}
}

func TestTerminalSnippetWrapsToTerminalWidth(t *testing.T) {
	result := sampleResult()
	result.Findings[0].Evidence.CodeSnippet = `token := "sk-proj-123456789abcdefghijklmnopqrstuvwxyz" // a deliberately very long source line`
	var output bytes.Buffer
	if err := NewTerminal(&output, ui.Capability{Width: 50}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "   | ") && len([]rune(line)) > 50 {
			t.Fatalf("snippet line exceeds terminal width: %q", line)
		}
	}
	if strings.Contains(output.String(), "123456789") || !strings.Contains(output.String(), "sk-proj-********") {
		t.Fatalf("secret prefix was not safely redacted: %s", output.String())
	}
}

func TestTerminalTrueZeroInteractiveUsesSeriousResultAndOneApprovedFlavor(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/checkout/repo", ScannedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, false, HumanOptions{
		CanonicalFindingCount: 0,
		VisibleFindingCount:   0,
		RepositoryIdentity:    "repo",
	})
	if err := report.Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "✓ No findings detected.") != 1 {
		t.Fatalf("serious zero result count != 1:\n%s", got)
	}
	if strings.Count(got, "Suspiciously clean. Nice work.") != 1 {
		t.Fatalf("stable approved flavor count != 1:\n%s", got)
	}
}

func TestTerminalFilterZeroDoesNotUseCanonicalZeroPersonality(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/checkout/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, false, HumanOptions{
		CanonicalFindingCount: 4,
		VisibleFindingCount:   0,
		FiltersActive:         true,
		RepositoryIdentity:    "repo",
	})
	if err := report.Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "No findings match current filters.") {
		t.Fatalf("filter-zero explanation missing:\n%s", got)
	}
	if strings.Contains(got, "No findings detected") || strings.Contains(got, "Suspiciously clean. Nice work.") {
		t.Fatalf("canonical-zero personality leaked into filter-zero:\n%s", got)
	}
}

func TestTerminalUnresolvedSignalsDoNotUseZeroOrFilterCopy(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/checkout/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, false, HumanOptions{CanonicalFindingCount: 1, VisibleFindingCount: 0, RepositoryIdentity: "repo", CountsExplicit: true})
	if err := report.Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "No actionable findings.") || strings.Contains(got, "No findings detected") || strings.Contains(got, "No findings match current filters") || strings.Contains(got, "Suspiciously clean") {
		t.Fatalf("unresolved signal state is misleading:\n%s", got)
	}
}

func TestTerminalZeroFlavorIsStableAcrossCheckoutParents(t *testing.T) {
	outputs := make([]string, 2)
	for index, repoPath := range []string{"/home/user/Tests/repo", "/work/repo"} {
		var output bytes.Buffer
		result := &Result{RepoPath: repoPath, ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
		report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, false, HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: filepath.Base(repoPath)})
		if err := report.Report(result); err != nil {
			t.Fatal(err)
		}
		outputs[index] = output.String()
	}
	for _, got := range outputs {
		if !strings.Contains(got, "Suspiciously clean. Nice work.") {
			t.Fatalf("stable repository identity selected unexpected flavor:\n%s", got)
		}
	}
}

func TestTerminalNonTTYZeroHasNoPersonality(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/checkout/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: false, Unicode: true, Width: 80}, false, HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: "repo"})
	if err := report.Report(result); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); strings.Contains(got, "Suspiciously clean. Nice work.") || strings.Contains(got, "✓ No findings detected.") {
		t.Fatalf("TTY zero personality leaked into non-TTY output:\n%s", got)
	}
}

func TestTerminalNarrowLayoutStaysWithinDetectedWidth(t *testing.T) {
	tests := []struct {
		name    string
		result  *Result
		options HumanOptions
	}{
		{name: "zero", result: &Result{RepoPath: "/checkout/a-very-long-repository-name", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}, options: HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: "repo"}},
		{name: "finding", result: sampleResult(), options: HumanOptions{CanonicalFindingCount: 1, VisibleFindingCount: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			test.options.RepositoryIdentity = "repo"
			report := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 32}, false, test.options)
			if err := report.Report(test.result); err != nil {
				t.Fatal(err)
			}
			for lineNumber, line := range strings.Split(output.String(), "\n") {
				if width := runewidth.StringWidth(line); width > 32 {
					t.Fatalf("line %d width = %d, want <= 32: %q\n%s", lineNumber+1, width, line, output.String())
				}
			}
		})
	}
}

func TestMarkdownZeroStatesUseDistinctCopyAndFlavorOnlyCanonicalZero(t *testing.T) {
	for _, test := range []struct {
		name    string
		options HumanOptions
		want    string
		flavor  bool
	}{
		{name: "canonical zero", options: HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: "repo", CountsExplicit: true}, want: "No findings detected.", flavor: true},
		{name: "filter zero", options: HumanOptions{CanonicalFindingCount: 2, VisibleFindingCount: 0, FiltersActive: true, RepositoryIdentity: "repo", CountsExplicit: true}, want: "No findings match current filters."},
		{name: "unresolved only", options: HumanOptions{CanonicalFindingCount: 1, VisibleFindingCount: 0, RepositoryIdentity: "repo", CountsExplicit: true}, want: "No actionable findings are available for presentation."},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			result := &Result{RepoPath: "/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
			if err := NewMarkdown(&output, test.options).Report(result); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			if !strings.Contains(got, test.want) {
				t.Fatalf("missing %q:\n%s", test.want, got)
			}
			flavor := "*Suspiciously clean. Nice work.*"
			if count := strings.Count(got, flavor); count != map[bool]int{true: 1, false: 0}[test.flavor] {
				t.Fatalf("zero flavor count = %d, want canonical-zero-only flavor:\n%s", count, got)
			}
			if test.flavor && !strings.Contains(got, "No findings detected.\n\n"+flavor) {
				t.Fatalf("serious zero result is not primary to the flavor:\n%s", got)
			}
			if test.name == "canonical zero" && strings.Contains(got, "| Rule | Signal | Findings |") {
				t.Fatalf("empty dominant-rules table was rendered:\n%s", got)
			}
		})
	}
}

func TestTerminalAndMarkdownCanonicalZeroUseSameFlavor(t *testing.T) {
	options := HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: "repo", RepositoryRevision: "1111111111111111111111111111111111111111", CountsExplicit: true}
	result := &Result{RepoPath: "/checkout/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	var terminalOutput, markdownOutput bytes.Buffer
	if err := NewTerminalWithOptions(&terminalOutput, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, false, options).Report(result); err != nil {
		t.Fatal(err)
	}
	if err := NewMarkdown(&markdownOutput, options).Report(result); err != nil {
		t.Fatal(err)
	}
	flavor := zeroFindingFlavor(options.RepositoryIdentity, options.RepositoryRevision)
	if strings.Count(terminalOutput.String(), flavor) != 1 || strings.Count(markdownOutput.String(), "*"+flavor+"*") != 1 {
		t.Fatalf("reporters did not use the same single flavor %q:\nterminal:\n%s\nmarkdown:\n%s", flavor, terminalOutput.String(), markdownOutput.String())
	}
}

func TestTerminalQuietCanonicalZeroHasNoPersonality(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/checkout/repo", Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	options := HumanOptions{CanonicalFindingCount: 0, RepositoryIdentity: "repo", RepositoryRevision: "1111111111111111111111111111111111111111", CountsExplicit: true}
	if err := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Width: 80}, true, options).Report(result); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "Static Risk: 0.0/100\n" {
		t.Fatalf("quiet canonical-zero output = %q", got)
	}
}

func TestMarkdownSingleMaintainabilityFindingHasAccurateCompactCounts(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}, Findings: []finding.Finding{{RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1, Title: "High complexity"}}}
	if err := NewMarkdown(&output, HumanOptions{CanonicalFindingCount: 1, VisibleFindingCount: 1}).Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "Showing the 0 highest-impact findings") || strings.Contains(got, "additional findings are not expanded") {
		t.Fatalf("single finding reported as omitted:\n%s", got)
	}
	if strings.Count(got, "### 1. [Medium] GO-MNT-002") != 1 {
		t.Fatalf("single finding heading count is wrong:\n%s", got)
	}
}

func TestMarkdownSeparatesDeterministicObservationFromContextualAssessment(t *testing.T) {
	var output bytes.Buffer
	result := sampleResult()
	result.Findings[0].Observation = "A deterministic sink call was observed."
	if err := NewMarkdown(&output, HumanOptions{CanonicalFindingCount: 1, VisibleFindingCount: 1}).Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"**Deterministic observation:**", "**Contextual assessment:**", "```\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "```go") {
		t.Fatalf("language-agnostic evidence was mislabeled as Go:\n%s", got)
	}
}

func TestMarkdownExplainsAnalysisAndReviewCoverage(t *testing.T) {
	var output bytes.Buffer
	result := coverageResult()
	result.ReviewSummary = &ai.ReviewSummary{TotalStaticCandidates: 1, ContextualCandidates: 1, ProviderReviewed: 1, Confirmed: 1}
	if err := NewMarkdown(&output, HumanOptions{CanonicalFindingCount: len(result.Findings), VisibleFindingCount: len(result.Findings), CountsExplicit: true}).Report(result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		"Coverage describes which candidate source files reached each deterministic analysis stage.",
		"Static-authoritative signals need no contextual review; context-required signals remain unresolved unless reviewed.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("coverage explanation missing %q:\n%s", want, got)
		}
	}
}

func TestTerminalNoColorContainsNoANSI(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/repo", ScannedAt: time.Now(), Scores: scoring.Scores{ByCategory: map[finding.Category]float64{}}}
	if err := NewTerminalWithOptions(&output, ui.Capability{IsTTY: true, Unicode: true, Color: false, Width: 80}, false, HumanOptions{CanonicalFindingCount: 0, CountsExplicit: true, RepositoryIdentity: "repo"}).Report(result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("NO_COLOR presentation contains ANSI: %q", output.String())
	}
}

func TestTerminalReporterQuietWritesOneScoreLine(t *testing.T) {
	var output bytes.Buffer
	reporter := NewTerminal(&output, ui.Capability{Width: 80}, true)
	if err := reporter.Report(sampleResult()); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "Static Risk: 87.5/100\n"; got != want {
		t.Fatalf("quiet output = %q, want %q", got, want)
	}
}

func TestMarkdownReporterRendersSummaryAndFindingDetails(t *testing.T) {
	var output bytes.Buffer
	if err := NewMarkdown(&output).Report(sampleResult()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Lookup Report", "| Static Risk Score | 87.5 / 100 |", "Verified Health Score", "Successfully analyzed files", "Hardcoded API key", "```\n", "main → NewClient", "AI reviewed", "Read it from the environment."} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("markdown output missing %q:\n%s", want, output.String())
		}
	}
}

func TestHumanReportsBoundRepetitiveMaintainabilityDetailsWithoutChangingTotals(t *testing.T) {
	result := sampleResult()
	result.Findings = nil
	for i := 0; i < 30; i++ {
		result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1, Title: "Complexity", Evidence: finding.Evidence{AffectedFiles: []string{fmt.Sprintf("f%02d.go:%d", i, i+1)}}})
	}
	result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-SEC-001", Category: finding.CategorySecurity, Severity: finding.SeverityCritical, Confidence: .95, Title: "Secret"})

	var markdown bytes.Buffer
	if err := NewMarkdown(&markdown).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| Actionable findings | 31 |", "GO-MNT-002 · Complexity", "Showing the 25", "5 additional findings are not expanded", "GO-SEC-001"} {
		if !strings.Contains(markdown.String(), want) {
			t.Errorf("markdown missing %q", want)
		}
	}
	if got := strings.Count(markdown.String(), "[Medium] GO-MNT-002 · Complexity"); got != 25 {
		t.Fatalf("markdown details = %d, want 25", got)
	}

	var terminal bytes.Buffer
	if err := NewTerminal(&terminal, ui.Capability{Width: 100}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "GO-MNT-002 · 30 findings") || strings.Count(terminal.String(), "GO-MNT-002 · Complexity") != 25 {
		t.Fatalf("terminal compaction missing:\n%s", terminal.String())
	}
}

func TestMarkdownDetailedExpandsEveryFinding(t *testing.T) {
	result := repetitiveResult(30)
	var output bytes.Buffer
	if err := NewMarkdown(&output, HumanOptions{Detailed: true}).Report(result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "[Medium] GO-MNT-002 · Complexity"); got != 30 {
		t.Fatalf("detailed blocks = %d, want 30", got)
	}
	if strings.Contains(output.String(), "not expanded") {
		t.Fatalf("detailed report contains omission message: %s", output.String())
	}
}

func TestTerminalDetailedExpandsEveryFinding(t *testing.T) {
	result := repetitiveResult(30)
	var output bytes.Buffer
	if err := NewTerminal(&output, ui.Capability{Width: 100}, false, true).Report(result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "GO-MNT-002 · Complexity"); got != 30 {
		t.Fatalf("terminal detailed blocks = %d, want 30", got)
	}
	if strings.Contains(output.String(), "omitted from details") {
		t.Fatalf("detailed terminal contains omission message")
	}
}

func TestCompactMarkdownHeaderPathsCountsAndOrderingAreDeterministic(t *testing.T) {
	result := repetitiveResult(30)
	result.RepoPath = "/home/user/work/repo"
	result.Stats = ScanStats{FilesFound: 12, FilesParsed: 10, FilesSkipped: 37, GraphNodes: 40, GraphEdges: 50, RulesRun: 6}
	result.LanguageCoverage = []language.LangCoverage{
		{Language: "Go", Files: 10, Parse: language.StageCoverage{Supported: 10, Attempted: 10, Succeeded: 9}, Structural: language.StageCoverage{Supported: 10, Attempted: 9, Succeeded: 9}, Semantic: language.StageCoverage{Supported: 10, Attempted: 9, Succeeded: 9}},
		{Language: "Text", Files: 2},
	}
	result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-SEC-001", Category: finding.CategorySecurity, Severity: finding.SeverityCritical, Confidence: .9, Title: "Secret", Evidence: finding.Evidence{AffectedFiles: []string{"/home/user/work/repo/cmd/main.go:9"}, Steps: []finding.EvidenceStep{{Kind: "evidence", File: "/home/user/work/repo/cmd/main.go", Line: 9, Message: "literal observed"}}}})
	render := func() string {
		var out bytes.Buffer
		if err := NewMarkdown(&out).Report(result); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	first, second := render(), render()
	if first != second {
		t.Fatal("Markdown output is not deterministic")
	}
	for _, want := range []string{"# Lookup Report", "## Overview", "## Findings by severity", "Critical", "## Static risk by category", "## Dominant rules", "GO-MNT-002", "30", "## Analysis coverage", "Candidate source files", "Successfully analyzed files", "Parse failures", "Excluded/unsupported discovery entries", "cmd/main.go:9", "<details>", "## Findings"} {
		if !strings.Contains(first, want) {
			t.Errorf("compact Markdown missing %q", want)
		}
	}
	for _, want := range []string{"| Actionable findings | 31 |", "| Critical | 1 | 0 |", "| GO-MNT-002 | Complexity | 30 |"} {
		if !strings.Contains(first, want) {
			t.Errorf("summary count missing %q", want)
		}
	}
	if strings.Contains(first, "/home/user/work/repo/cmd/main.go") {
		t.Fatalf("absolute path leaked:\n%s", first)
	}
	if strings.Index(first, "GO-SEC-001 · Secret") > strings.Index(first, "GO-MNT-002 · Complexity") {
		t.Fatal("critical security finding was not prioritized")
	}
}

func TestCompactSelectionUsesHighestImpactAndCompactsAnyRepetitiveMaintainabilityRule(t *testing.T) {
	result := repetitiveResult(30)
	for i := 0; i < 30; i++ {
		result.Findings = append(result.Findings, finding.Finding{RuleID: "CUSTOM-MNT", Category: finding.CategoryMaintainability, Severity: finding.SeverityLow, Confidence: float64(i) / 30, Title: fmt.Sprintf("Signal %02d", i)})
	}
	var output bytes.Buffer
	if err := NewMarkdown(&output).Report(result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "[Low] CUSTOM-MNT · Signal"); got != 25 {
		t.Fatalf("custom repetitive blocks = %d, want 25", got)
	}
	if strings.Contains(output.String(), "[Low] CUSTOM-MNT · Signal 00") || !strings.Contains(output.String(), "[Low] CUSTOM-MNT · Signal 29") {
		t.Fatal("top-N did not retain highest confidence signals")
	}
}

func TestCompactMetricSelectionUsesLargestMeasuredValuesFirst(t *testing.T) {
	result := repetitiveResult(0)
	for i := 1; i <= 30; i++ {
		result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-MNT-001", Category: finding.CategoryMaintainability, Severity: finding.SeverityHigh, Confidence: 1, Title: fmt.Sprintf("Function is excessively long (%d lines)", i)})
	}
	var output bytes.Buffer
	if err := NewMarkdown(&output).Report(result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "[High] GO-MNT-001 · Function is excessively long (1 lines)") || !strings.Contains(output.String(), "[High] GO-MNT-001 · Function is excessively long (30 lines)") {
		t.Fatal("compact metric selection did not retain largest measured values")
	}
}

func repetitiveResult(count int) *Result {
	result := &Result{RepoPath: "/repo", Scores: scoring.Scores{Overall: 80, ByCategory: map[finding.Category]float64{}}}
	for i := 0; i < count; i++ {
		result.Findings = append(result.Findings, finding.Finding{RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1, Title: "Complexity", Evidence: finding.Evidence{AffectedFiles: []string{fmt.Sprintf("/repo/f%02d.go:%d", i, i+1)}}})
	}
	return result
}

func TestHumanReportersSummarizeStructuredEvidence(t *testing.T) {
	result := sampleResult()
	result.Findings[0].Evidence.Steps = []finding.EvidenceStep{
		{Kind: "source", File: "handler.go", Line: 3, Message: "HTTP query input"},
		{Kind: "propagation", File: "handler.go", Line: 6, Message: "assigned to query"},
		{Kind: "sink", File: "repository.go", Line: 9, Message: "dynamic SQL execution"},
	}
	var terminal bytes.Buffer
	if err := NewTerminal(&terminal, ui.Capability{Width: 100}, false).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"evidence: source handler.go:3", "propagation handler.go:6", "sink repository.go:9"} {
		if !strings.Contains(terminal.String(), want) {
			t.Errorf("terminal evidence missing %q:\n%s", want, terminal.String())
		}
	}

	var markdown bytes.Buffer
	if err := NewMarkdown(&markdown).Report(result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**Evidence:**", "source `handler.go:3` — HTTP query input", "sink `repository.go:9` — dynamic SQL execution"} {
		if !strings.Contains(markdown.String(), want) {
			t.Errorf("markdown evidence missing %q:\n%s", want, markdown.String())
		}
	}
}

func TestJSONReporterWritesTheCompleteResult(t *testing.T) {
	var output bytes.Buffer
	if err := NewJSON(&output).Report(sampleResult()); err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.RepoPath != "/work/api" || decoded.Scores.Overall != 87.5 || len(decoded.Findings) != 1 || decoded.Stats.RulesRun != 8 {
		t.Fatalf("incomplete result: %#v", decoded)
	}
	if decoded.Findings[0].Evidence.CodeSnippet != `token := "secret"` {
		t.Fatalf("machine-readable evidence was unexpectedly changed: %#v", decoded.Findings[0].Evidence)
	}
}

func TestFilterKeepsRequestedCategoryAndMinimumSeverity(t *testing.T) {
	items := []finding.Finding{
		{ID: "critical-security", Category: finding.CategorySecurity, Severity: finding.SeverityCritical},
		{ID: "low-security", Category: finding.CategorySecurity, Severity: finding.SeverityLow},
		{ID: "high-correctness", Category: finding.CategoryCorrectness, Severity: finding.SeverityHigh},
	}
	got, err := Filter(items, "high", "security")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "critical-security" {
		t.Fatalf("Filter() = %#v", got)
	}
}

func TestFilterRejectsUnknownValues(t *testing.T) {
	for _, test := range []struct{ severity, category string }{
		{severity: "urgent"},
		{category: "reliability"},
	} {
		if _, err := Filter(nil, test.severity, test.category); err == nil {
			t.Errorf("Filter(%q, %q) error = nil", test.severity, test.category)
		}
	}
}
