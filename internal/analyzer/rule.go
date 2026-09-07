package analyzer

import (
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/semantic"
)

// ReviewPolicyRule is an optional capability. Rules default to contextual
// review so existing implementations remain source compatible and safe.
type ReviewPolicyRule interface {
	ReviewPolicy() finding.ReviewPolicy
}

// Category represents the category of a finding.
type Category string

const (
	CategorySecurity        Category = "Security"
	CategoryPerformance     Category = "Performance"
	CategoryArchitecture    Category = "Architecture"
	CategoryCorrectness     Category = "Correctness"
	CategoryMaintainability Category = "Maintainability"
)

// Severity represents the severity level of a finding.
type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityHigh     Severity = "High"
	SeverityMedium   Severity = "Medium"
	SeverityLow      Severity = "Low"
	SeverityInfo     Severity = "Info"
)

// Rule is the interface that all analysis rules must implement.
// Generic rules (internal/analyzer/generic/) implement this directly.
// Language-specific rules are wrapped via langRuleAdapter.
type Rule interface {
	// ID returns a unique identifier for this rule (e.g. "SEC-001").
	ID() string

	// Category returns the category this rule belongs to.
	Category() Category

	// Severity returns the default severity of findings from this rule.
	Severity() Severity

	// Check runs the rule against the code graph and returns potential findings.
	Check(g *graph.Graph) []language.PotentialFinding
}

// langRuleAdapter wraps a language.AnalysisRule to implement analyzer.Rule.
type langRuleAdapter struct {
	inner language.AnalysisRule
}

func (a *langRuleAdapter) ID() string         { return a.inner.ID() }
func (a *langRuleAdapter) Category() Category { return Category(a.inner.CategoryName()) }
func (a *langRuleAdapter) Severity() Severity { return Severity(a.inner.SeverityName()) }
func (a *langRuleAdapter) Check(g *graph.Graph) []language.PotentialFinding {
	return a.inner.Check(g)
}
func (a *langRuleAdapter) ReviewPolicy() finding.ReviewPolicy {
	if policy, ok := a.inner.(interface{ ReviewPolicy() finding.ReviewPolicy }); ok {
		return policy.ReviewPolicy()
	}
	return finding.ReviewPolicyContextRequired
}
func (a *langRuleAdapter) RequiredEcosystems() []semantic.Ecosystem {
	if scoped, ok := a.inner.(language.EcosystemRule); ok {
		return scoped.RequiredEcosystems()
	}
	return nil
}

// WrapLanguageRule wraps a language.AnalysisRule into an analyzer.Rule.
func WrapLanguageRule(r language.AnalysisRule) Rule {
	return &langRuleAdapter{inner: r}
}
