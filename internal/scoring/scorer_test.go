package scoring

import (
	"math"
	"reflect"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestCalculateAppliesSeverityConfidenceAndCategoryWeights(t *testing.T) {
	findings := []finding.Finding{
		{RuleID: "SEC-A", Category: finding.CategorySecurity, Severity: finding.SeverityCritical, Confidence: 1},
		{RuleID: "SEC-B", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .5},
		{RuleID: "COR-A", Category: finding.CategoryCorrectness, Severity: finding.SeverityMedium, Confidence: .5},
		{RuleID: "MNT-A", Category: finding.CategoryMaintainability, Severity: finding.SeverityInfo, Confidence: 1},
	}

	got := Calculate(testFinalFindings(findings))

	wants := map[finding.Category]float64{
		finding.CategorySecurity:        81,
		finding.CategoryCorrectness:     98.5,
		finding.CategoryArchitecture:    100,
		finding.CategoryPerformance:     100,
		finding.CategoryMaintainability: 100,
	}
	for category, want := range wants {
		if math.Abs(got.ByCategory[category]-want) > .0001 {
			t.Errorf("ByCategory[%s] = %v, want %v", category, got.ByCategory[category], want)
		}
	}
	if want := 93.925; math.Abs(got.Overall-want) > .0001 {
		t.Errorf("Overall = %v, want %v", got.Overall, want)
	}
}

func TestCalculateDiminishesEquivalentFindingsWithinOneRule(t *testing.T) {
	scores := make([]float64, 0, 4)
	for _, count := range []int{1, 10, 100, 1000} {
		items := make([]finding.FinalFinding, count)
		for i := range items {
			items[i] = finding.FinalFinding{Finding: finding.Finding{RuleID: "GO-MNT-002", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1}}
		}
		scores = append(scores, Calculate(items).ByCategory[finding.CategoryMaintainability])
	}
	if !(scores[0] > scores[1] && scores[1] > scores[2] && scores[2] == scores[3]) {
		t.Fatalf("scores for 1/10/100/1000 equivalent findings = %v; want monotonic diminishing saturation", scores)
	}
	if scores[2] <= 0 {
		t.Fatalf("one repetitive maintainability rule destroyed category score: %v", scores)
	}
}

func TestCalculateAccumulatesIndependentRulesInSameCategory(t *testing.T) {
	oneRule := repeatedFindings("RULE-A", finding.CategoryCorrectness, finding.SeverityHigh, 100)
	twoRules := append(append([]finding.FinalFinding(nil), oneRule...), repeatedFindings("RULE-B", finding.CategoryCorrectness, finding.SeverityHigh, 100)...)
	if gotOne, gotTwo := Calculate(oneRule).ByCategory[finding.CategoryCorrectness], Calculate(twoRules).ByCategory[finding.CategoryCorrectness]; !(gotTwo < gotOne) {
		t.Fatalf("independent rule scores: one=%v two=%v", gotOne, gotTwo)
	}
}

func TestCalculateIndependentHighSeverityRulesCanStillReachZero(t *testing.T) {
	var items []finding.FinalFinding
	for i := 0; i < 4; i++ {
		items = append(items, repeatedFindings(string(rune('A'+i)), finding.CategorySecurity, finding.SeverityCritical, 100)...)
	}
	if got := Calculate(items).ByCategory[finding.CategorySecurity]; got != 0 {
		t.Fatalf("independent critical rules score = %v, want 0", got)
	}
}

func TestCalculateSeverityOrderingAndDeterminism(t *testing.T) {
	medium := repeatedFindings("R", finding.CategorySecurity, finding.SeverityMedium, 100)
	critical := repeatedFindings("R", finding.CategorySecurity, finding.SeverityCritical, 100)
	if gotMedium, gotCritical := Calculate(medium).ByCategory[finding.CategorySecurity], Calculate(critical).ByCategory[finding.CategorySecurity]; gotCritical > gotMedium {
		t.Fatalf("critical score %v penalizes less than medium score %v", gotCritical, gotMedium)
	}
	items := append(append([]finding.FinalFinding(nil), medium...), repeatedFindings("OTHER", finding.CategorySecurity, finding.SeverityHigh, 10)...)
	first := Calculate(items)
	for i := 0; i < 20; i++ {
		if got := Calculate(items); !reflect.DeepEqual(got, first) {
			t.Fatalf("non-deterministic result: first=%#v got=%#v", first, got)
		}
	}
}

func repeatedFindings(ruleID string, category finding.Category, severity finding.Severity, count int) []finding.FinalFinding {
	items := make([]finding.FinalFinding, count)
	for i := range items {
		items[i] = finding.FinalFinding{Finding: finding.Finding{RuleID: ruleID, Category: category, Severity: severity, Confidence: 1}}
	}
	return items
}

func TestCalculateClampsScoresAndConfidence(t *testing.T) {
	findings := make([]finding.Finding, 8)
	for i := range findings {
		findings[i] = finding.Finding{
			RuleID:     string(rune('A' + i)),
			Category:   finding.CategorySecurity,
			Severity:   finding.SeverityCritical,
			Confidence: 2,
		}
	}

	got := Calculate(testFinalFindings(findings))
	if got.ByCategory[finding.CategorySecurity] != 0 {
		t.Fatalf("Security score = %v, want 0", got.ByCategory[finding.CategorySecurity])
	}
	if want := 70.0; got.Overall != want {
		t.Fatalf("Overall = %v, want %v", got.Overall, want)
	}
}

func testFinalFindings(items []finding.Finding) []finding.FinalFinding {
	result := make([]finding.FinalFinding, len(items))
	for i := range items {
		result[i] = finding.FinalFinding{Finding: items[i]}
	}
	return result
}

func TestCalculateEmptyFindingsReturnsPerfectScores(t *testing.T) {
	got := Calculate(nil)
	if got.Overall != 100 {
		t.Fatalf("Overall = %v, want 100", got.Overall)
	}
	if len(got.ByCategory) != 5 {
		t.Fatalf("category count = %d, want 5", len(got.ByCategory))
	}
}
