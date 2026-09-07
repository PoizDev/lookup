package analyzer

import (
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	golangadapter "github.com/poizdev/lookup/internal/language/golang"
	golangrules "github.com/poizdev/lookup/internal/language/golang/rules"
	"github.com/poizdev/lookup/internal/semantic"
)

type testRule struct {
	id       string
	findings []language.PotentialFinding
}

func TestEngineAddsLanguageRulesFromRegistry(t *testing.T) {
	registry := language.NewRegistry()
	if err := registry.Register(&golangadapter.Adapter{}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if err := engine.AddRegistryLanguageRules(registry); err != nil {
		t.Fatal(err)
	}
	if got, want := engine.RuleCount(), len(golangrules.All()); got != want {
		t.Fatalf("RuleCount() = %d, want %d", got, want)
	}
}

type scopedTestRule struct {
	testRule
	required []semantic.Ecosystem
	calls    *atomic.Int32
}

type panicRule struct{ id string }

func (r panicRule) ID() string                                   { return r.id }
func (panicRule) Category() Category                             { return CategorySecurity }
func (panicRule) Severity() Severity                             { return SeverityLow }
func (panicRule) Check(*graph.Graph) []language.PotentialFinding { panic("boom") }

func (r scopedTestRule) RequiredEcosystems() []semantic.Ecosystem { return r.required }
func (r scopedTestRule) Check(g *graph.Graph) []language.PotentialFinding {
	r.calls.Add(1)
	return r.testRule.Check(g)
}

func (r testRule) ID() string         { return r.id }
func (r testRule) Category() Category { return CategorySecurity }
func (r testRule) Severity() Severity { return SeverityLow }
func (r testRule) Check(*graph.Graph) []language.PotentialFinding {
	return append([]language.PotentialFinding(nil), r.findings...)
}

func TestNewEngineCheckedRejectsDuplicateRuleIDs(t *testing.T) {
	_, err := NewEngineChecked(testRule{id: "GO-SEC-001"}, testRule{id: "GO-SEC-001"})
	if err == nil {
		t.Fatal("expected duplicate rule ID to be rejected")
	}
}

func TestEngineAddRuleRejectsEmptyID(t *testing.T) {
	engine, err := NewEngineChecked()
	if err != nil {
		t.Fatalf("NewEngineChecked: %v", err)
	}
	if err := engine.AddRule(testRule{}); err == nil {
		t.Fatal("expected empty rule ID to be rejected")
	}
}

func TestEngineRunReturnsStableFindingOrder(t *testing.T) {
	engine, err := NewEngineChecked(
		testRule{id: "GO-SEC-002", findings: []language.PotentialFinding{
			{RuleID: "GO-SEC-002", File: "z.go", StartLine: 9, Title: "later", Observation: "observed", Hypothesis: "possible"},
			{RuleID: "GO-SEC-002", File: "a.go", StartLine: 2, Title: "first", Observation: "observed", Hypothesis: "possible"},
		}},
		testRule{id: "GO-SEC-001", findings: []language.PotentialFinding{
			{RuleID: "GO-SEC-001", File: "z.go", StartLine: 4, Title: "earliest rule", Observation: "observed", Hypothesis: "possible"},
		}},
	)
	if err != nil {
		t.Fatalf("NewEngineChecked: %v", err)
	}

	want := []string{"GO-SEC-001:z.go:4", "GO-SEC-002:a.go:2", "GO-SEC-002:z.go:9"}
	for run := 0; run < 20; run++ {
		gotFindings := engine.Run(graph.NewGraph())
		got := make([]string, 0, len(gotFindings))
		for _, finding := range gotFindings {
			got = append(got, finding.RuleID+":"+finding.File+":"+itoa(finding.StartLine))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d order = %v, want %v", run, got, want)
		}
	}
}

func TestRunCheckedRejectsContextFindingWithoutExplicitSemanticContract(t *testing.T) {
	engine := NewEngine(testRule{id: "GO-COR-999", findings: []language.PotentialFinding{{RuleID: "GO-COR-999", Title: "judgmental fallback"}}})
	findings, err := engine.RunChecked(graph.NewGraph())
	if err == nil || !strings.Contains(err.Error(), "without explicit observation and hypothesis") {
		t.Fatalf("RunChecked error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none from invalid contextual emitter", findings)
	}
}

func TestEngineSkipsRulesWhoseEcosystemIsNotDetected(t *testing.T) {
	var calls atomic.Int32
	engine, err := NewEngineChecked(scopedTestRule{testRule: testRule{id: "GIN-SEC-001"}, required: []semantic.Ecosystem{semantic.EcosystemGin}, calls: &calls})
	if err != nil {
		t.Fatal(err)
	}
	engine.Run(graph.NewGraph())
	if calls.Load() != 0 {
		t.Fatalf("inactive Gin rule called %d times", calls.Load())
	}

	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "handler.go", Imports: []string{"github.com/gin-gonic/gin"}})
	engine.Run(g)
	if calls.Load() != 1 {
		t.Fatalf("active Gin rule called %d times", calls.Load())
	}
}

func TestRunCheckedReportsRulePanicDeterministically(t *testing.T) {
	engine := NewEngine(testRule{id: "GO-OK-001"}, panicRule{id: "GO-PANIC-001"})
	findings, err := engine.RunChecked(graph.NewGraph())
	if err == nil || !strings.Contains(err.Error(), "GO-PANIC-001") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("panic diagnostic = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
