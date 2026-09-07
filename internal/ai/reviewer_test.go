package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
)

type reviewerBatchProvider struct {
	calls    int
	requests [][]ReviewRequest
	fail     bool
}

func (*reviewerBatchProvider) Name() string { return "batch" }

func (provider *reviewerBatchProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := provider.ReviewBatch(ctx, []ReviewRequest{request})
	if err != nil {
		return ReviewResponse{}, err
	}
	return responses[0], nil
}

func (provider *reviewerBatchProvider) ReviewBatch(_ context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	provider.calls++
	provider.requests = append(provider.requests, append([]ReviewRequest(nil), requests...))
	if provider.fail {
		return nil, errors.New("rate limited")
	}
	responses := make([]ReviewResponse, len(requests))
	for index, request := range requests {
		if index == 0 {
			responses[index] = ReviewResponse{ID: request.Finding.ID, EvidenceRelation: EvidenceInsufficient, Reason: "insufficient", Recommendation: "inspect"}
			continue
		}
		responses[index] = ReviewResponse{
			ID:               request.Finding.ID,
			EvidenceRelation: EvidenceSupports,
			ClaimStrength:    ClaimStrengthLower,
			Severity:         finding.SeverityCritical,
			Confidence:       0.9,
			Reason:           "reachable",
			Recommendation:   "fix it",
		}
	}
	return responses, nil
}

type reviewerSingleProvider struct{ calls int }

func (*reviewerSingleProvider) Name() string { return "single" }
func (provider *reviewerSingleProvider) Review(_ context.Context, request ReviewRequest) (ReviewResponse, error) {
	provider.calls++
	return ReviewResponse{ID: request.Finding.ID, EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityLow, Confidence: 0.7, Reason: "confirmed", Recommendation: "change it"}, nil
}

func TestNormalizePotentialCreatesStableBoundedFinding(t *testing.T) {
	snippet := strings.Repeat("line\n", 20) + "NOT_STORED"
	potential := language.PotentialFinding{
		RuleID: "SEC-001", Category: "Security", Severity: "High", Title: "Secret",
		Description: "literal secret", File: "main.go", StartLine: 4, EndLine: 8,
		CodeSnippet: snippet, AffectedSymbols: []string{"connect"}, CallChain: []string{"main", "connect"},
	}

	first := NormalizePotential(potential)
	second := NormalizePotential(potential)
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("IDs are not stable: %q and %q", first.ID, second.ID)
	}
	if first.Confidence != 1 || first.AIReviewed || first.Reason != "literal secret" {
		t.Fatalf("deterministic finding = %#v", first)
	}
	if strings.Contains(first.Evidence.CodeSnippet, "NOT_STORED") {
		t.Fatal("stored evidence exceeds 20 lines")
	}
	if len(first.Evidence.AffectedFiles) != 1 || first.Evidence.AffectedFiles[0] != "main.go" {
		t.Fatalf("affected files = %#v", first.Evidence.AffectedFiles)
	}
}

func TestNormalizePotentialPreservesStaticAssessmentAndStructuredEvidence(t *testing.T) {
	potential := language.PotentialFinding{
		RuleID:           "GO-SQL-SEC-001",
		Category:         "Security",
		Severity:         "High",
		Title:            "Tainted SQL",
		Description:      "HTTP input reaches SQL execution",
		File:             "handler.go",
		StartLine:        9,
		Confidence:       0.90,
		EvidenceStrength: "strong",
		EvidenceSteps: []language.EvidenceStep{
			{Kind: "source", File: "handler.go", Line: 3, Expression: `c.Query("id")`, Message: "HTTP query input"},
			{Kind: "propagation", File: "handler.go", Line: 6, Expression: `query := prefix + id`, Message: "assigned to query"},
			{Kind: "sink", File: "handler.go", Line: 9, Expression: `db.Raw(query)`, Message: "dynamic SQL execution"},
		},
	}

	got := NormalizePotential(potential)
	if got.Confidence != 0.90 || got.EvidenceStrength != "strong" {
		t.Fatalf("assessment = confidence %.2f strength %q", got.Confidence, got.EvidenceStrength)
	}
	if len(got.Evidence.Steps) != 3 || got.Evidence.Steps[0].Kind != "source" || got.Evidence.Steps[2].Line != 9 {
		t.Fatalf("evidence steps = %#v", got.Evidence.Steps)
	}
}

func TestNormalizePotentialUsesEvidenceToDisambiguateSameSinkFlows(t *testing.T) {
	base := language.PotentialFinding{RuleID: "GIN-SEC-001", Category: "Security", Severity: "High", Title: "SQL flow", File: "repo.go", StartLine: 20, EndLine: 20}
	first := base
	first.EvidenceSteps = []language.EvidenceStep{{Kind: "source", File: "handler.go", Line: 5, Expression: `c.Query("id")`}, {Kind: "sink", File: "repo.go", Line: 20, Expression: "db.Raw(query)"}}
	second := base
	second.EvidenceSteps = []language.EvidenceStep{{Kind: "source", File: "handler.go", Line: 6, Expression: `c.Query("name")`}, {Kind: "sink", File: "repo.go", Line: 20, Expression: "db.Raw(query)"}}

	left, right := NormalizePotential(first), NormalizePotential(second)
	if left.ID == right.ID {
		t.Fatalf("flow IDs collide: %q", left.ID)
	}
	if left.Location == nil || left.Location.StartLine != 20 {
		t.Fatalf("primary location = %#v", left.Location)
	}
}

