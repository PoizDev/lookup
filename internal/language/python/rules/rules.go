package rules

import (
	"sort"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/semantic"
)

type factRule struct {
	id, category, severity        string
	kind                          semantic.FactKind
	operation, title, description string
}

type traceRule struct{ id, severity, sink, title, description string }

func (r *traceRule) ID() string           { return r.id }
func (*traceRule) CategoryName() string   { return "Security" }
func (r *traceRule) SeverityName() string { return r.severity }
func (r *traceRule) Check(g *graph.Graph) []language.PotentialFinding {
	var out []language.PotentialFinding
	for _, trace := range g.DataflowTraces() {
		if trace.Sink.Operation != r.sink {
			continue
		}
		steps := []language.EvidenceStep{{Kind: "source", File: trace.Source.Location.File, Line: trace.Source.Location.StartLine, Column: trace.Source.Location.StartColumn, Expression: trace.Source.Expression, Message: "External request input originates here."}}
		for _, p := range trace.Propagation {
			steps = append(steps, language.EvidenceStep{Kind: "propagation", File: p.Location.File, Line: p.Location.StartLine, Column: p.Location.StartColumn, Expression: p.Expression, Message: "Input propagates through this value."})
		}
		steps = append(steps, language.EvidenceStep{Kind: "sink", File: trace.Sink.Location.File, Line: trace.Sink.Location.StartLine, Column: trace.Sink.Location.StartColumn, Expression: trace.Sink.Expression, Message: r.description})
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: "Security", Severity: r.severity, Title: r.title, Description: r.description, Observation: "A bounded provenance trace connects external request input to the supplied sink operation.", Hypothesis: r.description, File: trace.Sink.Location.File, StartLine: trace.Sink.Location.StartLine, EndLine: trace.Sink.Location.EndLine, CodeSnippet: trace.Sink.Expression, AffectedSymbols: append([]string(nil), trace.Sink.Inputs...), Confidence: trace.Confidence, EvidenceStrength: string(trace.Strength), EvidenceSteps: steps})
	}
	return out
}

type openCVRule struct{ resource bool }

