package language

import (
	"reflect"
	"testing"
)

func TestCoverageKeepsUnsupportedFilesSeparateFromFailures(t *testing.T) {
	collector := NewCoverageCollector()
	collector.Discover("Python", Capabilities{})

	got := collector.Coverage()
	want := []LangCoverage{{Language: "Python", Files: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Coverage() = %#v, want %#v", got, want)
	}
}

func TestCoverageAggregatesActualAttemptsAndSuccesses(t *testing.T) {
	collector := NewCoverageCollector()
	caps := Capabilities{Parse: true, Structural: true, Semantic: true}
	first := collector.Discover("Go", caps)
	second := collector.Discover("Go", caps)

	for _, stage := range []Stage{StageParse, StageStructural, StageSemantic} {
		if err := collector.Attempt(first, stage); err != nil {
			t.Fatal(err)
		}
		if err := collector.Succeed(first, stage); err != nil {
			t.Fatal(err)
		}
	}
	if err := collector.Attempt(second, StageParse); err != nil {
		t.Fatal(err)
	}

	want := []LangCoverage{{
		Language: "Go", Files: 2,
		Parse:      StageCoverage{Supported: 2, Attempted: 2, Succeeded: 1},
		Structural: StageCoverage{Supported: 2, Attempted: 1, Succeeded: 1},
		Semantic:   StageCoverage{Supported: 2, Attempted: 1, Succeeded: 1},
	}}
	if got := collector.Coverage(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Coverage() = %#v, want %#v", got, want)
	}
}

func TestCoverageRejectsImpossibleStageTransitions(t *testing.T) {
	collector := NewCoverageCollector()
	unsupported := collector.Discover("Python", Capabilities{})
	goFile := collector.Discover("Go", Capabilities{Parse: true, Structural: true, Semantic: true})

	checks := []struct {
		name string
		call func() error
	}{
		{name: "attempt unsupported", call: func() error { return collector.Attempt(unsupported, StageParse) }},
		{name: "succeed before attempt", call: func() error { return collector.Succeed(goFile, StageParse) }},
		{name: "structural before parse success", call: func() error { return collector.Attempt(goFile, StageStructural) }},
		{name: "semantic before structural success", call: func() error { return collector.Attempt(goFile, StageSemantic) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("transition error = nil")
			}
		})
	}
}

func TestCoverageSortsLanguagesAndReturnsSnapshot(t *testing.T) {
	collector := NewCoverageCollector()
	collector.Discover("Python", Capabilities{})
	collector.Discover("Go", Capabilities{Parse: true})
	first := collector.Coverage()
	if got, want := []string{first[0].Language, first[1].Language}, []string{"Go", "Python"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("language order = %v, want %v", got, want)
	}
	first[0].Files = 99
	if got := collector.Coverage()[0].Files; got != 1 {
		t.Fatalf("collector state mutated through snapshot: Files = %d", got)
	}
}
