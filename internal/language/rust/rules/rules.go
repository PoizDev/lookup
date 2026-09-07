package rules

import (
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/semantic"
)

type unsafeRule struct{}
type factRule struct {
	id, category, severity, operation, title, description string
	kind                                                  semantic.FactKind
}

func (r *factRule) ID() string           { return r.id }
func (r *factRule) CategoryName() string { return r.category }
func (r *factRule) SeverityName() string { return r.severity }
func (r *factRule) Check(g *graph.Graph) []language.PotentialFinding {
	var out []language.PotentialFinding
	for _, f := range g.SemanticIndex().Facts(r.kind) {
		if f.Operation != r.operation {
			continue
		}
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: r.category, Severity: r.severity, Title: r.title, Description: r.description, Observation: "The semantic extractor emitted operation " + r.operation + " at the supplied source location.", Hypothesis: r.description, File: f.Location.File, StartLine: f.Location.StartLine, EndLine: f.Location.EndLine, CodeSnippet: f.Expression, Confidence: .9, EvidenceStrength: "strong", EvidenceSteps: []language.EvidenceStep{{Kind: "evidence", File: f.Location.File, Line: f.Location.StartLine, Column: f.Location.StartColumn, Expression: f.Expression, Message: r.description}}})
	}
	return out
}

type traceRule struct{ id, sink, title, description string }

func (r *traceRule) ID() string         { return r.id }
func (*traceRule) CategoryName() string { return "Security" }
func (*traceRule) SeverityName() string { return "High" }
func (r *traceRule) Check(g *graph.Graph) []language.PotentialFinding {
	var out []language.PotentialFinding
	for _, trace := range g.DataflowTraces() {
		if trace.Sink.Operation != r.sink {
			continue
		}
		steps := []language.EvidenceStep{{Kind: "source", File: trace.Source.Location.File, Line: trace.Source.Location.StartLine, Column: trace.Source.Location.StartColumn, Expression: trace.Source.Expression, Message: "External service input originates here."}}
		for _, p := range trace.Propagation {
			steps = append(steps, language.EvidenceStep{Kind: "propagation", File: p.Location.File, Line: p.Location.StartLine, Column: p.Location.StartColumn, Expression: p.Expression, Message: "Input propagates here."})
		}
		steps = append(steps, language.EvidenceStep{Kind: "sink", File: trace.Sink.Location.File, Line: trace.Sink.Location.StartLine, Column: trace.Sink.Location.StartColumn, Expression: trace.Sink.Expression, Message: r.description})
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: "Security", Severity: "High", Title: r.title, Description: r.description, Observation: "A bounded provenance trace connects external service input to the supplied sink operation.", Hypothesis: r.description, File: trace.Sink.Location.File, StartLine: trace.Sink.Location.StartLine, EndLine: trace.Sink.Location.EndLine, CodeSnippet: trace.Sink.Expression, Confidence: trace.Confidence, EvidenceStrength: string(trace.Strength), EvidenceSteps: steps})
	}
	return out
}

func (*unsafeRule) ID() string           { return "RUST-COR-001" }
func (*unsafeRule) CategoryName() string { return "Correctness" }
func (*unsafeRule) SeverityName() string { return "Low" }
func (r *unsafeRule) Check(g *graph.Graph) []language.PotentialFinding {
	var out []language.PotentialFinding
	for _, f := range g.SemanticIndex().Facts(semantic.FactControl) {
		if f.Operation != "rust.unsafe_block" {
			continue
		}
		description := "An unsafe block bypasses Rust safety checks and requires manual invariant review."
		out = append(out, language.PotentialFinding{RuleID: r.ID(), Category: r.CategoryName(), Severity: r.SeverityName(), Title: "Unsafe Rust block", Description: description, Observation: "The supplied source contains a Rust unsafe block.", Hypothesis: "The block may violate memory-safety invariants unless its surrounding checks and documented invariants are sufficient.", File: f.Location.File, StartLine: f.Location.StartLine, EndLine: f.Location.EndLine, CodeSnippet: f.Expression, Confidence: .75, EvidenceStrength: "moderate", EvidenceSteps: []language.EvidenceStep{{Kind: "evidence", File: f.Location.File, Line: f.Location.StartLine, Column: f.Location.StartColumn, Expression: f.Expression, Message: description}}})
	}
	return out
}
func All() []language.AnalysisRule {
	return []language.AnalysisRule{
		&unsafeRule{},
		&factRule{id: "RUST-ASYNC-001", category: "Correctness", severity: "Medium", kind: semantic.FactGuard, operation: "rust.async.blocking_sleep", title: "Blocking sleep in async context", description: "std::thread::sleep blocks a Tokio async worker outside spawn_blocking."},
		&traceRule{id: "RUST-SQL-SEC-001", sink: "rust.sql.dynamic", title: "External input reaches dynamic Rust SQL", description: "Axum, Actix, or Tonic input reaches a dynamic SQLx/Diesel raw query."},
		&traceRule{id: "RUST-CMD-SEC-001", sink: "rust.shell.command", title: "External input reaches shell command", description: "External service input reaches sh/bash -c interpretation."},
		&traceRule{id: "RUST-FS-SEC-001", sink: "rust.filesystem.path", title: "External input reaches filesystem", description: "External service input controls a std::fs path."},
		&traceRule{id: "RUST-SSRF-SEC-001", sink: "rust.http.outbound_url", title: "External input reaches reqwest URL", description: "External service input controls a reqwest outbound URL."},
		&factRule{id: "RUST-TLS-SEC-001", category: "Security", severity: "High", kind: semantic.FactGuard, operation: "rust.tls.invalid_certs", title: "Reqwest accepts invalid certificates", description: "Reqwest client explicitly accepts invalid TLS certificates."},
	}
}