func (r *openCVRule) ID() string {
	if r.resource {
		return "PY-CV-RES-001"
	}
	return "PY-CV-COR-001"
}
func (*openCVRule) CategoryName() string { return "Correctness" }
func (*openCVRule) SeverityName() string { return "Medium" }
func (r *openCVRule) Check(g *graph.Graph) []language.PotentialFinding {
	index := g.SemanticIndex()
	if r.resource {
		return captureFindings(r, index)
	}
	var out []language.PotentialFinding
	loads := index.Facts(semantic.FactCall)
	guards := index.Facts(semantic.FactGuard)
	for _, load := range loads {
		if load.Operation != "python.opencv.imread" || len(load.Outputs) == 0 {
			continue
		}
		image := load.Outputs[0]
		for _, consumer := range guards {
			if consumer.Operation != "python.opencv.image_consumer" || !after(load, consumer) || !has(consumer.Inputs, image) {
				continue
			}
			checked := false
			for _, guard := range guards {
				if guard.Operation == "python.opencv.image_checked" && after(load, guard) && after(guard, consumer) && has(guard.Inputs, image) {
					checked = true
				}
			}
			if !checked {
				out = append(out, finding(r, consumer, "OpenCV image consumed without a visible None check", .9, "strong"))
			}
		}
	}
	return out
}
func captureFindings(r *openCVRule, index *semantic.Index) []language.PotentialFinding {
	var out []language.PotentialFinding
	releases := index.Facts(semantic.FactResourceRelease)
	returns := index.Facts(semantic.FactReturn)
	for _, acquire := range index.Facts(semantic.FactResourceAcquire) {
		if acquire.Operation != "python.opencv.capture" || len(acquire.Outputs) == 0 {
			continue
		}
		name := acquire.Outputs[0]
		released, escaped := false, false
		for _, release := range releases {
			if after(acquire, release) && has(release.Inputs, name) {
				released = true
			}
		}
		for _, ret := range returns {
			if after(acquire, ret) && has(ret.Inputs, name) {
				escaped = true
			}
		}
		if !released && !escaped {
			out = append(out, finding(r, acquire, "OpenCV capture is not released and does not escape", .8, "moderate"))
		}
	}
	return out
}
func finding(r *openCVRule, f semantic.Fact, description string, confidence float64, strength string) language.PotentialFinding {
	return language.PotentialFinding{RuleID: r.ID(), Category: r.CategoryName(), Severity: r.SeverityName(), Title: description, Description: description, Observation: "The OpenCV operation and its bounded same-function lifecycle or guard relationship match the rule predicate.", Hypothesis: description, File: f.Location.File, StartLine: f.Location.StartLine, EndLine: f.Location.EndLine, CodeSnippet: f.Expression, Confidence: confidence, EvidenceStrength: strength, EvidenceSteps: []language.EvidenceStep{{Kind: "evidence", File: f.Location.File, Line: f.Location.StartLine, Column: f.Location.StartColumn, Expression: f.Expression, Message: description}}}
}
func after(a, b semantic.Fact) bool {
	return a.Location.File == b.Location.File && a.Function == b.Function && (a.Location.StartLine < b.Location.StartLine || a.Location.StartLine == b.Location.StartLine && a.Location.StartColumn <= b.Location.StartColumn)
}
func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
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
		out = append(out, language.PotentialFinding{RuleID: r.id, Category: r.category, Severity: r.severity, Title: r.title, Description: r.description, Observation: "The semantic extractor emitted operation " + r.operation + " at the supplied source location.", Hypothesis: r.description, File: f.Location.File, StartLine: f.Location.StartLine, EndLine: f.Location.EndLine, CodeSnippet: f.Expression, AffectedSymbols: append([]string(nil), f.Inputs...), Confidence: .95, EvidenceStrength: "strong", EvidenceSteps: []language.EvidenceStep{{Kind: "evidence", File: f.Location.File, Line: f.Location.StartLine, Column: f.Location.StartColumn, Expression: f.Expression, Message: r.description}}})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out
}
func All() []language.AnalysisRule {
	return []language.AnalysisRule{
		&factRule{id: "PY-SEC-001", category: "Security", severity: "High", kind: semantic.FactSink, operation: "python.builtin.eval", title: "Dynamic Python execution", description: "Unshadowed Python eval executes dynamically supplied code."},
		&factRule{id: "PY-SEC-002", category: "Security", severity: "High", kind: semantic.FactSink, operation: "python.builtin.exec", title: "Dynamic Python execution", description: "Unshadowed Python exec executes dynamically supplied code."},
		&factRule{id: "PY-COR-001", category: "Correctness", severity: "Medium", kind: semantic.FactGuard, operation: "python.mutable_default", title: "Mutable default argument", description: "A mutable default value is shared between function calls."},
		&traceRule{id: "PY-CMD-SEC-001", severity: "High", sink: "python.shell.command", title: "Request input reaches shell execution", description: "External request input reaches a shell-interpreted command."},
		&traceRule{id: "PY-SQL-SEC-001", severity: "High", sink: "python.sql.dynamic", title: "Request input reaches dynamic SQL", description: "External request input reaches a provenance-qualified DB-API execute call as dynamic SQL."},
		&traceRule{id: "PY-FS-SEC-001", severity: "High", sink: "python.filesystem.path", title: "Request input reaches filesystem path", description: "External request input controls a filesystem path."},
		&traceRule{id: "PY-DESER-SEC-001", severity: "High", sink: "python.deserialize.unsafe", title: "Request input reaches unsafe deserialization", description: "External request bytes reach Python pickle deserialization."},
		&traceRule{id: "PY-ML-SEC-001", severity: "High", sink: "python.torch.load.unsafe", title: "Untrusted artifact reaches unsafe PyTorch loading", description: "External request input reaches torch.load with weights_only explicitly disabled."},
		&factRule{id: "PY-TLS-SEC-001", category: "Security", severity: "Medium", kind: semantic.FactGuard, operation: "python.tls.verify_disabled", title: "TLS certificate verification disabled", description: "An HTTP request explicitly disables TLS certificate verification."},
		&openCVRule{}, &openCVRule{resource: true},
	}
}
