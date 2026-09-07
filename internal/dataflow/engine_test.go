package dataflow

import (
	"testing"

	"github.com/poizdev/lookup/internal/semantic"
)

func TestAnalyzeTracesLocalSourcePropagationToSink(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Language: "Go", Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		fact("format", semantic.FactPropagation, "fmt.Sprintf", 3, []string{"id"}, []string{"query"}, "handler"),
		fact("sink", semantic.FactSink, "sql.raw", 4, []string{"query"}, nil, "handler"),
	}}

	traces := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits())
	if len(traces) != 1 {
		t.Fatalf("traces = %#v", traces)
	}
	trace := traces[0]
	if trace.Source.ID != "source" || trace.Sink.ID != "sink" || len(trace.Propagation) != 1 || trace.Propagation[0].ID != "format" {
		t.Fatalf("trace = %#v", trace)
	}
	if trace.Confidence != ConfidenceLocal || trace.Strength != EvidenceStrong {
		t.Fatalf("assessment = confidence %.2f strength %s", trace.Confidence, trace.Strength)
	}
}

func TestAnalyzeAssignsDirectConfidenceWithoutPropagation(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.path", 2, nil, []string{"id"}, "handler"),
		fact("sink", semantic.FactSink, "command.exec", 3, []string{"id"}, nil, "handler"),
	}}
	traces := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits())
	if len(traces) != 1 || traces[0].Confidence != ConfidenceDirect {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAnalyzePropagatesTaintIntoCalledFunctionWithinDepth(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{
		{Name: "dangerous", Parameters: []string{"value"}},
	}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		fact("call", semantic.FactCall, "dangerous", 3, []string{"id"}, nil, "handler"),
		fact("sink", semantic.FactSink, "sql.exec", 7, []string{"value"}, nil, "dangerous"),
	}}

	limits := DefaultLimits()
	limits.MaxCallDepth = 1
	traces := Analyze(semantic.NewIndex([]*semantic.Document{document}), limits)
	if len(traces) != 1 || !traces[0].Interprocedural || traces[0].Confidence != ConfidenceInterprocedural {
		t.Fatalf("traces = %#v", traces)
	}
	limits.MaxCallDepth = 0
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), limits); len(got) != 0 {
		t.Fatalf("zero-depth traces = %#v", got)
	}
}

func TestAnalyzePreservesArgumentPositionWhenLiteralsPrecedeTaint(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{
		{Name: "dangerous", Parameters: []string{"prefix", "value"}},
	}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		{ID: "call", Kind: semantic.FactCall, Operation: "dangerous", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Arguments: []string{`"fixed"`, "id"}, Inputs: []string{"id"}},
		fact("sink", semantic.FactSink, "sql.exec", 7, []string{"value"}, nil, "dangerous"),
	}}
	traces := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits())
	if len(traces) != 1 || !traces[0].Interprocedural {
		t.Fatalf("positional traces = %#v", traces)
	}
}

func TestAnalyzePropagatesIdentifiersInsideCallArgumentExpressions(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{{Name: "dangerous", Parameters: []string{"value"}}}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		{ID: "call", Kind: semantic.FactCall, Operation: "dangerous", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Arguments: []string{`"prefix" + id`}, ArgumentInputs: [][]string{{"id"}}, Inputs: []string{"id"}},
		fact("sink", semantic.FactSink, "sql.exec", 7, []string{"value"}, nil, "dangerous"),
	}}
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 1 || !got[0].Interprocedural {
		t.Fatalf("expression argument traces = %#v", got)
	}
}

func TestAnalyzePropagatesSimpleReturnedParameter(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{
		{Name: "identity", Parameters: []string{"prefix", "value"}},
	}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		{ID: "return", Kind: semantic.FactReturn, Operation: "return", Function: "identity", Location: semantic.Location{File: "handler.go", StartLine: 10}, Inputs: []string{"value"}},
		{ID: "assign", Kind: semantic.FactPropagation, Operation: "identity", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Arguments: []string{`"fixed"`, "id"}, Inputs: []string{"id"}, Outputs: []string{"query"}},
		fact("sink", semantic.FactSink, "sql.exec", 7, []string{"query"}, nil, "handler"),
	}}
	traces := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits())
	if len(traces) != 1 || !traces[0].Interprocedural || traces[0].Confidence != ConfidenceInterprocedural {
		t.Fatalf("return traces = %#v", traces)
	}
}

