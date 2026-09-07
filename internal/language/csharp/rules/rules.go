package rules

import (
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/semantic"
)

type rule struct {
	id, category, severity        string
	kind                          semantic.FactKind
	operation, title, description string
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
		steps := []language.EvidenceStep{{Kind: "source", File: trace.Source.Location.File, Line: trace.Source.Location.StartLine, Column: trace.Source.Location.StartColumn, Expression: trace.Source.Expression, Message: "ASP.NET request input originates here."}}
		for _, p := range trace.Propagation {
			steps = append(steps, language.EvidenceStep{Kind: "propagation", File: p.Location.File, Line: p.Location.StartLine, Column: p.Location.StartColumn, Expression: p.Expression, Message: "Request input propagates here."})
		}
		steps = append(steps, language.EvidenceStep{Kind: "sink", File: trace.Sink.Location.File, Line: trace.Sink.Location.StartLine, Column: trace.Sink.Location.StartColumn, Expression: trace.Sink.Expression, Message: r.description})
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: "Security", Severity: "High", Title: r.title, Description: r.description, Observation: "A bounded provenance trace connects ASP.NET request input to the supplied sink operation.", Hypothesis: r.description, File: trace.Sink.Location.File, StartLine: trace.Sink.Location.StartLine, EndLine: trace.Sink.Location.EndLine, CodeSnippet: trace.Sink.Expression, Confidence: trace.Confidence, EvidenceStrength: string(trace.Strength), EvidenceSteps: steps})
	}
	return out
}

func (r *rule) ID() string           { return r.id }
func (r *rule) CategoryName() string { return r.category }
func (r *rule) SeverityName() string { return r.severity }
func (r *rule) Check(g *graph.Graph) []language.PotentialFinding {
	var out []language.PotentialFinding
	for _, f := range g.SemanticIndex().Facts(r.kind) {
		if f.Operation != r.operation {
			continue
		}
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: r.category, Severity: r.severity, Title: r.title, Description: r.description, Observation: "The semantic extractor emitted operation " + r.operation + " at the supplied source location.", Hypothesis: r.description, File: f.Location.File, StartLine: f.Location.StartLine, EndLine: f.Location.EndLine, CodeSnippet: f.Expression, Confidence: .9, EvidenceStrength: "strong", EvidenceSteps: []language.EvidenceStep{{Kind: "evidence", File: f.Location.File, Line: f.Location.StartLine, Column: f.Location.StartColumn, Expression: f.Expression, Message: r.description}}})
	}
	return out
}
func All() []language.AnalysisRule {
	return []language.AnalysisRule{
		&rule{id: "CSHARP-COR-001", category: "Correctness", severity: "Medium", kind: semantic.FactGuard, operation: "csharp.async_void", title: "Async void method", description: "Async void methods cannot be awaited and propagate exceptions outside normal Task handling."},
		&rule{id: "CSHARP-SEC-001", category: "Security", severity: "High", kind: semantic.FactSink, operation: "csharp.ef.raw_sql.dynamic", title: "Dynamic EF raw SQL", description: "Dynamic SQL reaches a provenance-qualified EF raw SQL API without parameterization."},
		&rule{id: "CSHARP-ASYNC-001", category: "Correctness", severity: "Medium", kind: semantic.FactGuard, operation: "csharp.task.blocking", title: "Task blocked in async context", description: "A provenance-qualified Task is synchronously waited inside an async method."},
		&traceRule{id: "CSHARP-CMD-SEC-001", sink: "csharp.shell.command", title: "Request input reaches shell process", description: "ASP.NET request input reaches cmd or PowerShell shell interpretation."},
		&traceRule{id: "CSHARP-FS-SEC-001", sink: "csharp.filesystem.path", title: "Request input reaches filesystem", description: "ASP.NET request input controls a System.IO filesystem path."},
		&traceRule{id: "CSHARP-SSRF-SEC-001", sink: "csharp.http.outbound_url", title: "Request input reaches outbound URL", description: "ASP.NET request input controls an HttpClient outbound URL."},
		&rule{id: "CSHARP-TLS-SEC-001", category: "Security", severity: "High", kind: semantic.FactGuard, operation: "csharp.tls.validation_bypass", title: "TLS certificate validation bypassed", description: "Certificate validation callback unconditionally returns true."},
	}
}
