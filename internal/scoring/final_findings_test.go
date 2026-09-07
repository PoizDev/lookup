package scoring

import (
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestCalculateAcceptsOnlyFinalFindings(t *testing.T) {
	finals := []finding.FinalFinding{{Finding: finding.Finding{RuleID: "CONFIRMED", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: 1}}}
	got := Calculate(finals)
	if got.ByCategory[finding.CategorySecurity] >= 100 {
		t.Fatalf("security score = %v, want confirmed finding penalty", got.ByCategory[finding.CategorySecurity])
	}
}
