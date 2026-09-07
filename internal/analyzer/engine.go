package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/logger"
	"github.com/poizdev/lookup/internal/semantic"
	"go.uber.org/zap"
)

// Engine runs analysis rules against a code graph.
// It collects rules from two sources:
//   - GenericRules: language-agnostic rules from internal/analyzer/generic/
//   - Language-specific rules injected via AddLanguageRules from adapter.LanguageRules()
type Engine struct {
	mu      sync.RWMutex
	rules   []Rule
	ruleIDs map[string]struct{}
}

// NewEngine creates a new analysis engine with the given generic rules.
func NewEngine(genericRules ...Rule) *Engine {
	engine, err := NewEngineChecked(genericRules...)
	if err != nil {
		panic(err)
	}
	return engine
}

// NewEngineChecked creates an engine and validates the complete initial rule set.
func NewEngineChecked(rules ...Rule) (*Engine, error) {
	engine := &Engine{ruleIDs: make(map[string]struct{}, len(rules))}
	for _, rule := range rules {
		if err := engine.AddRule(rule); err != nil {
			return nil, err
		}
	}
	return engine, nil
}

// AddRule adds a single rule to the engine, rejecting empty and duplicate IDs.
func (e *Engine) AddRule(r Rule) error {
	if r == nil {
		return fmt.Errorf("register analyzer rule: nil rule")
	}
	id := strings.TrimSpace(r.ID())
	if id == "" {
		return fmt.Errorf("register analyzer rule: empty rule ID")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.ruleIDs[id]; exists {
		return fmt.Errorf("register analyzer rule %q: duplicate rule ID", id)
	}
	e.rules = append(e.rules, r)
	e.ruleIDs[id] = struct{}{}
	return nil
}

// AddLanguageRules wraps and adds language-specific rules from a LanguageAdapter.
// This is called for each Level 3 adapter.
func (e *Engine) AddLanguageRules(adapter language.LanguageAdapter) error {
	langRules := adapter.LanguageRules()
	if len(langRules) == 0 {
		return nil
	}
	for _, lr := range langRules {
		if err := e.AddRule(WrapLanguageRule(lr)); err != nil {
			return err
		}
	}
	return nil
}

// AddRegistryLanguageRules adds every semantic adapter's rules in stable language order.
func (e *Engine) AddRegistryLanguageRules(registry *language.Registry) error {
	if registry == nil {
		return fmt.Errorf("register language rules: nil registry")
	}
	for _, adapter := range registry.Adapters() {
		if !adapter.Capabilities().Semantic {
			continue
		}
		if err := e.AddLanguageRules(adapter); err != nil {
			return fmt.Errorf("register %s language rules: %w", adapter.Language(), err)
		}
	}
	return nil
}

// Run executes all registered rules against the graph concurrently.
// Independent rules run in parallel; results are merged.
func (e *Engine) Run(g *graph.Graph) []language.PotentialFinding {
	findings, err := e.RunChecked(g)
	if err != nil {
		logger.Error("analyzer rule execution failed", zap.Error(err))
	}
	return findings
}

// RunChecked executes all rules and reports recovered rule panics. CLI
// orchestration uses this method so partial rule execution cannot be recorded
// as successful semantic coverage.
func (e *Engine) RunChecked(g *graph.Graph) ([]language.PotentialFinding, error) {
	e.mu.RLock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID() < rules[j].ID() })
	index := g.SemanticIndex()
	active := rules[:0]
	for _, rule := range rules {
		scoped, ok := rule.(interface{ RequiredEcosystems() []semantic.Ecosystem })
		if !ok || ecosystemsDetected(index, scoped.RequiredEcosystems()) {
			active = append(active, rule)
		}
	}
	rules = active

	if len(rules) == 0 {
		return nil, nil
	}

	type result struct {
		findings []language.PotentialFinding
		err      error
	}

	results := make(chan result, len(rules))
	var wg sync.WaitGroup

	for _, r := range rules {
		wg.Add(1)
		go func(rule Rule) {
			defer wg.Done()

			// Each rule runs independently against the graph.
			// Panics in a rule should not crash the entire engine.
			defer func() {
				if rec := recover(); rec != nil {
					results <- result{err: fmt.Errorf("analyzer rule %s panicked: %v", rule.ID(), rec)}
				}
			}()

			findings := rule.Check(g)
			policy := language.ReviewPolicyContextRequired
			if declared, ok := rule.(ReviewPolicyRule); ok {
				policy = declared.ReviewPolicy()
			}
			for index := range findings {
				if findings[index].ReviewPolicy == "" {
					findings[index].ReviewPolicy = policy
				}
				if findings[index].ReviewPolicy == language.ReviewPolicyContextRequired && (strings.TrimSpace(findings[index].Observation) == "" || strings.TrimSpace(findings[index].Hypothesis) == "") {
					results <- result{err: fmt.Errorf("analyzer rule %s emitted context-required finding without explicit observation and hypothesis", rule.ID())}
					return
				}
			}
			results <- result{findings: findings}
		}(r)
	}

	// Close channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	var allFindings []language.PotentialFinding
	var diagnostics []string
	for res := range results {
		allFindings = append(allFindings, res.findings...)
		if res.err != nil {
			diagnostics = append(diagnostics, res.err.Error())
		}
	}
	sort.SliceStable(allFindings, func(i, j int) bool {
		left, right := allFindings[i], allFindings[j]
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.StartLine != right.StartLine {
			return left.StartLine < right.StartLine
		}
		if left.EndLine != right.EndLine {
			return left.EndLine < right.EndLine
		}
		return left.Title < right.Title
	})

	if len(diagnostics) > 0 {
		sort.Strings(diagnostics)
		return allFindings, fmt.Errorf("partial analyzer execution: %s", strings.Join(diagnostics, "; "))
	}
	return allFindings, nil
}

func ecosystemsDetected(index *semantic.Index, required []semantic.Ecosystem) bool {
	for _, ecosystem := range required {
		if !index.HasEcosystem(ecosystem) {
			return false
		}
	}
	return true
}

// RuleCount returns the number of registered rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}
