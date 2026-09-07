package ai

import (
	"strings"

	"github.com/poizdev/lookup/internal/finding"
)

type ReviewMode string

const (
	ReviewModeSmart ReviewMode = "smart"
	ReviewModeAll   ReviewMode = "all"
	ReviewModeOff   ReviewMode = "off"
)

// Eligible decides whether a context-required candidate should be adjudicated.
func Eligible(mode ReviewMode, item finding.Finding) bool {
	if item.ReviewPolicy == finding.ReviewPolicyStaticAuthoritative {
		return false
	}
	if mode == ReviewModeOff {
		return false
	}
	if mode == ReviewModeAll {
		return true
	}
	strength := strings.ToLower(strings.TrimSpace(item.EvidenceStrength))
	if strength == "moderate" || strength == "weak" || item.Confidence < .85 {
		return true
	}
	if item.Category == finding.CategoryMaintainability || item.Category == finding.CategoryArchitecture {
		return true
	}
	title := strings.ToLower(item.Title + " " + item.Reason)
	for _, marker := range []string{"lifecycle", "ownership", "may ", "possibly", "complex", "long function", "excessively long", "layer"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	directFlow := false
	for _, step := range item.Evidence.Steps {
		switch strings.ToLower(step.Kind) {
		case "source", "propagation", "sink":
			directFlow = true
		}
	}
	return !(strength == "strong" && item.Confidence >= .9 && directFlow)
}
