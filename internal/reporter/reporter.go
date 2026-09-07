// Package reporter renders analysis results for people and automation.
package reporter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/scoring"
)

const humanAdvisoryDetailLimit = 25

type omittedRule struct {
	RuleID       string
	Total, Shown int
	Title        string
}

type HumanOptions struct {
	Detailed              bool
	CanonicalFindingCount int
	VisibleFindingCount   int
	FiltersActive         bool
	RepositoryIdentity    string
	RepositoryRevision    string
	CountsExplicit        bool
}

func humanFindings(items []finding.Finding, detailed bool) ([]finding.Finding, []omittedRule) {
	ordered := append([]finding.Finding(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		li, lj := severityRank(string(ordered[i].Severity)), severityRank(string(ordered[j].Severity))
		if li != lj {
			return li > lj
		}
		if ordered[i].Confidence != ordered[j].Confidence {
			return ordered[i].Confidence > ordered[j].Confidence
		}
		if left, right := metricImpact(ordered[i].Title), metricImpact(ordered[j].Title); left != right {
			return left > right
		}
		if ordered[i].RuleID != ordered[j].RuleID {
			return ordered[i].RuleID < ordered[j].RuleID
		}
		left, right := strings.Join(ordered[i].Evidence.AffectedFiles, "\x00"), strings.Join(ordered[j].Evidence.AffectedFiles, "\x00")
		return left < right
	})
	if detailed {
		return ordered, nil
	}
	totals, shown, titles := map[string]int{}, map[string]int{}, map[string]string{}
	for _, item := range ordered {
		if item.Category == finding.CategoryMaintainability {
			totals[item.RuleID]++
			if titles[item.RuleID] == "" {
				titles[item.RuleID] = item.Title
			}
		}
	}
	visible := make([]finding.Finding, 0, len(ordered))
	for _, item := range ordered {
		if repetitiveAdvisory(item, totals) && shown[item.RuleID] >= humanAdvisoryDetailLimit {
			continue
		}
		visible = append(visible, item)
		if repetitiveAdvisory(item, totals) {
			shown[item.RuleID]++
		}
	}
	var omitted []omittedRule
	for ruleID, total := range totals {
		if total > humanAdvisoryDetailLimit && total > shown[ruleID] {
			omitted = append(omitted, omittedRule{RuleID: ruleID, Total: total, Shown: shown[ruleID], Title: titles[ruleID]})
		}
	}
	sort.Slice(omitted, func(i, j int) bool { return omitted[i].RuleID < omitted[j].RuleID })
	return visible, omitted
}

func metricImpact(title string) int {
	open, close := strings.LastIndex(title, "("), strings.LastIndex(title, ")")
	if open < 0 || close <= open {
		return 0
	}
	fields := strings.Fields(title[open+1 : close])
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.Atoi(fields[0])
	return value
}

func repetitiveAdvisory(item finding.Finding, totals map[string]int) bool {
	return item.Category == finding.CategoryMaintainability && totals[item.RuleID] > humanAdvisoryDetailLimit
}

func humanLocation(repoPath, value string) string {
	if value == "" || repoPath == "" {
		return value
	}
	path, suffix := value, ""
	if colon := strings.LastIndex(value, ":"); colon > 0 {
		candidate := value[colon+1:]
		if candidate != "" && strings.IndexFunc(candidate, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			path, suffix = value[:colon], value[colon:]
		}
	}
	relative, err := filepath.Rel(repoPath, path)
	if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(relative) + suffix
	}
	return value
}

func parseFailures(coverage []language.LangCoverage) int {
	total := 0
	for _, item := range coverage {
		total += item.Parse.Attempted - item.Parse.Succeeded
	}
	return total
}

type ruleCount struct {
	RuleID, Title string
	Count         int
}

