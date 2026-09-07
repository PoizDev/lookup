package reporter

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/scoring"
	"github.com/poizdev/lookup/internal/ui"
)

type Terminal struct {
	writer   io.Writer
	cap      ui.Capability
	styles   *ui.Styles
	icons    ui.IconSet
	quiet    bool
	detailed bool
	options  HumanOptions
}

func NewTerminalWithOptions(writer io.Writer, cap ui.Capability, quiet bool, options HumanOptions) *Terminal {
	r := NewTerminal(writer, cap, quiet, options.Detailed)
	r.options = options
	return r
}

func NewTerminal(writer io.Writer, cap ui.Capability, quiet bool, detailed ...bool) *Terminal {
	theme := ui.NewTheme(cap)
	r := &Terminal{writer: writer, cap: cap, styles: ui.NewStyles(theme, cap), icons: ui.Icons(cap.Unicode), quiet: quiet}
	if len(detailed) > 0 {
		r.detailed = detailed[0]
	}
	return r
}

func (r *Terminal) Report(result *Result) error {
	if r.quiet {
		if result.VerifiedHealth.Available && result.VerifiedHealth.Scores != nil {
			_, err := fmt.Fprintf(r.writer, "Verified Health: %.1f/100\n", result.VerifiedHealth.Scores.Overall)
			return err
		}
		_, err := fmt.Fprintf(r.writer, "Static Risk: %.1f/100\n", staticScores(result).Overall)
		return err
	}
	width := r.cap.Width
	if width <= 0 {
		width = 80
	}
	if width < 60 {
		return r.reportNarrow(result, width)
	}
	if width > 100 {
		width = 100
	}
	var b strings.Builder
	divider := r.styles.Divider(width)
	repository := filepath.Base(filepath.Clean(result.RepoPath))
	fmt.Fprintf(&b, "%s\n  %s  ·  %s  ·  %s\n%s\n\n", divider, r.styles.Render("LOOKUP", r.styles.Heading()), repository, result.ScannedAt.Format("2006-01-02"), divider)
	fmt.Fprintf(&b, "  LOOKUP SCAN COMPLETE\n  Duration    %s\n  Candidates  %d source files\n  Analyzed    %d successfully\n  Parse fail  %d\n  Excluded    %d discovery entries (separate population)\n  Graph       %d nodes · %d edges · %d rules\n\n", result.Duration.Round(100*time.Millisecond), result.Stats.FilesFound, result.Stats.FilesParsed, parseFailures(result.LanguageCoverage), result.Stats.FilesSkipped, result.Stats.GraphNodes, result.Stats.GraphEdges, result.Stats.RulesRun)
	if len(result.LanguageCoverage) > 0 {
		b.WriteString("  Languages detected\n")
		fmt.Fprintf(&b, "  %-16s %7s %10s %12s %10s\n", "Language", "Files", "Parse", "Structural", "Semantic")
		for _, coverage := range result.LanguageCoverage {
			fmt.Fprintf(&b, "  %-16s %7d %10s %12s %10s\n", coverage.Language, coverage.Files, formatStageCoverage(coverage.Parse), formatStageCoverage(coverage.Structural), formatStageCoverage(coverage.Semantic))
		}
		b.WriteByte('\n')
	}
	static := staticScores(result)
	fmt.Fprintf(&b, "  Static Risk Score      %s\n", r.styles.Render(fmt.Sprintf("%.1f / 100", static.Overall), r.styles.ScoreStyle(static.Overall)))
	if result.VerifiedHealth.Available && result.VerifiedHealth.Scores != nil {
		verified := result.VerifiedHealth.Scores.Overall
		fmt.Fprintf(&b, "  Verified Health Score  %s\n\n", r.styles.Render(fmt.Sprintf("%.1f / 100", verified), r.styles.ScoreStyle(verified)))
	} else {
		fmt.Fprintf(&b, "  Verified Health Score  unavailable · %s\n\n", verifiedUnavailableReason(result))
	}
	for _, category := range categoryOrder {
		score := static.ByCategory[category]
		fmt.Fprintf(&b, "  %-16s %s  %5.1f\n", category, progressBar(score, 18, r.cap.Unicode), score)
	}
	defects, measured := findingClasses(result.Findings)
	defectCounts, measuredCounts := severityCounts(defects), severityCounts(measured)
	fmt.Fprintf(&b, "\n%s\n  Actionable defect/risk  %d · %d Critical · %d High · %d Medium · %d Low\n  Maintainability signals %d · %d Critical · %d High · %d Medium · %d Low\n%s\n", divider, len(defects), defectCounts[finding.SeverityCritical], defectCounts[finding.SeverityHigh], defectCounts[finding.SeverityMedium], defectCounts[finding.SeverityLow], len(measured), measuredCounts[finding.SeverityCritical], measuredCounts[finding.SeverityHigh], measuredCounts[finding.SeverityMedium], measuredCounts[finding.SeverityLow], divider)
	if len(result.Findings) == 0 {
		switch {
		case r.cap.IsTTY && r.options.CanonicalFindingCount == 0:
			fmt.Fprintf(&b, "\n  %s No findings detected.\n  %s\n", r.icons.Success, zeroFindingFlavor(r.options.RepositoryIdentity, r.options.RepositoryRevision))
		case r.options.CanonicalFindingCount > 0 && r.options.VisibleFindingCount == 0 && r.options.FiltersActive:
			b.WriteString("\n  No findings match current filters.\n")
		default:
			b.WriteString("\n  No actionable findings.\n")
		}
	}
	if summary := result.ReviewSummary; summary != nil {
		fmt.Fprintf(&b, "\n  Review coverage\n  Static signals        %d\n  Static authoritative  %d\n  Context required      %d\n  AI selected           %d\n  Provider reviewed     %d\n  Confirmed             %d\n  Downgraded            %d\n  Unsupported           %d\n  Uncertain             %d\n  Not reviewed          %d\n", summary.TotalStaticCandidates, summary.StaticAuthoritativeCandidates, summary.ContextualCandidates, summary.SelectedForAIReview, summary.ProviderReviewed, summary.Confirmed, summary.Downgraded, summary.Unsupported, summary.Uncertain, summary.NotReviewed)
		if result.ReviewPlan != nil {
			input, output, requests := result.ReviewPlan.EstimatedInputTokens, result.ReviewPlan.PlannedOutputTokens, result.ReviewPlan.EstimatedProviderRequests
			if result.AIReview != nil {
				input, output, requests = result.AIReview.ActualInputTokens, result.AIReview.ActualOutputTokens, result.AIReview.Batches
			}
			fmt.Fprintf(&b, "\n  Review limits\n  Input tokens          ~%d / %d\n  Output tokens         %d / %d\n  Requests              %d / %d\n  Budget exhausted      %t\n", input, result.ReviewPlan.Budget.MaxEstimatedInputTokens, output, result.ReviewPlan.Budget.MaxOutputTokens, requests, result.ReviewPlan.Budget.MaxProviderRequests, summary.BudgetExhausted)
		}
	} else if len(result.Candidates) > 0 {
		states := adjudicationCounts(result.Candidates)
		fmt.Fprintf(&b, "\n  Candidate adjudication (audit)\n  CONFIRMED      %d\n  DOWNGRADED     %d\n  UNSUPPORTED    %d\n  UNCERTAIN      %d\n  NOT_REVIEWED   %d\n", states[finding.AdjudicationConfirmed], states[finding.AdjudicationDowngraded], states[finding.AdjudicationUnsupported], states[finding.AdjudicationUncertain], states[finding.AdjudicationNotReviewed])
	}
	visible, omitted := humanFindings(result.Findings, r.detailed)
	for _, summary := range omitted {
		fmt.Fprintf(&b, "\n  %s · %d findings · showing %d · %d omitted from details\n", summary.RuleID, summary.Total, summary.Shown, summary.Total-summary.Shown)
	}
	for _, item := range visible {
		badge := r.styles.Render(strings.ToUpper(string(item.Severity)), r.styles.SeverityBadge(item.Severity))
		fmt.Fprintf(&b, "\n  %s %s  %s · %s", r.icons.Error, badge, item.RuleID, item.Title)
		if item.AIReviewed {
			b.WriteString("  AI reviewed")
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "   | Final confidence    %.0f%%\n", item.Confidence*100)
		if item.AIReview != nil {
			fmt.Fprintf(&b, "   | AI assessment       %s · %.0f%%\n", aiVerdictLabel(item.AIReview.Verdict), item.AIReview.Confidence*100)
			if item.AIReview.Reason != "" {
				fmt.Fprintf(&b, "   | AI: %s\n", item.AIReview.Reason)
			}
			if item.AIReview.Recommendation != "" {
				fmt.Fprintf(&b, "   %s Recommendation: %s\n", r.icons.Arrow, item.AIReview.Recommendation)
			}
		}
		locations := make([]string, len(item.Evidence.AffectedFiles))
		for i, value := range item.Evidence.AffectedFiles {
			locations[i] = humanLocation(result.RepoPath, value)
		}
		if location := strings.Join(locations, ", "); location != "" {
			fmt.Fprintf(&b, "   | %s\n", location)
		}
		if snippet := strings.TrimSpace(item.Evidence.CodeSnippet); snippet != "" {
			for _, line := range terminalSnippet(item.RuleID, snippet, width-5) {
				fmt.Fprintf(&b, "   | %s\n", line)
			}
		}
		if len(item.Evidence.CallChain) > 0 {
			fmt.Fprintf(&b, "   | call chain: %s\n", strings.Join(item.Evidence.CallChain, " "+r.icons.Arrow+" "))
		}
		if len(item.Evidence.Steps) > 0 {
			labels := make([]string, 0, len(item.Evidence.Steps))
			for _, step := range item.Evidence.Steps {
				label := strings.TrimSpace(step.Kind)
				if step.File != "" {
					location := humanLocation(result.RepoPath, step.File)
					if step.Line > 0 {
						location = fmt.Sprintf("%s:%d", location, step.Line)
					}
					label = strings.TrimSpace(label + " " + location)
				}
				labels = append(labels, label)
			}
			fmt.Fprintf(&b, "   | evidence: %s\n", strings.Join(labels, " "+r.icons.Arrow+" "))
		}
		if item.Recommendation != "" {
			fmt.Fprintf(&b, "   %s Recommendation: %s\n", r.icons.Arrow, item.Recommendation)
		}
	}
	if result.AIReview != nil && result.AIReview.StopReason != "" {
		fmt.Fprintf(&b, "\n  AI review incomplete  %s\n  Static analysis completed successfully.\n", result.AIReview.StopReason)
	} else if result.ReviewSummary == nil && result.AIReview != nil {
		stats := result.AIReview
		fmt.Fprintf(&b, "\n  AI Review\n  Selected       %d / %d\n  Cached         %d\n  Reviewed       %d\n  Confirmed      %d\n  Downgraded     %d\n  Unsupported    %d\n  Uncertain      %d\n", stats.Selected, stats.FindingsTotal, stats.CacheHits, stats.ProviderReviews, stats.Confirmed, stats.Downgraded, stats.Unsupported, stats.Uncertain)
	}
	fmt.Fprintf(&b, "\n%s\n", divider)
	_, err := io.WriteString(r.writer, b.String())
	return err
}

