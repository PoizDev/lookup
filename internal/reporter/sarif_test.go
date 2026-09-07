package reporter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestSARIFProducesSchemaLevelsAndRelativePaths(t *testing.T) {
	var output bytes.Buffer
	result := &Result{RepoPath: "/repo", ToolVersion: "0.1.0", Findings: []finding.Finding{
		{RuleID: "SEC-001", Title: "Secret", Reason: "Found secret", Severity: finding.SeverityCritical, Category: finding.CategorySecurity, Evidence: finding.Evidence{AffectedFiles: []string{"/repo/internal/auth.go:42"}, CallChain: []string{"NewClient", "main"}}},
		{RuleID: "MNT-001", Title: "Complex", Reason: "Too complex", Severity: finding.SeverityMedium},
		{RuleID: "INFO-001", Title: "Info", Reason: "Info", Severity: finding.SeverityInfo},
	}}
	if err := NewSARIF(&output, "/repo").Report(result); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(output.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["version"] != "2.1.0" {
		t.Fatalf("version = %v", doc["version"])
	}
	text := output.String()
	if !strings.Contains(text, `"semanticVersion": "0.1.0"`) {
		t.Fatalf("SARIF missing release version: %s", text)
	}
	for _, want := range []string{"sarif-schema-2.1.0", "internal/auth.go", "\"error\"", "\"warning\"", "\"note\"", "codeFlows"} {
		if !strings.Contains(text, want) {
			t.Errorf("SARIF missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "/repo/internal/auth.go") {
		t.Fatalf("SARIF leaked absolute repository path: %s", text)
	}
}

func TestSARIFOutputIsDeterministicAcrossFindingOrder(t *testing.T) {
	findings := []finding.Finding{
		{ID: "b", RuleID: "SEC-002", Title: "B", Severity: finding.SeverityHigh, Evidence: finding.Evidence{AffectedFiles: []string{"b.go:2"}}},
		{ID: "a", RuleID: "SEC-001", Title: "A", Severity: finding.SeverityLow, Evidence: finding.Evidence{AffectedFiles: []string{"a.go:1"}}},
	}
	encode := func(items []finding.Finding) string {
		var output bytes.Buffer
		if err := NewSARIF(&output, "/repo").Report(&Result{ToolVersion: "0.1.0", Findings: items}); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	if first, second := encode(findings), encode([]finding.Finding{findings[1], findings[0]}); first != second {
		t.Fatalf("SARIF output depends on input ordering:\n%s\n---\n%s", first, second)
	}
}

func TestSARIFCodeFlowUsesStructuredEvidenceLocations(t *testing.T) {
	result := &Result{RepoPath: "/repo", Findings: []finding.Finding{{
		ID: "flow", RuleID: "GIN-SEC-001", Title: "Tainted SQL", Reason: "HTTP input reaches SQL", Severity: finding.SeverityHigh, Category: finding.CategorySecurity,
		Evidence: finding.Evidence{
			AffectedFiles: []string{"/repo/handler.go:9"},
			Steps: []finding.EvidenceStep{
				{Kind: "source", File: "/repo/handler.go", Line: 3, Message: "HTTP query input", Expression: `c.Query("id")`},
				{Kind: "propagation", File: "/repo/handler.go", Line: 6, Message: "assigned to query", Expression: `query := prefix + id`},
				{Kind: "sink", File: "/repo/handler.go", Line: 9, Message: "dynamic SQL execution", Expression: `db.Raw(query)`},
			},
		},
	}}}
	var output bytes.Buffer
	if err := NewSARIF(&output, "/repo").Report(result); err != nil {
		t.Fatal(err)
	}
	var document sarifDocument
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	flow := document.Runs[0].Results[0].CodeFlows[0].ThreadFlows[0].Locations
	if len(flow) != 3 {
		t.Fatalf("flow locations = %#v", flow)
	}
	for index, wantLine := range []int{3, 6, 9} {
		location := flow[index].Location
		if location.PhysicalLocation == nil || location.PhysicalLocation.ArtifactLocation.URI != "handler.go" || location.PhysicalLocation.Region == nil || location.PhysicalLocation.Region.StartLine != wantLine {
			t.Fatalf("flow[%d] = %#v", index, location)
		}
	}
}

func TestSARIFUsesNormalizedPrimaryFindingLine(t *testing.T) {
	result := &Result{RepoPath: "/repo", Findings: []finding.Finding{{
		ID: "finding", RuleID: "GO-SEC-001", Title: "Issue", Severity: finding.SeverityHigh,
		Location: &finding.Location{File: "/repo/main.go", StartLine: 27, EndLine: 27},
		Evidence: finding.Evidence{AffectedFiles: []string{"/repo/main.go"}},
	}}}
	var output bytes.Buffer
	if err := NewSARIF(&output, "/repo").Report(result); err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(output.String(), `"uri": "main.go"`) || !strings.Contains(output.String(), `"startLine": 27`) {
		t.Fatalf("SARIF primary location missing analyzer line: %s", output.String())
	}
}
