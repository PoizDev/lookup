package rules

import (
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
	"testing"
)

func pythonGraph(facts ...semantic.Fact) *graph.Graph {
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "case.py", Language: "Python", Facts: facts})
	return g
}

func fact(kind semantic.FactKind, operation, expression string) semantic.Fact {
	f := semantic.NewFact(kind, operation, semantic.Location{File: "case.py", StartLine: 3, StartColumn: 1, EndLine: 3})
	f.Expression = expression
	return f
}

func TestDynamicExecutionRequiresBuiltinProvenance(t *testing.T) {
	rule := All()[0]
	unsafe := fact(semantic.FactSink, "python.builtin.eval", "eval(user_input)")
	if got := rule.Check(pythonGraph(unsafe)); len(got) != 1 || got[0].RuleID != "PY-SEC-001" || got[0].Observation == "" || got[0].Hypothesis == "" {
		t.Fatalf("unsafe findings = %#v", got)
	}
	shadowed := fact(semantic.FactCall, "eval", "eval(value)")
	if got := rule.Check(pythonGraph(shadowed)); len(got) != 0 {
		t.Fatalf("shadowed findings = %#v", got)
	}
	execRule := All()[1]
	unsafeExec := fact(semantic.FactSink, "python.builtin.exec", "exec(user_input)")
	if got := execRule.Check(pythonGraph(unsafeExec)); len(got) != 1 || got[0].RuleID != "PY-SEC-002" {
		t.Fatalf("unsafe exec findings = %#v", got)
	}
}

func TestMutableDefaultOnlyFlagsMutableLiteral(t *testing.T) {
	rule := All()[2]
	mutable := fact(semantic.FactGuard, "python.mutable_default", "def f(items=[]):")
	if got := rule.Check(pythonGraph(mutable)); len(got) != 1 || got[0].RuleID != "PY-COR-001" || got[0].Observation == "" || got[0].Hypothesis == "" {
		t.Fatalf("mutable findings = %#v", got)
	}
	immutable := fact(semantic.FactControl, "function", "def f(value=None):")
	if got := rule.Check(pythonGraph(immutable)); len(got) != 0 {
		t.Fatalf("immutable findings = %#v", got)
	}
}