func (r *Terminal) reportNarrow(result *Result, width int) error {
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	divider := r.styles.Divider(width)
	repository := filepath.Base(filepath.Clean(result.RepoPath))
	b.WriteString(divider + "\n")
	appendTerminalWrapped(&b, "  ", "LOOKUP · "+repository, width)
	appendTerminalWrapped(&b, "  ", result.ScannedAt.Format("2006-01-02"), width)
	b.WriteString(divider + "\n\n")
	appendTerminalWrapped(&b, "  ", "SCAN COMPLETE", width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Duration: %s", result.Duration.Round(100*time.Millisecond)), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Files: %d analyzed / %d candidates", result.Stats.FilesParsed, result.Stats.FilesFound), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Parse failures: %d", parseFailures(result.LanguageCoverage)), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Excluded entries: %d", result.Stats.FilesSkipped), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Graph: %d nodes · %d edges", result.Stats.GraphNodes, result.Stats.GraphEdges), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Rules: %d", result.Stats.RulesRun), width)
	if len(result.LanguageCoverage) > 0 {
		b.WriteByte('\n')
		appendTerminalWrapped(&b, "  ", "Languages", width)
		for _, coverage := range result.LanguageCoverage {
			appendTerminalWrapped(&b, "  ", fmt.Sprintf("%s: %d files · parse %s · structural %s · semantic %s", coverage.Language, coverage.Files, formatStageCoverage(coverage.Parse), formatStageCoverage(coverage.Structural), formatStageCoverage(coverage.Semantic)), width)
		}
	}
	static := staticScores(result)
	b.WriteByte('\n')
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Static Risk: %.1f / 100", static.Overall), width)
	if result.VerifiedHealth.Available && result.VerifiedHealth.Scores != nil {
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("Verified Health: %.1f / 100", result.VerifiedHealth.Scores.Overall), width)
	} else {
		appendTerminalWrapped(&b, "  ", "Verified Health: unavailable", width)
		appendTerminalWrapped(&b, "    ", verifiedUnavailableReason(result), width)
	}
	for _, category := range categoryOrder {
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("%s: %.1f", category, static.ByCategory[category]), width)
	}

	defects, measured := findingClasses(result.Findings)
	b.WriteByte('\n')
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Defect/risk findings: %d", len(defects)), width)
	appendTerminalWrapped(&b, "  ", fmt.Sprintf("Maintainability signals: %d", len(measured)), width)
	if len(result.Findings) == 0 {
		b.WriteByte('\n')
		switch {
		case r.cap.IsTTY && r.options.CanonicalFindingCount == 0:
			appendTerminalWrapped(&b, "  ", r.icons.Success+" No findings detected.", width)
			appendTerminalWrapped(&b, "  ", zeroFindingFlavor(r.options.RepositoryIdentity, r.options.RepositoryRevision), width)
		case r.options.CanonicalFindingCount > 0 && r.options.VisibleFindingCount == 0 && r.options.FiltersActive:
			appendTerminalWrapped(&b, "  ", "No findings match current filters.", width)
		default:
			appendTerminalWrapped(&b, "  ", "No actionable findings.", width)
		}
	}
	if summary := result.ReviewSummary; summary != nil {
		b.WriteByte('\n')
		appendTerminalWrapped(&b, "  ", "Review coverage", width)
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("Signals: %d · static: %d · contextual: %d", summary.TotalStaticCandidates, summary.StaticAuthoritativeCandidates, summary.ContextualCandidates), width)
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("AI selected: %d · reviewed: %d", summary.SelectedForAIReview, summary.ProviderReviewed), width)
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("Confirmed: %d · downgraded: %d", summary.Confirmed, summary.Downgraded), width)
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("Unsupported: %d · uncertain: %d", summary.Unsupported, summary.Uncertain), width)
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("Not reviewed: %d", summary.NotReviewed), width)
	}
	visible, omitted := humanFindings(result.Findings, r.detailed)
	for _, summary := range omitted {
		b.WriteByte('\n')
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("%s · %d findings · showing %d · %d omitted", summary.RuleID, summary.Total, summary.Shown, summary.Total-summary.Shown), width)
	}
	for _, item := range visible {
		b.WriteByte('\n')
		appendTerminalWrapped(&b, "  ", fmt.Sprintf("%s %s · %s", r.icons.Error, strings.ToUpper(string(item.Severity)), item.RuleID), width)
		appendTerminalWrapped(&b, "    ", item.Title, width)
		appendTerminalWrapped(&b, "    ", fmt.Sprintf("Confidence: %.0f%%", item.Confidence*100), width)
		if item.AIReview != nil {
			appendTerminalWrapped(&b, "    ", fmt.Sprintf("AI: %s · %.0f%%", aiVerdictLabel(item.AIReview.Verdict), item.AIReview.Confidence*100), width)
			if item.AIReview.Reason != "" {
				appendTerminalWrapped(&b, "    ", item.AIReview.Reason, width)
			}
		}
		for _, value := range item.Evidence.AffectedFiles {
			appendTerminalWrapped(&b, "    ", humanLocation(result.RepoPath, value), width)
		}
		for _, line := range terminalSnippet(item.RuleID, strings.TrimSpace(item.Evidence.CodeSnippet), width-4) {
			appendTerminalWrapped(&b, "    ", line, width)
		}
		if item.Recommendation != "" {
			appendTerminalWrapped(&b, "    ", "Recommendation: "+item.Recommendation, width)
		}
	}
	b.WriteString("\n" + divider + "\n")
	_, err := io.WriteString(r.writer, b.String())
	return err
}

