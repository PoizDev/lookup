package maintainability

import (
	"testing"

	"github.com/poizdev/lookup/internal/language"
)

func TestMeasurementRulesAreStaticAuthoritative(t *testing.T) {
	for name, rule := range map[string]interface{ ReviewPolicy() language.ReviewPolicy }{
		"cyclomatic complexity": &CyclomaticComplexity{},
		"function length":       &FunctionLength{},
	} {
		if got := rule.ReviewPolicy(); got != language.ReviewPolicyStaticAuthoritative {
			t.Fatalf("%s ReviewPolicy = %q", name, got)
		}
	}
}
