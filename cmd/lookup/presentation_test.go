package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/ui"
)

func TestDerivePresentationReviewState(t *testing.T) {
	tests := []struct {
		name    string
		ai      config.AIConfig
		summary *ai.ReviewSummary
		stats   *ai.ReviewStats
		want    reviewPresentationState
	}{
		{name: "disabled", ai: config.AIConfig{NoAI: true, ReviewMode: "off"}, summary: &ai.ReviewSummary{ContextualCandidates: 2, NotReviewed: 2}, want: reviewDisabled},
		{name: "unnecessary", ai: config.AIConfig{ReviewMode: "smart"}, summary: &ai.ReviewSummary{StaticAuthoritativeCandidates: 2}, want: reviewUnnecessary},
		{name: "complete", ai: config.AIConfig{ReviewMode: "all"}, summary: &ai.ReviewSummary{ContextualCandidates: 2, SelectedForAIReview: 2, ProviderReviewed: 2, Confirmed: 1, Unsupported: 1}, stats: &ai.ReviewStats{ProviderReviews: 2}, want: reviewComplete},
		{name: "partial", ai: config.AIConfig{ReviewMode: "all"}, summary: &ai.ReviewSummary{ContextualCandidates: 3, SelectedForAIReview: 3, ProviderReviewed: 2, Confirmed: 2, NotReviewed: 1, ProviderFailures: 1}, stats: &ai.ReviewStats{ProviderReviews: 2, Failures: 1}, want: reviewPartial},
		{name: "unavailable", ai: config.AIConfig{ReviewMode: "all"}, summary: &ai.ReviewSummary{ContextualCandidates: 2, SelectedForAIReview: 2, NotReviewed: 2, ProviderFailures: 2}, stats: &ai.ReviewStats{Failures: 2, StopReason: "provider_unavailable"}, want: reviewUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := derivePresentationSummary(presentationInput{AI: test.ai, ReviewSummary: test.summary, AIReview: test.stats})
			if got.ReviewState != test.want {
				t.Fatalf("ReviewState = %q, want %q", got.ReviewState, test.want)
			}
		})
	}
}

func TestHasContextualReviewWork(t *testing.T) {
	tests := []struct {
		name  string
		items []finding.Finding
		want  bool
	}{
		{name: "zero", want: false},
		{name: "static authoritative only", items: []finding.Finding{{ReviewPolicy: finding.ReviewPolicyStaticAuthoritative}}, want: false},
		{name: "contextual", items: []finding.Finding{{ReviewPolicy: finding.ReviewPolicyContextRequired}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasContextualReviewWork(test.items); got != test.want {
				t.Fatalf("hasContextualReviewWork() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRenderCompletionReportsTruthfulReviewState(t *testing.T) {
	tests := []struct {
		name     string
		state    reviewPresentationState
		summary  *ai.ReviewSummary
		want     string
		reviewed int
	}{
		{name: "disabled", state: reviewDisabled, summary: &ai.ReviewSummary{ContextualCandidates: 2, NotReviewed: 2}, want: "AI review disabled"},
		{name: "unnecessary", state: reviewUnnecessary, summary: &ai.ReviewSummary{}, want: "AI review unnecessary · no contextual findings"},
		{name: "complete", state: reviewComplete, summary: &ai.ReviewSummary{ContextualCandidates: 2, ProviderReviewed: 2}, want: "AI review complete · 2 findings reviewed", reviewed: 2},
		{name: "partial", state: reviewPartial, summary: &ai.ReviewSummary{ContextualCandidates: 3, ProviderReviewed: 2, NotReviewed: 1}, want: "AI review partial · 2 of 3 contextual findings reviewed", reviewed: 2},
		{name: "unavailable", state: reviewUnavailable, summary: &ai.ReviewSummary{ContextualCandidates: 2, NotReviewed: 2}, want: "AI review unavailable · 0 of 2 contextual findings reviewed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			summary := presentationSummary{Interactive: true, Capability: ui.Capability{IsTTY: true, Unicode: true, Width: 80}, ReviewState: test.state, ReviewSummary: test.summary, SignalCount: 3, CanonicalFindingCount: 1}
			if err := renderCompletion(&output, summary); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); !strings.Contains(got, test.want) || !strings.Contains(got, fmt.Sprintf("Scan complete · 3 signals · %d reviewed", test.reviewed)) {
				t.Fatalf("completion output = %q", got)
			}
		})
	}
}

func TestRenderCompletionRespectsHumanOutputGates(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary presentationSummary
	}{
		{name: "non tty", summary: presentationSummary{Capability: ui.Capability{IsTTY: false}, Interactive: false}},
		{name: "quiet", summary: presentationSummary{Capability: ui.Capability{IsTTY: true}, Interactive: true, Quiet: true}},
		{name: "json", summary: presentationSummary{Capability: ui.Capability{IsTTY: true}, Interactive: true, Format: "json"}},
		{name: "sarif", summary: presentationSummary{Capability: ui.Capability{IsTTY: true}, Interactive: true, Format: "sarif"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := renderCompletion(&output, test.summary); err != nil {
				t.Fatal(err)
			}
			if output.Len() != 0 {
				t.Fatalf("completion leaked: %q", output.String())
			}
		})
	}
}

func TestRenderCompletionWrapsToNarrowTerminal(t *testing.T) {
	var output bytes.Buffer
	summary := presentationSummary{
		Interactive: true, Capability: ui.Capability{IsTTY: true, Unicode: true, Width: 32},
		ReviewState: reviewPartial, ReviewSummary: &ai.ReviewSummary{ContextualCandidates: 14, ProviderReviewed: 2, NotReviewed: 12},
		SignalCount: 14, CanonicalFindingCount: 2, Format: "markdown", ReportPath: "/tmp/a-very-long-report-destination-name.md",
	}
	if err := renderCompletion(&output, summary); err != nil {
		t.Fatal(err)
	}
	for lineNumber, line := range strings.Split(output.String(), "\n") {
		if width := runewidth.StringWidth(line); width > 32 {
			t.Fatalf("line %d width = %d, want <= 32: %q", lineNumber+1, width, line)
		}
	}
}

func TestPresentationCountsPreserveCanonicalAndVisibleTruth(t *testing.T) {
	tests := []struct {
		name      string
		canonical int
		visible   int
		filters   bool
	}{
		{name: "canonical zero", canonical: 0, visible: 0, filters: false},
		{name: "filter zero", canonical: 4, visible: 0, filters: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := derivePresentationSummary(presentationInput{CanonicalFindingCount: test.canonical, VisibleFindingCount: test.visible, FiltersActive: test.filters})
			if got.CanonicalFindingCount != test.canonical || got.VisibleFindingCount != test.visible || got.FiltersActive != test.filters {
				t.Fatalf("summary counts = canonical %d visible %d filters %t", got.CanonicalFindingCount, got.VisibleFindingCount, got.FiltersActive)
			}
		})
	}
}
