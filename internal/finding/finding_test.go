package finding

import (
	"encoding/json"
	"testing"
)

func TestFindingJSONContract(t *testing.T) {
	want := Finding{
		ID:             "finding-1",
		RuleID:         "SEC-001",
		Category:       CategorySecurity,
		Severity:       SeverityHigh,
		Confidence:     0.91,
		Title:          "Hardcoded credential",
		Reason:         "A credential is embedded in source code.",
		Recommendation: "Load it from a secret store.",
		Evidence: Evidence{
			AffectedFiles:   []string{"main.go"},
			AffectedSymbols: []string{"main"},
			CodeSnippet:     `token := "secret"`,
			CallChain:       []string{"main", "connect"},
		},
		AIReviewed: true,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal Finding: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal Finding JSON: %v", err)
	}

	if got["category"] != "Security" {
		t.Fatalf("category = %v, want Security", got["category"])
	}
	if got["severity"] != "High" {
		t.Fatalf("severity = %v, want High", got["severity"])
	}
	if got["confidence"] != 0.91 {
		t.Fatalf("confidence = %v, want 0.91", got["confidence"])
	}
	if got["ai_reviewed"] != true {
		t.Fatalf("ai_reviewed = %v, want true", got["ai_reviewed"])
	}

	evidence, ok := got["evidence"].(map[string]any)
	if !ok {
		t.Fatalf("evidence has type %T, want JSON object", got["evidence"])
	}
	files, ok := evidence["affected_files"].([]any)
	if !ok || len(files) != 1 || files[0] != "main.go" {
		t.Fatalf("affected_files = %#v, want [main.go]", evidence["affected_files"])
	}
	chain, ok := evidence["call_chain"].([]any)
	if !ok || len(chain) != 2 || chain[0] != "main" || chain[1] != "connect" {
		t.Fatalf("call_chain = %#v, want [main connect]", evidence["call_chain"])
	}
}

func TestEnumValues(t *testing.T) {
	severities := []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo}
	wantSeverities := []string{"Critical", "High", "Medium", "Low", "Info"}
	for i, severity := range severities {
		if string(severity) != wantSeverities[i] {
			t.Fatalf("severity[%d] = %q, want %q", i, severity, wantSeverities[i])
		}
	}

	categories := []Category{CategorySecurity, CategoryPerformance, CategoryArchitecture, CategoryCorrectness, CategoryMaintainability}
	wantCategories := []string{"Security", "Performance", "Architecture", "Correctness", "Maintainability"}
	for i, category := range categories {
		if string(category) != wantCategories[i] {
			t.Fatalf("category[%d] = %q, want %q", i, category, wantCategories[i])
		}
	}
}