func dominantRules(items []finding.Finding) []ruleCount {
	counts, titles := map[string]int{}, map[string]string{}
	for _, item := range items {
		counts[item.RuleID]++
		if titles[item.RuleID] == "" {
			titles[item.RuleID] = item.Title
		}
	}
	result := make([]ruleCount, 0, len(counts))
	for id, count := range counts {
		result = append(result, ruleCount{id, titles[id], count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].RuleID < result[j].RuleID
	})
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

type Reporter interface {
	Report(result *Result) error
}

type ScoreScope = scoring.Scope

const (
	ScoreScopeStaticRisk     = scoring.ScopeStaticRisk
	ScoreScopeVerifiedHealth = scoring.ScopeVerifiedHealth
	ScoreScopeVerified       = scoring.ScopeVerifiedHealth
	ScoreScopePartialReview  = scoring.ScopeStaticRisk
)

type ScanStats struct {
	FilesFound   int `json:"files_found"`
	FilesParsed  int `json:"files_parsed"`
	FilesSkipped int `json:"files_skipped"`
	GraphNodes   int `json:"graph_nodes"`
	GraphEdges   int `json:"graph_edges"`
	RulesRun     int `json:"rules_run"`
}

type Result struct {
	RepoPath       string            `json:"repo_path"`
	ScannedAt      time.Time         `json:"scanned_at"`
	Duration       time.Duration     `json:"duration_ns"`
	ToolVersion    string            `json:"tool_version"`
	Scores         scoring.Scores    `json:"scores"`
	ScoreScope     ScoreScope        `json:"score_scope,omitempty"`
	StaticRisk     scoring.Result    `json:"static_risk"`
	VerifiedHealth scoring.Result    `json:"verified_health"`
	Findings       []finding.Finding `json:"findings"`
	// Candidates preserves non-final audit state for review coverage and
	// machine-readable adjudication details.
	Candidates       []finding.PotentialFinding `json:"candidates,omitempty"`
	Stats            ScanStats                  `json:"stats"`
	LanguageCoverage []language.LangCoverage    `json:"-"`
	AIReview         *ai.ReviewStats            `json:"ai_review,omitempty"`
	ReviewPlan       *ai.ReviewPlan             `json:"review_plan,omitempty"`
	ReviewSummary    *ai.ReviewSummary          `json:"review_summary,omitempty"`
}

func adjudicationCounts(candidates []finding.PotentialFinding) map[finding.AdjudicationStatus]int {
	counts := make(map[finding.AdjudicationStatus]int)
	for _, candidate := range candidates {
		counts[candidate.Decision.Status]++
	}
	return counts
}

func aiVerdictLabel(verdict finding.AIVerdict) string {
	switch verdict {
	case finding.AdjudicationConfirmed:
		return "Confirmed"
	case finding.AdjudicationDowngraded:
		return "Downgraded"
	case finding.AdjudicationUnsupported:
		return "Unsupported"
	default:
		return "Uncertain"
	}
}

func formatStageCoverage(stage language.StageCoverage) string {
	if stage.Supported == 0 {
		return "—"
	}
	if stage.Attempted == stage.Supported && stage.Succeeded == stage.Attempted {
		return fmt.Sprintf("%d", stage.Succeeded)
	}
	return fmt.Sprintf("%d/%d", stage.Succeeded, stage.Attempted)
}

func Filter(items []finding.Finding, minimumSeverity, category string) ([]finding.Finding, error) {
	minimumSeverity = strings.TrimSpace(minimumSeverity)
	category = strings.TrimSpace(category)
	if minimumSeverity != "" && severityRank(minimumSeverity) == 0 {
		return nil, fmt.Errorf("unsupported severity %q", minimumSeverity)
	}
	if category != "" && !knownCategory(category) {
		return nil, fmt.Errorf("unsupported category %q", category)
	}
	if strings.TrimSpace(minimumSeverity) == "" && strings.TrimSpace(category) == "" {
		return items, nil
	}
	filtered := make([]finding.Finding, 0, len(items))
	minimum := severityRank(minimumSeverity)
	for _, item := range items {
		if category != "" && !strings.EqualFold(string(item.Category), strings.TrimSpace(category)) {
			continue
		}
		if minimumSeverity != "" && severityRank(string(item.Severity)) < minimum {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func knownCategory(value string) bool {
	for _, category := range categoryOrder {
		if strings.EqualFold(string(category), value) {
			return true
		}
	}
	return false
}

func severityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "info":
		return 1
	case "low":
		return 2
	case "medium":
		return 3
	case "high":
		return 4
	case "critical":
		return 5
	default:
		return 0
	}
}
