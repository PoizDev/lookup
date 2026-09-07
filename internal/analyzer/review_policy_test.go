package analyzer

import (
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

type policyTestRule struct{ authoritative bool }

func (r policyTestRule) ID() string         { return "TEST-001" }
func (r policyTestRule) Category() Category { return CategoryMaintainability }
func (r policyTestRule) Severity() Severity { return SeverityMedium }
func (r policyTestRule) Check(*graph.Graph) []language.PotentialFinding {
	return []language.PotentialFinding{{RuleID: r.ID(), Title: "measured fact", Observation: "measured", Hypothesis: "possible consequence"}}
}
func (r policyTestRule) ReviewPolicy() language.ReviewPolicy {
	if r.authoritative {
		return language.ReviewPolicyStaticAuthoritative
	}
	return language.ReviewPolicyContextRequired
}

type defaultPolicyTestRule struct{ policyTestRule }

func (defaultPolicyTestRule) ReviewPolicy() {}

func TestRulesDefaultToContextRequired(t *testing.T) {
	rule := ruleWithoutPolicy{}
	engine := NewEngine(rule)
	got := engine.Run(graph.NewGraph())
	if len(got) != 1 || got[0].ReviewPolicy != language.ReviewPolicyContextRequired {
		t.Fatalf("findings = %#v, want context-required policy", got)
	}
}

func TestRuleMayOptIntoStaticAuthority(t *testing.T) {
	engine := NewEngine(policyTestRule{authoritative: true})
	got := engine.Run(graph.NewGraph())
	if len(got) != 1 || got[0].ReviewPolicy != language.ReviewPolicyStaticAuthoritative {
		t.Fatalf("findings = %#v, want static-authoritative policy", got)
	}
}

type ruleWithoutPolicy struct{}

func (ruleWithoutPolicy) ID() string         { return "TEST-DEFAULT" }
func (ruleWithoutPolicy) Category() Category { return CategoryCorrectness }
func (ruleWithoutPolicy) Severity() Severity { return SeverityHigh }
func (ruleWithoutPolicy) Check(*graph.Graph) []language.PotentialFinding {
	return []language.PotentialFinding{{RuleID: "TEST-DEFAULT", Title: "candidate", Observation: "observed", Hypothesis: "possible consequence"}}
}