func TestAnalyzePropagatesReturnedLocalAlias(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{{Name: "identity", Parameters: []string{"value"}}}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		fact("alias", semantic.FactPropagation, "assignment", 8, []string{"value"}, []string{"alias"}, "identity"),
		fact("return", semantic.FactReturn, "return", 9, []string{"alias"}, nil, "identity"),
		{ID: "assign", Kind: semantic.FactPropagation, Operation: "identity", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Arguments: []string{"id"}, ArgumentInputs: [][]string{{"id"}}, Inputs: []string{"id"}, Outputs: []string{"query"}},
		fact("sink", semantic.FactSink, "sql.exec", 5, []string{"query"}, nil, "handler"),
	}}
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 1 {
		t.Fatalf("returned alias traces = %#v", got)
	}
}

func TestAnalyzeDoesNotResolveQualifiedExternalCallToLocalBasename(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{{Name: "Sprintf", Parameters: []string{"clean"}}}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		{ID: "format", Kind: semantic.FactPropagation, Operation: "fmt.Sprintf", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Inputs: []string{"id"}, Outputs: []string{"query"}, Metadata: map[string]string{"external_import": "true"}},
		fact("sink", semantic.FactSink, "sql.exec", 4, []string{"query"}, nil, "handler"),
	}}
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 1 {
		t.Fatalf("external basename traces = %#v", got)
	}
}

func TestAnalyzeBoundsChainedReturnPropagationByCallDepth(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{
		{Name: "first", Parameters: []string{"value"}},
		{Name: "second", Parameters: []string{"value"}},
	}, Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"id"}, "handler"),
		{ID: "return-first", Kind: semantic.FactReturn, Operation: "return", Function: "first", Location: semantic.Location{File: "handler.go", StartLine: 12}, Inputs: []string{"value"}},
		{ID: "return-second", Kind: semantic.FactReturn, Operation: "return", Function: "second", Location: semantic.Location{File: "handler.go", StartLine: 16}, Inputs: []string{"value"}},
		{ID: "assign-first", Kind: semantic.FactPropagation, Operation: "first", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 3}, Arguments: []string{"id"}, Inputs: []string{"id"}, Outputs: []string{"one"}},
		{ID: "assign-second", Kind: semantic.FactPropagation, Operation: "second", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 4}, Arguments: []string{"one"}, Inputs: []string{"one"}, Outputs: []string{"two"}},
		fact("sink", semantic.FactSink, "sql.exec", 9, []string{"two"}, nil, "handler"),
	}}
	limits := DefaultLimits()
	limits.MaxCallDepth = 1
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), limits); len(got) != 0 {
		t.Fatalf("depth-one chained traces = %#v", got)
	}
	limits.MaxCallDepth = 2
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), limits); len(got) != 1 {
		t.Fatalf("depth-two chained traces = %#v", got)
	}
}

func TestAnalyzeStopsAtStepLimit(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 1, nil, []string{"a"}, "handler"),
		fact("p1", semantic.FactPropagation, "assignment", 2, []string{"a"}, []string{"b"}, "handler"),
		fact("p2", semantic.FactPropagation, "assignment", 3, []string{"b"}, []string{"c"}, "handler"),
		fact("sink", semantic.FactSink, "sql.raw", 4, []string{"c"}, nil, "handler"),
	}}
	limits := DefaultLimits()
	limits.MaxSteps = 1
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), limits); len(got) != 0 {
		t.Fatalf("step-limited traces = %#v", got)
	}
}

func TestAnalyzeRespectsProgramOrderAndLiteralReassignmentKills(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		fact("early-sink", semantic.FactSink, "sql.raw", 1, []string{"value"}, nil, "handler"),
		fact("source", semantic.FactSource, "http.query", 2, nil, []string{"value"}, "handler"),
		fact("overwrite", semantic.FactPropagation, "assignment", 3, nil, []string{"value"}, "handler"),
		fact("late-sink", semantic.FactSink, "sql.raw", 4, []string{"value"}, nil, "handler"),
	}}

	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 0 {
		t.Fatalf("ordered traces = %#v", got)
	}
}

func TestAnalyzeRetainsSinkReachedBeforeLaterOverwrite(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 1, nil, []string{"value"}, "handler"),
		fact("sink", semantic.FactSink, "sql.raw", 2, []string{"value"}, nil, "handler"),
		fact("overwrite", semantic.FactPropagation, "assignment", 3, nil, []string{"value"}, "handler"),
	}}
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 1 {
		t.Fatalf("pre-overwrite traces = %#v", got)
	}
}

func TestAnalyzeStopsAtExplicitSanitizerFact(t *testing.T) {
	document := &semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		fact("source", semantic.FactSource, "http.query", 1, nil, []string{"value"}, "handler"),
		fact("sanitizer", semantic.FactSanitizer, "validation", 2, []string{"value"}, []string{"value"}, "handler"),
		fact("sink", semantic.FactSink, "sql.raw", 3, []string{"value"}, nil, "handler"),
	}}
	if got := Analyze(semantic.NewIndex([]*semantic.Document{document}), DefaultLimits()); len(got) != 0 {
		t.Fatalf("sanitized traces = %#v", got)
	}
}

