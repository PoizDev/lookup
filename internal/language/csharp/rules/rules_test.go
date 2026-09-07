package rules

import (
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
	"testing"
)

func graphWith(facts ...semantic.Fact) *graph.Graph {
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "Case.cs", Language: "C#", Imports: []string{"Microsoft.EntityFrameworkCore"}, Facts: facts})
	return g
}
func csFact(kind semantic.FactKind, op, expr string) semantic.Fact {
	f := semantic.NewFact(kind, op, semantic.Location{File: "Case.cs", StartLine: 4})
	f.Expression = expr
	return f
}
func TestAsyncVoidRule(t *testing.T) {
	got := All()[0].Check(graphWith(csFact(semantic.FactGuard, "csharp.async_void", "async void Fire()")))
	if len(got) != 1 || got[0].RuleID != "CSHARP-COR-001" || got[0].Observation == "" || got[0].Hypothesis == "" {
		t.Fatalf("findings=%#v", got)
	}
	if got := All()[0].Check(graphWith(csFact(semantic.FactControl, "csharp.async_task", "async Task Work()"))); len(got) != 0 {
		t.Fatalf("task findings=%#v", got)
	}
}
func TestEFRawSQLRequiresDynamicEvidenceAndProvenance(t *testing.T) {
	unsafe := csFact(semantic.FactSink, "csharp.ef.raw_sql.dynamic", "db.Database.ExecuteSqlRaw(sql)")
	got := All()[1].Check(graphWith(unsafe))
	if len(got) != 1 || got[0].RuleID != "CSHARP-SEC-001" {
		t.Fatalf("unsafe findings=%#v", got)
	}
	for _, near := range []semantic.Fact{csFact(semantic.FactCall, "ExecuteSqlRaw", "other.ExecuteSqlRaw(sql)"), csFact(semantic.FactDatabaseOperation, "csharp.ef.raw_sql.parameterized", "db.Database.ExecuteSqlRaw(\"... {0}\", value)")} {
		if got := All()[1].Check(graphWith(near)); len(got) != 0 {
			t.Fatalf("near miss findings=%#v", got)
		}
	}
}
