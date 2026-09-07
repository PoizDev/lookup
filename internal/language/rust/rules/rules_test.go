package rules

import (
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
	"testing"
)

func TestUnsafeRuleIsContextualAndNonCritical(t *testing.T) {
	f := semantic.NewFact(semantic.FactControl, "rust.unsafe_block", semantic.Location{File: "case.rs", StartLine: 2})
	f.Expression = "unsafe { raw.read() }"
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "case.rs", Language: "Rust", Facts: []semantic.Fact{f}})
	rule := All()[0]
	if rule.SeverityName() == "Critical" {
		t.Fatal("unsafe presence must not be Critical")
	}
	got := rule.Check(g)
	if len(got) != 1 || got[0].RuleID != "RUST-COR-001" || got[0].Confidence >= .95 || got[0].Observation == "" || got[0].Hypothesis == "" {
		t.Fatalf("findings = %#v", got)
	}
	safe := graph.NewGraph()
	safe.AddDocument(&semantic.Document{Path: "safe.rs", Language: "Rust"})
	if got := rule.Check(safe); len(got) != 0 {
		t.Fatalf("safe findings = %#v", got)
	}
}
