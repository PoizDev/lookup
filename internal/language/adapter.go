package language

import (
	"context"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
)

type ReviewPolicy = finding.ReviewPolicy

const (
	ReviewPolicyContextRequired     = finding.ReviewPolicyContextRequired
	ReviewPolicyStaticAuthoritative = finding.ReviewPolicyStaticAuthoritative
)

// LanguageAdapter is the interface that language-specific adapters must implement.
type LanguageAdapter interface {
	// Language returns the language this adapter handles.
	Language() Language

	// Capabilities reports stage support independently from execution results.
	Capabilities() Capabilities

	// Parse parses source code into a RawAST (Level 1+)
	Parse(ctx context.Context, src []byte) (*graph.RawAST, error)

	// ExtractSymbols extracts all symbols from parsed AST (Level 2+)
	ExtractSymbols(ast *graph.RawAST) []graph.Symbol

	// ExtractFunctions extracts function declarations (Level 2+)
	ExtractFunctions(ast *graph.RawAST) []graph.Function

	// ExtractTypes extracts type declarations (Level 2+)
	ExtractTypes(ast *graph.RawAST) []graph.TypeDef

	// ExtractImports extracts import statements (Level 2+)
	ExtractImports(ast *graph.RawAST) []graph.Import

	// ExtractCalls extracts function/method calls (Level 2+)
	ExtractCalls(ast *graph.RawAST) []graph.Call

	// LanguageRules returns language-specific analysis rules (Level 3, can return nil)
	LanguageRules() []AnalysisRule
}

// SemanticAdapter is an optional parse-once extension implemented by adapters that
// can normalize their AST into shared semantic facts. It intentionally does not
// expand the 6.1 LanguageAdapter capability contract.
type SemanticAdapter interface {
	ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error)
}

// AnalysisRule is a minimal rule interface to avoid circular dependency with analyzer package.
// The analyzer package will type-assert or wrap these.
type AnalysisRule interface {
	ID() string
	CategoryName() string
	SeverityName() string
	Check(g *graph.Graph) []PotentialFinding
}

// EcosystemRule optionally scopes a rule to imports detected in semantic documents.
type EcosystemRule interface {
	RequiredEcosystems() []semantic.Ecosystem
}

// PotentialFinding is a potential issue found by a rule.
type PotentialFinding struct {
	RuleID          string
	Category        string
	Severity        string
	Title           string
	Description     string
	File            string
	StartLine       int
	EndLine         int
	CodeSnippet     string
	AffectedSymbols []string
	CallChain       []string
	Confidence      float64
	// ConfidenceSpecified distinguishes an intentional zero from the legacy
	// zero value, which historically means the rule did not supply confidence.
	ConfidenceSpecified bool
	EvidenceStrength    string
	EvidenceSteps       []EvidenceStep
	Observation         string
	Hypothesis          string
	ReviewPolicy        ReviewPolicy
}

// EvidenceStep is an ordered, machine-readable explanation of a finding.
type EvidenceStep struct {
	Kind          string
	File          string
	Line          int
	Column        int
	Expression    string
	Message       string
	RelatedSymbol string
}
