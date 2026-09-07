package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/ui"
)

type reviewPresentationState string

const (
	reviewDisabled    reviewPresentationState = "disabled"
	reviewUnnecessary reviewPresentationState = "unnecessary"
	reviewComplete    reviewPresentationState = "complete"
	reviewPartial     reviewPresentationState = "partial"
	reviewUnavailable reviewPresentationState = "unavailable"
)

type presentationInput struct {
	AI                    config.AIConfig
	CanonicalFindingCount int
	VisibleFindingCount   int
	FiltersActive         bool
	ReviewSummary         *ai.ReviewSummary
	AIReview              *ai.ReviewStats
	SignalCount           int
	Format                string
	ReportPath            string
	Quiet                 bool
	Capability            ui.Capability
}

type presentationSummary struct {
	CanonicalFindingCount int
	VisibleFindingCount   int
	FiltersActive         bool
	ReviewState           reviewPresentationState
	ReviewSummary         *ai.ReviewSummary
	AIReview              *ai.ReviewStats
	SignalCount           int
	Format                string
	ReportPath            string
	Quiet                 bool
	Interactive           bool
	Capability            ui.Capability
}

func derivePresentationSummary(input presentationInput) presentationSummary {
	result := presentationSummary{
		CanonicalFindingCount: input.CanonicalFindingCount,
		VisibleFindingCount:   input.VisibleFindingCount,
		FiltersActive:         input.FiltersActive,
		ReviewSummary:         input.ReviewSummary,
		AIReview:              input.AIReview,
		SignalCount:           input.SignalCount,
		Format:                strings.ToLower(strings.TrimSpace(input.Format)),
		ReportPath:            input.ReportPath,
		Quiet:                 input.Quiet,
		Interactive:           input.Capability.IsTTY,
		Capability:            input.Capability,
	}
	if input.AI.NoAI || strings.EqualFold(strings.TrimSpace(input.AI.ReviewMode), "off") {
		result.ReviewState = reviewDisabled
		return result
	}
	if input.ReviewSummary == nil || input.ReviewSummary.ContextualCandidates == 0 {
		result.ReviewState = reviewUnnecessary
		return result
	}

	completed := input.ReviewSummary.ProviderReviewed + input.ReviewSummary.CacheHits
	failed := input.ReviewSummary.ProviderFailures > 0 || input.ReviewSummary.ContextInsufficient > 0
	if input.AIReview != nil {
		failed = failed || input.AIReview.Failures > 0 || input.AIReview.StopReason != ""
	}
	if input.ReviewSummary.NotReviewed == 0 && !failed {
		result.ReviewState = reviewComplete
	} else if completed > 0 {
		result.ReviewState = reviewPartial
	} else if failed {
		result.ReviewState = reviewUnavailable
	} else {
		result.ReviewState = reviewPartial
	}
	return result
}

func renderCompletion(writer io.Writer, summary presentationSummary) error {
	if !summary.Interactive || !summary.Capability.IsTTY || summary.Quiet || summary.Format == "json" || summary.Format == "sarif" {
		return nil
	}
	icons := ui.Icons(summary.Capability.Unicode)
	reviewed, contextual := 0, 0
	if summary.ReviewSummary != nil {
		reviewed = summary.ReviewSummary.ProviderReviewed + summary.ReviewSummary.CacheHits
		contextual = summary.ReviewSummary.ContextualCandidates
	}
	var reviewLine string
	switch summary.ReviewState {
	case reviewDisabled:
		reviewLine = fmt.Sprintf("%s AI review disabled", icons.Arrow)
	case reviewUnnecessary:
		reviewLine = fmt.Sprintf("%s AI review unnecessary · no contextual findings", icons.Success)
	case reviewComplete:
		reviewLine = fmt.Sprintf("%s AI review complete · %d %s reviewed", icons.Success, reviewed, plural(reviewed, "finding", "findings"))
	case reviewPartial:
		reviewLine = fmt.Sprintf("%s AI review partial · %d of %d contextual findings reviewed", icons.Warning, reviewed, contextual)
	case reviewUnavailable:
		reviewLine = fmt.Sprintf("%s AI review unavailable · %d of %d contextual findings reviewed", icons.Warning, reviewed, contextual)
	}
	if reviewLine != "" {
		if err := writeCompletionLine(writer, reviewLine, summary.Capability.Width); err != nil {
			return err
		}
	}
	if summary.Format == "markdown" && strings.TrimSpace(summary.ReportPath) != "" {
		if err := writeCompletionLine(writer, fmt.Sprintf("%s Report written to %s", icons.Success, summary.ReportPath), summary.Capability.Width); err != nil {
			return err
		}
	}
	return writeCompletionLine(writer, fmt.Sprintf("%s Scan complete · %d %s · %d reviewed", icons.Success, summary.SignalCount, plural(summary.SignalCount, "signal", "signals"), reviewed), summary.Capability.Width)
}

func writeCompletionLine(writer io.Writer, value string, width int) error {
	if width <= 0 || runewidth.StringWidth(value) <= width {
		_, err := fmt.Fprintln(writer, value)
		return err
	}
	line := ""
	for _, word := range strings.Fields(value) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if runewidth.StringWidth(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			if _, err := fmt.Fprintln(writer, line); err != nil {
				return err
			}
		}
		line = ""
		parts := splitDisplayWord(word, width)
		for _, part := range parts[:len(parts)-1] {
			if _, err := fmt.Fprintln(writer, part); err != nil {
				return err
			}
		}
		line = parts[len(parts)-1]
	}
	if line != "" {
		_, err := fmt.Fprintln(writer, line)
		return err
	}
	return nil
}

func splitDisplayWord(word string, width int) []string {
	if width <= 0 || runewidth.StringWidth(word) <= width {
		return []string{word}
	}
	parts := make([]string, 0, 2)
	var current strings.Builder
	currentWidth := 0
	for _, char := range word {
		charWidth := runewidth.RuneWidth(char)
		if currentWidth > 0 && currentWidth+charWidth > width {
			parts = append(parts, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(char)
		currentWidth += charWidth
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func plural(count int, singular, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}

func hasContextualReviewWork(items []finding.Finding) bool {
	for _, item := range items {
		if item.ReviewPolicy == finding.ReviewPolicyContextRequired || item.ReviewPolicy == "" {
			return true
		}
	}
	return false
}
