package reporter

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
)

type Markdown struct {
	writer  io.Writer
	options HumanOptions
}

func NewMarkdown(writer io.Writer, options ...HumanOptions) *Markdown {
	r := &Markdown{writer: writer}
	if len(options) > 0 {
		r.options = options[0]
	}
	return r
}

func analyzedLanguageCount(coverage []language.LangCoverage) int {
	total := 0
	for _, item := range coverage {
		if item.Parse.Succeeded > 0 {
			total++
		}
	}
	return total
}

func evidenceRepeatsReason(item finding.Finding) bool {
	reason := strings.TrimSpace(item.Reason)
	for _, step := range item.Evidence.Steps {
		if strings.EqualFold(strings.TrimSpace(step.Message), reason) {
			return true
		}
	}
	return false
}

func (r *Markdown) Report(result *Result) error {
	var b strings.Builder
	repository := filepath.Base(filepath.Clean(result.RepoPath))
	if repository == "." || repository == string(filepath.Separator) {
		repository = result.RepoPath
	}
	fmt.Fprintf(&b, "# Lookup Report\n\n**Repository:** `%s`  \n**Scanned at:** %s\n\n", repository, result.ScannedAt.Format("2006-01-02 15:04:05 MST"))
	if r.options.CountsExplicit {
		switch {
		case r.options.CanonicalFindingCount == 0:
			b.WriteString("**Status:** No findings detected.\n\n")
		case r.options.VisibleFindingCount == 0 && r.options.FiltersActive:
			b.WriteString("**Status:** Findings exist, but none match the current filters.\n\n")
		default:
			fmt.Fprintf(&b, "**Status:** %d of %d canonical findings shown.\n\n", r.options.VisibleFindingCount, r.options.CanonicalFindingCount)
		}
	}
	b.WriteString("## Overview\n\n| Metric | Value |\n|:--|--:|\n")
	static := staticScores(result)
	fmt.Fprintf(&b, "| Static Risk Score | %.1f / 100 |\n", static.Overall)
	if result.VerifiedHealth.Available && result.VerifiedHealth.Scores != nil {
		fmt.Fprintf(&b, "| Verified Health Score | %.1f / 100 |\n", result.VerifiedHealth.Scores.Overall)
	} else {
		fmt.Fprintf(&b, "| Verified Health Score | unavailable (%s) |\n", verifiedUnavailableReason(result))
	}
	fmt.Fprintf(&b, "| Actionable findings | %d |\n", len(result.Findings))
	if r.options.CountsExplicit {
		fmt.Fprintf(&b, "| Canonical findings | %d |\n| Findings shown | %d |\n", r.options.CanonicalFindingCount, len(result.Findings))
	}
	fmt.Fprintf(&b, "| Successfully analyzed files | %d / %d candidate source files |\n| Languages analyzed | %d |\n| Rules evaluated | %d |\n\n", result.Stats.FilesParsed, result.Stats.FilesFound, analyzedLanguageCount(result.LanguageCoverage), result.Stats.RulesRun)
	defects, measured := findingClasses(result.Findings)
	b.WriteString("## Finding classes\n\n| Class | Findings |\n|:--|--:|\n")
	fmt.Fprintf(&b, "| Actionable defect/risk | %d |\n| Maintainability/measured signals | %d |\n", len(defects), len(measured))
	b.WriteString("\n## Findings by severity and class\n\n| Severity | Defect/risk | Maintainability/measured |\n|:--|--:|--:|\n")
	defectCounts, measuredCounts := severityCounts(defects), severityCounts(measured)
	for _, severity := range []finding.Severity{finding.SeverityCritical, finding.SeverityHigh, finding.SeverityMedium, finding.SeverityLow, finding.SeverityInfo} {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", severity, defectCounts[severity], measuredCounts[severity])
	}
	categoryHeading := "Static risk by category"
	fmt.Fprintf(&b, "\n## %s\n\n| Category | Score |\n|:--|--:|\n", categoryHeading)
	for _, category := range categoryOrder {
		fmt.Fprintf(&b, "| %s | %.1f |\n", category, static.ByCategory[category])
	}
	dominant := dominantRules(result.Findings)
	b.WriteString("\n## Dominant rules\n\n")
	if len(dominant) == 0 {
		b.WriteString("No visible findings contribute a dominant rule.\n")
	} else {
		b.WriteString("| Rule | Signal | Findings |\n|:--|:--|--:|\n")
		for _, rule := range dominant {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", rule.RuleID, rule.Title, rule.Count)
		}
	}
	b.WriteString("\n## Analysis coverage\n\n")
	b.WriteString("Coverage describes which candidate source files reached each deterministic analysis stage.\n\n")
	if len(result.LanguageCoverage) > 0 {
		b.WriteString("| Supported language | Files | Parsed | Structural | Semantic |\n|:--|--:|--:|--:|--:|\n")
		unsupported := 0
		for _, coverage := range result.LanguageCoverage {
			if coverage.Parse.Supported == 0 {
				unsupported += coverage.Files
				continue
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n", coverage.Language, coverage.Files, formatStageCoverage(coverage.Parse), formatStageCoverage(coverage.Structural), formatStageCoverage(coverage.Semantic))
		}
		if unsupported > 0 {
			fmt.Fprintf(&b, "\n%d candidate files used unsupported language adapters and were not analyzed.\n", unsupported)
		}
	}
	fmt.Fprintf(&b, "\n- Candidate source files: %d\n- Successfully analyzed files: %d\n- Parse failures: %d\n- Excluded/unsupported discovery entries: %d (separate from candidate source files)\n\n", result.Stats.FilesFound, result.Stats.FilesParsed, parseFailures(result.LanguageCoverage), result.Stats.FilesSkipped)
	if summary := result.ReviewSummary; summary != nil {
		b.WriteString("## Review coverage\n\nStatic-authoritative signals need no contextual review; context-required signals remain unresolved unless reviewed.\n\n| Metric | Value |\n|:--|--:|\n")
		fmt.Fprintf(&b, "| Static signals | %d |\n| Static authoritative | %d |\n| Context required | %d |\n| Selected for AI review | %d |\n| Provider reviewed | %d |\n| Confirmed | %d |\n| Downgraded | %d |\n| Unsupported | %d |\n| Uncertain | %d |\n| Not reviewed | %d |\n| Context insufficient | %d |\n| Budget skipped | %d |\n\n", summary.TotalStaticCandidates, summary.StaticAuthoritativeCandidates, summary.ContextualCandidates, summary.SelectedForAIReview, summary.ProviderReviewed, summary.Confirmed, summary.Downgraded, summary.Unsupported, summary.Uncertain, summary.NotReviewed, summary.ContextInsufficient, summary.BudgetSkipped)
		if result.ReviewPlan != nil {
			input, output, requests := result.ReviewPlan.EstimatedInputTokens, result.ReviewPlan.PlannedOutputTokens, result.ReviewPlan.EstimatedProviderRequests
			if result.AIReview != nil {
				input, output, requests = result.AIReview.ActualInputTokens, result.AIReview.ActualOutputTokens, result.AIReview.Batches
			}
			fmt.Fprintf(&b, "### Review limits\n\n- Estimated input tokens: ~%d / %d\n- Planned output tokens: %d / %d\n- Provider requests: %d / %d\n- Budget exhausted: %t\n\n", input, result.ReviewPlan.Budget.MaxEstimatedInputTokens, output, result.ReviewPlan.Budget.MaxOutputTokens, requests, result.ReviewPlan.Budget.MaxProviderRequests, summary.BudgetExhausted)
		}
		if result.AIReview != nil && result.AIReview.StopReason != "" {
			fmt.Fprintf(&b, "AI review incomplete: %s. Static analysis completed successfully.\n\n", result.AIReview.StopReason)
		}
	} else if result.AIReview != nil {
		stats := result.AIReview
		b.WriteString("## AI Review\n\n| Metric | Value |\n|:--|--:|\n")
		fmt.Fprintf(&b, "| Selected | %d / %d |\n| Cached | %d |\n| Reviewed | %d |\n| Confirmed | %d |\n| Downgraded | %d |\n| Unsupported | %d |\n| Uncertain | %d |\n\n", stats.Selected, stats.FindingsTotal, stats.CacheHits, stats.ProviderReviews, stats.Confirmed, stats.Downgraded, stats.Unsupported, stats.Uncertain)
	}
	if result.ReviewSummary == nil && len(result.Candidates) > 0 {
		states := adjudicationCounts(result.Candidates)
		b.WriteString("## Candidate adjudication (audit)\n\n| State | Candidates |\n|:--|--:|\n")
		for _, status := range []finding.AdjudicationStatus{finding.AdjudicationConfirmed, finding.AdjudicationDowngraded, finding.AdjudicationUnsupported, finding.AdjudicationUncertain, finding.AdjudicationNotReviewed} {
			fmt.Fprintf(&b, "| %s | %d |\n", status, states[status])
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n## Findings\n\n")
	if len(result.Findings) == 0 {
		switch {
		case r.options.CountsExplicit && r.options.CanonicalFindingCount == 0:
			fmt.Fprintf(&b, "No findings detected.\n\n*%s*\n\n", zeroFindingFlavor(r.options.RepositoryIdentity, r.options.RepositoryRevision))
		case r.options.CountsExplicit && r.options.VisibleFindingCount == 0 && r.options.FiltersActive:
			b.WriteString("No findings match current filters.\n\n")
		default:
			b.WriteString("No actionable findings are available for presentation.\n\n")
		}
	}
	visible, omitted := humanFindings(result.Findings, r.options.Detailed)
	omittedIDs := map[string]bool{}
	for _, summary := range omitted {
		omittedIDs[summary.RuleID] = true
	}
	index := 0
	for _, item := range visible {
		if !omittedIDs[item.RuleID] {
			index++
			renderMarkdownFinding(&b, index, item, result, 3)
		}
	}
	for _, summary := range omitted {
		fmt.Fprintf(&b, "### %s · %s\n\n%d findings\n\nShowing the %d highest-impact findings.  \n%d additional findings are not expanded in the compact report.\n\n", summary.RuleID, summary.Title, summary.Total, summary.Shown, summary.Total-summary.Shown)
		for _, item := range visible {
			if item.RuleID == summary.RuleID {
				index++
				renderMarkdownFinding(&b, index, item, result, 4)
			}
		}
	}
	b.WriteString("## Technical details\n\n<details>\n<summary>Scan implementation metrics</summary>\n\n")
	fmt.Fprintf(&b, "- Graph nodes: %d\n- Graph edges: %d\n- Scan duration: %s\n- Tool version: %s\n\n</details>\n", result.Stats.GraphNodes, result.Stats.GraphEdges, result.Duration, result.ToolVersion)
	_, err := io.WriteString(r.writer, b.String())
	return err
}

func renderMarkdownFinding(b *strings.Builder, index int, item finding.Finding, result *Result, headingLevel int) {
	fmt.Fprintf(b, "%s %d. [%s] %s · %s\n\n", strings.Repeat("#", headingLevel), index, item.Severity, item.RuleID, item.Title)
	fmt.Fprintf(b, "- **Category:** %s\n- **Confidence:** %.0f%%\n", item.Category, item.Confidence*100)
	if item.ReviewPolicy == finding.ReviewPolicyStaticAuthoritative {
		b.WriteString("- **Review state:** Static-authoritative\n")
	} else if item.AIReviewed {
		b.WriteString("- **Review state:** AI reviewed\n")
	}
	if item.Observation != "" {
		fmt.Fprintf(b, "\n**Deterministic observation:** %s\n", item.Observation)
	}
	if item.AIReview != nil {
		fmt.Fprintf(b, "\n**Contextual assessment:** %s · %.0f%%\n", aiVerdictLabel(item.AIReview.Verdict), item.AIReview.Confidence*100)
		if item.AIReview.Reason != "" {
			fmt.Fprintf(b, "\n**AI:** %s\n", item.AIReview.Reason)
		}
		if item.AIReview.Recommendation != "" {
			fmt.Fprintf(b, "\n**AI recommendation:** %s\n", item.AIReview.Recommendation)
		}
	}
	if len(item.Evidence.AffectedFiles) > 0 {
		locations := make([]string, len(item.Evidence.AffectedFiles))
		for i, value := range item.Evidence.AffectedFiles {
			locations[i] = humanLocation(result.RepoPath, value)
		}
		fmt.Fprintf(b, "- **Location:** `%s`\n", strings.Join(locations, "`, `"))
	}
	if len(item.Evidence.AffectedSymbols) > 0 {
		fmt.Fprintf(b, "- **Symbols:** `%s`\n", strings.Join(item.Evidence.AffectedSymbols, "`, `"))
	}
	if len(item.Evidence.CallChain) > 0 {
		fmt.Fprintf(b, "- **Call chain:** %s\n", strings.Join(item.Evidence.CallChain, " → "))
	}
	if len(item.Evidence.Steps) > 0 {
		b.WriteString("\n**Evidence:**\n\n")
		for _, step := range item.Evidence.Steps {
			location := humanLocation(result.RepoPath, step.File)
			if step.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, step.Line)
			}
			fmt.Fprintf(b, "- %s `%s` — %s\n", step.Kind, location, step.Message)
		}
	}
	if item.Reason != "" && !evidenceRepeatsReason(item) {
		fmt.Fprintf(b, "\n**Why it matters:** %s\n", item.Reason)
	}
	if item.Evidence.CodeSnippet != "" {
		fmt.Fprintf(b, "\n```\n%s\n```\n", item.Evidence.CodeSnippet)
	}
	if item.Recommendation != "" {
		fmt.Fprintf(b, "\n**Recommendation:** %s\n", item.Recommendation)
	}
	b.WriteString("\n---\n\n")
}

var categoryOrder = []finding.Category{
	finding.CategorySecurity, finding.CategoryCorrectness, finding.CategoryArchitecture,
	finding.CategoryPerformance, finding.CategoryMaintainability,
}
