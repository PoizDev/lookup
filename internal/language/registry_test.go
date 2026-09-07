package language

import (
	"context"
	"reflect"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
)

type fakeAdapter struct {
	language     Language
	capabilities Capabilities
}

func (a *fakeAdapter) Language() Language         { return a.language }
func (a *fakeAdapter) Capabilities() Capabilities { return a.capabilities }
func (a *fakeAdapter) Parse(context.Context, []byte) (*graph.RawAST, error) {
	return &graph.RawAST{}, nil
}
func (a *fakeAdapter) ExtractSymbols(*graph.RawAST) []graph.Symbol     { return nil }
func (a *fakeAdapter) ExtractFunctions(*graph.RawAST) []graph.Function { return nil }
func (a *fakeAdapter) ExtractTypes(*graph.RawAST) []graph.TypeDef      { return nil }
func (a *fakeAdapter) ExtractImports(*graph.RawAST) []graph.Import     { return nil }
func (a *fakeAdapter) ExtractCalls(*graph.RawAST) []graph.Call         { return nil }
func (a *fakeAdapter) LanguageRules() []AnalysisRule                   { return nil }

type fakeSemanticAdapter struct{ *fakeAdapter }

func (*fakeSemanticAdapter) ExtractSemantic(*graph.RawAST) (*semantic.Document, error) {
	return &semantic.Document{}, nil
}

func TestCapabilitiesValidateDependencyChain(t *testing.T) {
	tests := []struct {
		name  string
		caps  Capabilities
		want  SupportLevel
		valid bool
	}{
		{name: "unsupported", caps: Capabilities{}, want: LevelUnsupported, valid: true},
		{name: "parse", caps: Capabilities{Parse: true}, want: LevelParse, valid: true},
		{name: "structural", caps: Capabilities{Parse: true, Structural: true}, want: LevelStructural, valid: true},
		{name: "semantic", caps: Capabilities{Parse: true, Structural: true, Semantic: true}, want: LevelSemantic, valid: true},
		{name: "structural without parse", caps: Capabilities{Structural: true}, valid: false},
		{name: "semantic without structural", caps: Capabilities{Parse: true, Semantic: true}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.caps.Validate()
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("Validate() error = nil")
			}
			if test.valid && test.caps.SupportLevel() != test.want {
				t.Fatalf("SupportLevel() = %v, want %v", test.caps.SupportLevel(), test.want)
			}
		})
	}
}

func TestRegistryRejectsSemanticCapabilityWithoutSemanticContract(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(&fakeAdapter{language: Go, capabilities: Capabilities{Parse: true, Structural: true, Semantic: true}})
	if err == nil {
		t.Fatal("semantic capability without SemanticAdapter was accepted")
	}
}

func TestRegistryAdaptersAreSortedByLanguage(t *testing.T) {
	registry := NewRegistry()
	for _, lang := range []Language{Python, Go, JavaScript} {
		if err := registry.Register(&fakeAdapter{language: lang, capabilities: Capabilities{Parse: true}}); err != nil {
			t.Fatal(err)
		}
	}
	got := registry.Adapters()
	want := []Language{Go, JavaScript, Python}
	for i, adapter := range got {
		if adapter.Language() != want[i] {
			t.Fatalf("adapter %d language = %s, want %s", i, adapter.Language(), want[i])
		}
	}
}

func TestRegistryRejectsInvalidRegistrationsAndSortsLanguages(t *testing.T) {
	registry := NewRegistry()
	var typedNil *fakeAdapter
	for _, adapter := range []LanguageAdapter{nil, typedNil, &fakeAdapter{language: Python, capabilities: Capabilities{Structural: true}}} {
		if err := registry.Register(adapter); err == nil {
			t.Fatalf("Register(%#v) error = nil", adapter)
		}
	}

	for _, lang := range []Language{Python, Go, JavaScript} {
		if err := registry.Register(&fakeAdapter{language: lang, capabilities: Capabilities{Parse: true}}); err != nil {
			t.Fatalf("Register(%s) error = %v", lang, err)
		}
	}
	if err := registry.Register(&fakeAdapter{language: Go, capabilities: Capabilities{Parse: true}}); err == nil {
		t.Fatal("duplicate Register() error = nil")
	}

	want := []Language{Go, JavaScript, Python}
	if got := registry.Languages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
	if registry.Get(Go) == nil || registry.GetByName("Go") == nil || !registry.Has(Go) {
		t.Fatal("registered Go adapter is not available through registry lookups")
	}
}