func TestReviewerUsesNativeBatchesAndAttachesNonAuthoritativeAssessment(t *testing.T) {
	provider := &reviewerBatchProvider{}
	potentials := []language.PotentialFinding{
		{RuleID: "SEC-001", Category: "Security", Severity: "High", Title: "first", CodeSnippet: strings.Repeat("line\n", 100) + "NOT_SENT"},
		{RuleID: "COR-001", Category: "Correctness", Severity: "Medium", Title: "second"},
	}

	got := NewReviewer(provider, 2, nil).Review(context.Background(), potentials)
	if provider.calls != 1 || len(provider.requests[0]) != 2 {
		t.Fatalf("batch calls = %d, requests = %#v", provider.calls, provider.requests)
	}
	if strings.Contains(provider.requests[0][0].CodeSnippet, "NOT_SENT") {
		t.Fatal("review request exceeds 100 snippet lines")
	}
	if len(got) != 2 || !got[0].AIReviewed || !got[1].AIReviewed {
		t.Fatalf("reviewed findings = %#v", got)
	}
	if got[0].AIReview == nil || got[0].AIReview.IsRealIssue || got[1].AIReview == nil || !got[1].AIReview.IsRealIssue {
		t.Fatalf("AI assessments = %#v", got)
	}
	if got[1].Severity != finding.SeverityMedium || got[1].Confidence != 1 {
		t.Fatalf("AI changed deterministic assessment: %#v", got[1])
	}
}

func TestReviewerFallsBackToSingleReview(t *testing.T) {
	provider := &reviewerSingleProvider{}
	got := NewReviewer(provider, 5, nil).Review(context.Background(), []language.PotentialFinding{
		{RuleID: "MNT-001", Category: "Maintainability", Severity: "Medium", Title: "one"},
		{RuleID: "MNT-002", Category: "Maintainability", Severity: "Medium", Title: "two"},
	})
	if provider.calls != 2 || len(got) != 2 || !got[0].AIReviewed || !got[1].AIReviewed {
		t.Fatalf("calls = %d, findings = %#v", provider.calls, got)
	}
}

func TestReviewerPreservesDeterministicFindingsAndWarnsOnFailure(t *testing.T) {
	provider := &reviewerBatchProvider{fail: true}
	var warnings []error
	got := NewReviewer(provider, 2, func(err error) { warnings = append(warnings, err) }).Review(
		context.Background(),
		[]language.PotentialFinding{{RuleID: "SEC-001", Category: "Security", Severity: "High", Title: "secret"}},
	)
	if len(got) != 1 || got[0].AIReviewed || got[0].Confidence != 1 {
		t.Fatalf("fallback findings = %#v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "rate limited") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestReviewerReportsProgressAfterEachBatch(t *testing.T) {
	provider := &reviewerBatchProvider{}
	potentials := []language.PotentialFinding{
		{RuleID: "SEC-001", Category: "Security", Severity: "High", Title: "one"},
		{RuleID: "SEC-002", Category: "Security", Severity: "High", Title: "two"},
		{RuleID: "SEC-003", Category: "Security", Severity: "High", Title: "three"},
	}
	var progress [][2]int

	NewReviewer(provider, 2, nil).ReviewWithProgress(context.Background(), potentials, func(done, total int) {
		progress = append(progress, [2]int{done, total})
	})

	want := [][2]int{{0, 3}, {2, 3}, {3, 3}}
	if len(progress) != len(want) {
		t.Fatalf("progress = %#v, want %#v", progress, want)
	}
	for index := range want {
		if progress[index] != want[index] {
			t.Fatalf("progress = %#v, want %#v", progress, want)
		}
	}
}

func TestAIReviewDoesNotChangeDeterministicFindingSetOrAssessment(t *testing.T) {
	provider := &reviewerBatchProvider{}
	potentials := []language.PotentialFinding{
		{RuleID: "GIN-SEC-001", Category: "Security", Severity: "High", Confidence: 0.90, Title: "SQL"},
		{RuleID: "GO-MNT-001", Category: "Maintainability", Severity: "Medium", Confidence: 1, Title: "Long"},
	}
	withoutAI := NewReviewer(nil, 2, nil).Review(context.Background(), potentials)
	withAI := NewReviewer(provider, 2, nil).Review(context.Background(), potentials)
	if len(withAI) != len(withoutAI) {
		t.Fatalf("finding count changed: AI=%d static=%d", len(withAI), len(withoutAI))
	}
	for index := range withoutAI {
		if withAI[index].ID != withoutAI[index].ID || withAI[index].Severity != withoutAI[index].Severity || withAI[index].Confidence != withoutAI[index].Confidence {
			t.Fatalf("finding %d changed: AI=%#v static=%#v", index, withAI[index], withoutAI[index])
		}
	}
}
