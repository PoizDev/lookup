package ai

import (
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestNormalizeResponsesCanonicalizesSeverityAndFence(t *testing.T) {
	content := "```json\n[{\"id\":\"f-1\",\"evidence_relation\":\"SUPPORTS\",\"claim_strength\":\"UNCHANGED\",\"severity\":\"high\",\"confidence\":0.8,\"reason\":\"reachable\",\"recommendation\":\"validate\"}]\n```"

	got, err := NormalizeResponses(content, []string{"f-1"})
	if err != nil {
		t.Fatalf("NormalizeResponses: %v", err)
	}
	if len(got) != 1 || got[0].ID != "f-1" || got[0].Severity != finding.SeverityHigh {
		t.Fatalf("responses = %#v", got)
	}
}

func TestNormalizeResponsesAcceptsSingleObjectForOneFinding(t *testing.T) {
	content := `{"id":"f-1","evidence_relation":"CONTRADICTS","reason":"false positive","recommendation":"none","evidence_ids":["guard.1"]}`
	got, err := NormalizeResponses(content, []string{"f-1"})
	if err != nil || len(got) != 1 || got[0].IsRealIssue {
		t.Fatalf("responses = %#v, error = %v", got, err)
	}
}

func TestNormalizeResponsesAcceptsCanonicalAssessmentEnvelope(t *testing.T) {
	content := `{"assessments":[{"id":"f-1","evidence_relation":"insufficient","reason":"insufficient context","recommendation":"inspect manually"}]}`
	got, err := NormalizeResponses(content, []string{"f-1"})
	if err != nil || len(got) != 1 || got[0].Verdict != finding.AIVerdictUncertain {
		t.Fatalf("responses=%#v err=%v", got, err)
	}
}

func TestNormalizeResponsesDerivesStableVerdict(t *testing.T) {
	got, err := NormalizeResponses(`[{"id":"f1","evidence_relation":"CONTRADICTS","reason":"allowlisted","recommendation":"none","evidence_ids":["guard.1"]}]`, []string{"f1"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Verdict != finding.AIVerdictDisputed {
		t.Fatalf("verdict = %q", got[0].Verdict)
	}
}

func TestNormalizeResponsesMakesMissingOrBlankReasonMalformedUncertain(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{name: "missing"},
		{name: "blank", reason: `,"reason":"   "`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := `{"assessments":[{"id":"f-1","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","severity":"High","confidence":0.8` + test.reason + `,"evidence_ids":["source.primary"]}]}`
			got, err := NormalizeResponses(content, []string{"f-1"})
			if err != nil {
				t.Fatalf("NormalizeResponses: %v", err)
			}
			if len(got) != 1 || got[0].Verdict != finding.AdjudicationUncertain || !got[0].Malformed {
				t.Fatalf("responses = %#v, want malformed UNCERTAIN", got)
			}
			if got[0].Reason == "" || got[0].IsRealIssue {
				t.Fatalf("response = %#v, want audit reason without decisive verdict", got[0])
			}
		})
	}
}

func TestNormalizeResponsesRejectsIrrelevantOrMissingImpactFields(t *testing.T) {
	tests := []string{
		`{"assessments":[{"id":"f-1","evidence_relation":"CONTRADICTS","confidence":2,"reason":"counterevidence","evidence_ids":["guard.1"]}]}`,
		`{"assessments":[{"id":"f-1","evidence_relation":"SUPPORTS","claim_strength":"LOWER","severity":"Medium","reason":"weaker","evidence_ids":["source.primary"]}]}`,
	}
	for _, content := range tests {
		got, err := NormalizeResponses(content, []string{"f-1"})
		if err != nil || len(got) != 1 || !got[0].Malformed || got[0].Verdict != finding.AdjudicationUncertain {
			t.Fatalf("responses = %#v, err = %v, want malformed UNCERTAIN", got, err)
		}
	}
}

func TestNormalizeResponsesRejectsNumericSeverityWithoutCoercion(t *testing.T) {
	content := `{"assessments":[{"id":"f-1","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","severity":0.75,"confidence":0.95,"reason":"supported","recommendation":"fix","evidence_ids":["source.primary"]}]}`
	if _, err := NormalizeResponses(content, []string{"f-1"}); err == nil {
		t.Fatal("numeric severity was accepted or coerced")
	}
}

func TestNormalizeResponsesPreservesValidNeighborsOfMalformedFinding(t *testing.T) {
	content := `{"assessments":[
		{"id":"f-1","evidence_relation":"CONTRADICTS","reason":"not supported","evidence_ids":["guard.1"]},
		{"id":"f-2","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","severity":"High","confidence":0.8,"reason":""},
		{"id":"f-3","evidence_relation":"INSUFFICIENT","reason":"more context needed"}
	]}`
	got, err := NormalizeResponses(content, []string{"f-1", "f-2", "f-3"})
	if err != nil {
		t.Fatalf("NormalizeResponses: %v", err)
	}
	if got[0].Verdict != finding.AdjudicationUnsupported || got[0].Malformed || got[1].Verdict != finding.AdjudicationUncertain || !got[1].Malformed || got[2].Verdict != finding.AdjudicationUncertain || got[2].Malformed {
		t.Fatalf("responses = %#v", got)
	}
}

func TestNormalizeResponsesRejectsInvalidContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		ids     []string
	}{
		{"mismatched id", `[{"id":"other","is_real_issue":false,"severity":"Low","confidence":0.2}]`, []string{"f-1"}},
		{"duplicate id", `[{"id":"f-1","is_real_issue":false,"severity":"Low","confidence":0.2},{"id":"f-1","is_real_issue":false,"severity":"Low","confidence":0.2}]`, []string{"f-1", "f-2"}},
		{"prose wrapper", `Result: [{"id":"f-1","is_real_issue":false,"severity":"Low","confidence":0.2}]`, []string{"f-1"}},
		{"trailing json", `[{"id":"f-1","is_real_issue":false,"severity":"Low","confidence":0.2}] {}`, []string{"f-1"}},
		{"malformed json", `[`, []string{"f-1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeResponses(test.content, test.ids); err == nil {
				t.Fatalf("NormalizeResponses(%q) returned nil error", test.content)
			}
		})
	}
}
