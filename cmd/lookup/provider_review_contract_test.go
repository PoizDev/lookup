package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/ai/providers"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/reviewcontext"
	"github.com/poizdev/lookup/internal/scoring"
)

type ProviderContractFailureTransport struct{ calls int }

func (r *ProviderContractFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, fmt.Errorf("provider contract injected transport failure")
}

// TestProviderReviewSmoke is explicitly opt-in, uses the configured provider only
// after the source audit, and never asserts agreement with a semantic verdict.
func TestProviderReviewSmoke(t *testing.T) {
	base := os.Getenv("LOOKUP_PROVIDER_REVIEW_ARTIFACTS")
	if base == "" {
		t.Skip("set LOOKUP_PROVIDER_REVIEW_ARTIFACTS to the audited artifact directory")
	}
	data, err := os.ReadFile(filepath.Join(base, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Case     string `json:"case"`
		Language string `json:"language"`
		Audit    struct {
			Packet   reviewcontext.ReviewPacket `json:"packet"`
			Required []string                   `json:"required_ids"`
		} `json:"packet_audit"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AI.NoAI = false
	// provider-review overrides: do not write the user's persistent Lookup config.
	if endpoint := os.Getenv("LOOKUP_PROVIDER_REVIEW_OLLAMA_URL"); endpoint != "" {
		cfg.AI.Provider = "ollama"
		cfg.AI.Ollama.BaseURL = endpoint
	}
	if model := os.Getenv("LOOKUP_PROVIDER_REVIEW_OLLAMA_MODEL"); model != "" {
		cfg.AI.Ollama.Model = model
	}
	for _, c := range cases {
		if c.Case != "PY06" && c.Case != "RS04" && c.Case != "CS03" {
			continue
		}
		t.Run(c.Case, func(t *testing.T) {
			packet := c.Audit.Packet
			for i := range packet.Evidence {
				for _, id := range c.Audit.Required {
					if packet.Evidence[i].ID == id {
						packet.Evidence[i].Required = true
						// Required semantic evidence has critical retention in the
						// builder; both local-only fields are omitted from JSON.
						packet.Evidence[i].Retention = reviewcontext.RetentionCritical
					}
				}
			}
			item := packet.Candidate.Finding
			for _, mode := range []string{"live", "failure", "insufficient"} {
				t.Run(mode, func(t *testing.T) {
					current := packet
					transport := &ProviderContractFailureTransport{}
					client := &http.Client{Timeout: 90 * time.Second}
					if mode != "live" {
						client.Transport = transport
					}
					if mode == "insufficient" {
						current.Evidence = nil
						current = reviewcontext.ReevaluateSufficiency(packet, current)
					}
					provider, err := providers.New(cfg.AI, client)
					if err != nil {
						t.Fatal(err)
					}
					var warnings []string
					options := ai.TriageOptions{Mode: ai.ReviewModeAll, TokenBudget: 8000, OutputTokenBudget: 2048, Concurrency: 1, Model: configuredModel(cfg.AI, provider.Name()), Budget: ai.ReviewBudget{MaxReviewedFindings: 1, MaxProviderRequests: 1, MaxEstimatedInputTokens: 20000, MaxOutputTokens: 2048}, BuildContext: func(finding.Finding) reviewcontext.ReviewPacket { return current }}
					ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
					defer cancel()
					items, stats := ai.NewTriageReviewer(provider, options, func(err error) { warnings = append(warnings, err.Error()) }).ReviewFindings(ctx, []finding.Finding{item}, nil)
					result := finalizeFindings(items)
					if len(result.Candidates) != 1 {
						t.Fatalf("invented/lost candidate: %d", len(result.Candidates))
					}
					if mode != "live" && len(result.FinalFindings) != 0 {
						t.Fatal("failure or missing evidence became authoritative")
					}
					if mode == "insufficient" && transport.calls != 0 {
						t.Fatal("insufficient packet reached provider")
					}
					static, verified := scoring.CalculateScoped(result)
					finals := make([]finding.Finding, 0, len(result.FinalFindings))
					for _, f := range result.FinalFindings {
						finals = append(finals, f.Finding)
					}
					report := reporter.Result{RepoPath: "provider-reviewed subset: " + c.Language, Findings: finals, Candidates: result.Candidates, StaticRisk: static, VerifiedHealth: verified, AIReview: &stats, ReviewPlan: stats.Plan}
					name := filepath.Join(base, "smoke-"+c.Case+"-"+mode+".json")
					file, err := os.Create(name)
					if err != nil {
						t.Fatal(err)
					}
					if err := reporter.NewJSON(file).Report(&report); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
					t.Logf("provider=%s mode=%s calls=%d reviewed=%d failures=%d decision=%s warnings=%v", provider.Name(), mode, transport.calls, stats.ProviderReviews, stats.Failures, result.Candidates[0].Decision.Status, warnings)
				})
			}
		})
	}
}

// The fixture transport exercises the actual Ollama adapter and JSON reporting,
// without using a model as an oracle or making additional network calls.
type ProviderContractFixtureTransport struct {
	response map[string]any
	payload  []byte
}

func (r *ProviderContractFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error
	r.payload, err = io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"assessments": []any{r.response}})
	if err != nil {
		return nil, err
	}
	envelope, err := json.Marshal(map[string]any{"message": map[string]any{"content": string(body)}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(envelope))}, nil
}
func TestProviderStructuredTransportContract(t *testing.T) {
	base := os.Getenv("LOOKUP_PROVIDER_REVIEW_ARTIFACTS")
	if base == "" {
		t.Skip("set LOOKUP_PROVIDER_REVIEW_ARTIFACTS")
	}
	data, err := os.ReadFile(filepath.Join(base, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Case  string `json:"case"`
		Audit struct {
			Packet   reviewcontext.ReviewPacket `json:"packet"`
			Required []string                   `json:"required_ids"`
		} `json:"packet_audit"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	for _, c := range cases {
		if c.Case != "PY06" && c.Case != "RS04" && c.Case != "CS03" {
			continue
		}
		p := c.Audit.Packet
		for i := range p.Evidence {
			for _, id := range c.Audit.Required {
				if p.Evidence[i].ID == id {
					p.Evidence[i].Required = true
					p.Evidence[i].Retention = reviewcontext.RetentionCritical
				}
			}
		}
		for _, mode := range []string{"confirmed", "downgraded", "unsupported", "uncertain", "invented_evidence"} {
			t.Run(c.Case+"/"+mode, func(t *testing.T) {
				response := map[string]any{"id": p.Candidate.ID, "reason": "provider contract transport fixture, not a semantic judgment.", "recommendation": "", "evidence_ids": []string{"source.primary"}}
				want := finding.AdjudicationConfirmed
				switch mode {
				case "confirmed", "invented_evidence":
					response["evidence_relation"] = "SUPPORTS"
					response["claim_strength"] = "UNCHANGED"
					response["severity"] = "Critical"
					response["confidence"] = 1.0
				case "downgraded":
					response["evidence_relation"] = "SUPPORTS"
					response["claim_strength"] = "LOWER"
					response["severity"] = "Low"
					response["confidence"] = 0.1
					want = finding.AdjudicationDowngraded
				case "unsupported":
					response["evidence_relation"] = "CONTRADICTS"
					want = finding.AdjudicationUnsupported
				case "uncertain":
					response["evidence_relation"] = "INSUFFICIENT"
					want = finding.AdjudicationUncertain
				}
				if mode == "invented_evidence" {
					response["evidence_ids"] = []string{"invented.id"}
					want = finding.AdjudicationUncertain
				}
				transport := &ProviderContractFixtureTransport{response: response}
				provider := providers.NewOllama(&http.Client{Transport: transport}, "provider-contract-fixture", "http://fixture.invalid")
				options := ai.TriageOptions{Mode: ai.ReviewModeAll, TokenBudget: 8000, OutputTokenBudget: 2048, Concurrency: 1, Model: "provider-contract-fixture", Budget: ai.ReviewBudget{MaxReviewedFindings: 1, MaxProviderRequests: 1, MaxEstimatedInputTokens: 20000, MaxOutputTokens: 2048}, BuildContext: func(finding.Finding) reviewcontext.ReviewPacket { return p }}
				items, stats := ai.NewTriageReviewer(provider, options, nil).ReviewFindings(context.Background(), []finding.Finding{p.Candidate.Finding}, nil)
				result := finalizeFindings(items)
				if len(result.Candidates) != 1 || result.Candidates[0].Decision.Status != want {
					t.Fatalf("unexpected result: %#v", result)
				}
				decision := result.Candidates[0].Decision
				if mode == "confirmed" && (decision.Severity != p.Candidate.Severity || decision.Confidence > p.Candidate.Confidence) {
					t.Fatal("static bounds escalated")
				}
				if mode == "invented_evidence" && len(result.FinalFindings) != 0 {
					t.Fatal("invented evidence accepted")
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(transport.payload, &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload["format"]) == 0 || !bytes.Contains(payload["messages"], []byte(p.Candidate.ID)) || !bytes.Contains(payload["messages"], []byte("source.primary")) {
					t.Fatal("request omitted schema, candidate or evidence identity")
				}
				var rendered bytes.Buffer
				report := reporter.Result{Candidates: result.Candidates, AIReview: &stats}
				for _, f := range result.FinalFindings {
					report.Findings = append(report.Findings, f.Finding)
				}
				if err := reporter.NewJSON(&rendered).Report(&report); err != nil {
					t.Fatal(err)
				}
				var decoded reporter.Result
				if err := json.Unmarshal(rendered.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Candidates[0].Decision.Status != want || len(decoded.Findings) != len(result.FinalFindings) {
					t.Fatal("report lost review state")
				}
				results = append(results, map[string]any{"case": c.Case, "fixture": mode, "decision": decision, "final_findings": len(result.FinalFindings), "schema_in_request": true, "report_roundtrip": true})
			})
		}
	}
	data, err = json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "structured-transport.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
