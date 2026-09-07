package ai

import (
	"context"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Review(_ context.Context, request ReviewRequest) (ReviewResponse, error) {
	return ReviewResponse{
		EvidenceRelation: EvidenceSupports,
		ClaimStrength:    ClaimStrengthUnchanged,
		Severity:         request.Finding.Severity,
		Confidence:       0.87,
		Reason:           request.CodeSnippet,
		Recommendation:   request.CallChain[0],
	}, nil
}

func TestProviderContract(t *testing.T) {
	var provider Provider = fakeProvider{}
	request := ReviewRequest{
		Finding: finding.Finding{
			ID:       "finding-1",
			Severity: finding.SeverityHigh,
		},
		CodeSnippet: "dangerous()",
		CallChain:   []string{"main", "dangerous"},
	}

	response, err := provider.Review(context.Background(), request)
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if provider.Name() != "fake" {
		t.Fatalf("Name = %q, want fake", provider.Name())
	}
	if response.EvidenceRelation != EvidenceSupports || response.ClaimStrength != ClaimStrengthUnchanged || response.Severity != finding.SeverityHigh {
		t.Fatalf("response = %#v, want SUPPORTS/UNCHANGED High judgment", response)
	}
	if response.Confidence != 0.87 || response.Reason != "dangerous()" || response.Recommendation != "main" {
		t.Fatalf("response = %#v, want normalized review fields", response)
	}
}