func appendTerminalWrapped(b *strings.Builder, prefix, value string, width int) {
	available := width - runewidth.StringWidth(prefix)
	if available < 1 {
		available = 1
	}
	for _, line := range wrapDisplayWords(value, available) {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func wrapDisplayWords(value string, width int) []string {
	if runewidth.StringWidth(value) <= width {
		return []string{value}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if runewidth.StringWidth(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
		if runewidth.StringWidth(word) > width {
			parts := wrapDisplayLine(word, width)
			lines = append(lines, parts[:len(parts)-1]...)
			current = parts[len(parts)-1]
		} else {
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func staticScores(result *Result) scoring.Scores {
	if result.StaticRisk.Scores != nil {
		return *result.StaticRisk.Scores
	}
	return result.Scores
}

func verifiedUnavailableReason(result *Result) string {
	if result.VerifiedHealth.UnavailableReason != "" {
		return result.VerifiedHealth.UnavailableReason
	}
	return "contextual verification incomplete"
}

func findingClasses(items []finding.Finding) ([]finding.Finding, []finding.Finding) {
	defects, measured := []finding.Finding{}, []finding.Finding{}
	for _, item := range items {
		if item.Category == finding.CategoryMaintainability {
			measured = append(measured, item)
		} else {
			defects = append(defects, item)
		}
	}
	return defects, measured
}

var (
	credentialAssignment = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password)\s*(?::=|=|:)\s*["'])([^"']+)(["'])`)
	knownSecretPrefix    = regexp.MustCompile(`(?i)\b(sk-(?:proj-)?)[A-Za-z0-9_-]{6,}`)
)

func terminalSnippet(ruleID, snippet string, width int) []string {
	if ruleID == "GO-SEC-001" || ruleID == "SEC-001" {
		snippet = credentialAssignment.ReplaceAllStringFunc(snippet, redactCredentialAssignment)
		snippet = knownSecretPrefix.ReplaceAllString(snippet, `${1}********`)
	}
	if width < 10 {
		width = 10
	}
	lines := make([]string, 0, 8)
	for _, sourceLine := range strings.Split(snippet, "\n") {
		lines = append(lines, wrapDisplayLine(sourceLine, width)...)
		if len(lines) >= 8 {
			lines = append(lines[:7], "…")
			break
		}
	}
	return lines
}

func redactCredentialAssignment(match string) string {
	parts := credentialAssignment.FindStringSubmatch(match)
	if len(parts) != 4 {
		return "********"
	}
	redacted := knownSecretPrefix.ReplaceAllString(parts[2], `${1}********`)
	if redacted == parts[2] {
		redacted = "********"
	}
	return parts[1] + redacted + parts[3]
}

func wrapDisplayLine(line string, width int) []string {
	if runewidth.StringWidth(line) <= width {
		return []string{line}
	}
	result := make([]string, 0, 2)
	var current strings.Builder
	currentWidth := 0
	for _, char := range line {
		charWidth := runewidth.RuneWidth(char)
		if currentWidth > 0 && currentWidth+charWidth > width {
			result = append(result, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(char)
		currentWidth += charWidth
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

func progressBar(score float64, width int, unicode bool) string {
	filled := min(max(int(score/100*float64(width)), 0), width)
	on, off := "#", "-"
	if unicode {
		on, off = "█", "░"
	}
	return strings.Repeat(on, filled) + strings.Repeat(off, width-filled)
}
func severityCounts(items []finding.Finding) map[finding.Severity]int {
	counts := map[finding.Severity]int{}
	for _, item := range items {
		counts[item.Severity]++
	}
	return counts
}