func TestAnalyzeDoesNotCrossTaintSameNamedFunctionsInDifferentFiles(t *testing.T) {
	sourceDocument := &semantic.Document{Path: "source.go", Functions: []semantic.Function{{Name: "handler"}}, Facts: []semantic.Fact{
		{ID: "source", Kind: semantic.FactSource, Operation: "http.query", Location: semantic.Location{File: "source.go", StartLine: 2}, Outputs: []string{"value"}, Function: "handler"},
	}}
	sinkDocument := &semantic.Document{Path: "sink.go", Functions: []semantic.Function{{Name: "handler"}}, Facts: []semantic.Fact{
		{ID: "sink", Kind: semantic.FactSink, Operation: "sql.raw", Location: semantic.Location{File: "sink.go", StartLine: 2}, Inputs: []string{"value"}, Function: "handler"},
	}}

	if got := Analyze(semantic.NewIndex([]*semantic.Document{sourceDocument, sinkDocument}), DefaultLimits()); len(got) != 0 {
		t.Fatalf("cross-file traces = %#v", got)
	}
}

func TestAnalyzePropagatesToUniquelyResolvedFunctionInAnotherFile(t *testing.T) {
	sourceDocument := &semantic.Document{Path: "handler.go", Functions: []semantic.Function{{Name: "handler"}}, Facts: []semantic.Fact{
		{ID: "source", Kind: semantic.FactSource, Operation: "http.query", Location: semantic.Location{File: "handler.go", StartLine: 2}, Outputs: []string{"value"}, Function: "handler"},
		{ID: "call", Kind: semantic.FactCall, Operation: "dangerous", Location: semantic.Location{File: "handler.go", StartLine: 3}, Inputs: []string{"value"}, Arguments: []string{"value"}, Function: "handler"},
	}}
	sinkDocument := &semantic.Document{Path: "repository.go", Functions: []semantic.Function{{Name: "dangerous", Parameters: []string{"input"}}}, Facts: []semantic.Fact{
		{ID: "sink", Kind: semantic.FactSink, Operation: "sql.raw", Location: semantic.Location{File: "repository.go", StartLine: 8}, Inputs: []string{"input"}, Function: "dangerous"},
	}}

	got := Analyze(semantic.NewIndex([]*semantic.Document{sourceDocument, sinkDocument}), DefaultLimits())
	if len(got) != 1 || !got[0].Interprocedural || got[0].Sink.Location.File != "repository.go" {
		t.Fatalf("cross-file trace = %#v", got)
	}
}

func TestAnalyzeSkipsAmbiguousSameNamedCallees(t *testing.T) {
	documents := []*semantic.Document{
		{Path: "handler.go", Functions: []semantic.Function{{Name: "handler"}}, Facts: []semantic.Fact{
			{ID: "source", Kind: semantic.FactSource, Location: semantic.Location{File: "handler.go"}, Outputs: []string{"value"}, Function: "handler"},
			{ID: "call", Kind: semantic.FactCall, Operation: "Save", Location: semantic.Location{File: "handler.go"}, Inputs: []string{"value"}, Arguments: []string{"value"}, Function: "handler"},
		}},
		{Path: "one.go", Functions: []semantic.Function{{Name: "One.Save", Parameters: []string{"input"}}}, Facts: []semantic.Fact{
			{ID: "sink-one", Kind: semantic.FactSink, Location: semantic.Location{File: "one.go"}, Inputs: []string{"input"}, Function: "One.Save"},
		}},
		{Path: "two.go", Functions: []semantic.Function{{Name: "Two.Save", Parameters: []string{"input"}}}, Facts: []semantic.Fact{
			{ID: "sink-two", Kind: semantic.FactSink, Location: semantic.Location{File: "two.go"}, Inputs: []string{"input"}, Function: "Two.Save"},
		}},
	}
	if got := Analyze(semantic.NewIndex(documents), DefaultLimits()); len(got) != 0 {
		t.Fatalf("ambiguous traces = %#v", got)
	}
}

func fact(id string, kind semantic.FactKind, operation string, line int, inputs, outputs []string, function string) semantic.Fact {
	return semantic.Fact{ID: id, Kind: kind, Operation: operation, Location: semantic.Location{File: "handler.go", StartLine: line}, Inputs: inputs, Outputs: outputs, Function: function, Expression: operation}
}
