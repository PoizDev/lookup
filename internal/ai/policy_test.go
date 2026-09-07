package ai

import (
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestSmartPolicySkipsStrongDirectDeterministicFinding(t *testing.T) {
	f := finding.Finding{RuleID: "GO-SEC-001", Category: finding.CategorySecurity, Confidence: .95, EvidenceStrength: "strong", Evidence: finding.Evidence{Steps: []finding.EvidenceStep{{Kind: "source"}, {Kind: "propagation"}, {Kind: "sink"}}}}
	if Eligible(ReviewModeSmart, f) {
		t.Fatal("strong direct dataflow finding was selected")
	}
}

func TestSmartPolicySelectsModerateAndContextualFindings(t *testing.T) {
	items := []finding.Finding{
		{RuleID: "RUST-COR-004", Confidence: .75, EvidenceStrength: "moderate", Title: "Ownership may escape"},
		{RuleID: "GO-MNT-001", Confidence: 1, EvidenceStrength: "strong", Category: finding.CategoryMaintainability, Title: "Function is excessively long"},
	}
	for _, item := range items {
		if !Eligible(ReviewModeSmart, item) {
			t.Fatalf("not selected: %#v", item)
		}
	}
}

func TestAllAndOffPolicies(t *testing.T) {
	f := finding.Finding{Confidence: 1, EvidenceStrength: "strong"}
	if !Eligible(ReviewModeAll, f) {
		t.Fatal("all did not select finding")
	}
	if Eligible(ReviewModeOff, f) {
		t.Fatal("off selected finding")
	}
}
