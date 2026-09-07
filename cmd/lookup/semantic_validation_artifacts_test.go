package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/analyzer/generic/architecture"
	"github.com/poizdev/lookup/internal/analyzer/generic/security"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/discovery"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

// TestSemanticValidationArtifactExport is an opt-in diagnostic, never a ground-truth oracle. It uses the
// production pipeline and writes only to an explicitly supplied artifact folder.
func TestSemanticValidationArtifactExport(t *testing.T) {
	root, out := os.Getenv("LOOKUP_VALIDATION_REPO"), os.Getenv("LOOKUP_VALIDATION_OUT")
	if root == "" || out == "" {
		t.Skip("set LOOKUP_VALIDATION_REPO and LOOKUP_VALIDATION_OUT")
	}
	walker, err := discovery.NewWalker(root, config.DefaultSkipDirs, config.DefaultMaxFileKB, false)
	if err != nil {
		t.Fatal(err)
	}
	files, stats, err := walker.Walk()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := newLanguageRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder := graph.NewBuilder()
	var warnings []string
	pipeline := processLanguageFiles(context.Background(), files, registry, builder, func(path string, err error) { warnings = append(warnings, path+": "+err.Error()) }, nil)
	g := builder.Build()
	engine, err := analyzer.NewEngineChecked(&security.HardcodedSecret{RepositoryRoot: root}, &architecture.LayerViolation{})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.AddRegistryLanguageRules(registry); err != nil {
		t.Fatal(err)
	}
	potentials, err := engine.RunChecked(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := pipeline.CompleteSemantic(); err != nil {
		t.Fatal(err)
	}
	normalized := normalizePotentials(potentials)
	cb := reviewcontext.NewBuilder(g, reviewcontext.DefaultLimits())
	type packetAudit struct {
		Packet                 reviewcontext.ReviewPacket `json:"packet"`
		RequiredIDs            []string                   `json:"required_ids"`
		CriticalRemovalBlocked bool                       `json:"critical_removal_blocked"`
	}
	packets := make([]packetAudit, 0, len(normalized))
	for _, f := range normalized {
		p := cb.Build(f)
		a := packetAudit{Packet: p, CriticalRemovalBlocked: true}
		for i, e := range p.Evidence {
			if !e.Required {
				continue
			}
			a.RequiredIDs = append(a.RequiredIDs, e.ID)
			altered := p
			altered.Evidence = append(append([]reviewcontext.EvidenceItem(nil), p.Evidence[:i]...), p.Evidence[i+1:]...)
			if reviewcontext.ReevaluateSufficiency(p, altered).Reviewable() {
				a.CriticalRemovalBlocked = false
			}
		}
		packets = append(packets, a)
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, v any) {
		t.Helper()
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(out, name), append(b, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("coverage.json", pipeline.Collector.Coverage())
	write("files.json", files)
	write("discovery.json", stats)
	write("warnings.json", warnings)
	write("packets.json", packets)
	t.Logf("files=%d parsed=%d candidates=%d", len(files), pipeline.ParsedCount, len(packets))
}

// TestContextMinimizationSufficiencyProbe records whether the real sufficiency gate blocks loss
// of source-audited context. It is diagnostic, not a passing regression for the
// unresolved semantic selection defect.
func TestContextMinimizationSufficiencyProbe(t *testing.T) {
	base := os.Getenv("LOOKUP_VALIDATION_ARTIFACTS")
	if base == "" {
		t.Skip("set LOOKUP_VALIDATION_ARTIFACTS")
	}
	data, err := os.ReadFile(filepath.Join(base, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Case  string `json:"case"`
		Audit struct {
			Packet   reviewcontext.ReviewPacket `json:"packet"`
			Required []string                   `json:"required_ids"`
		} `json:"packet_audit"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	type probe struct {
		Case       string                    `json:"case"`
		Removed    []string                  `json:"removed"`
		Before     reviewcontext.Sufficiency `json:"before"`
		After      reviewcontext.Sufficiency `json:"after"`
		Reviewable bool                      `json:"reviewable"`
	}
	var results []probe
	for _, c := range cases {
		if c.Case != "RS07" && c.Case != "RS09" {
			continue
		}
		original := c.Audit.Packet
		for i := range original.Evidence {
			for _, id := range c.Audit.Required {
				if original.Evidence[i].ID == id {
					original.Evidence[i].Required = true
				}
			}
		}
		transformed := original
		transformed.Evidence = nil
		result := probe{Case: c.Case, Before: original.Sufficiency}
		for _, e := range original.Evidence {
			// Strip optional evidence, including the enclosing function and non-local
			// source guards. This is exactly the class of loss minimization permits.
			if !e.Required && e.Kind != reviewcontext.EvidencePrimary {
				result.Removed = append(result.Removed, e.ID)
				continue
			}
			transformed.Evidence = append(transformed.Evidence, e)
		}
		transformed = reviewcontext.ReevaluateSufficiency(original, transformed)
		result.After = transformed.Sufficiency
		result.Reviewable = transformed.Reviewable()
		results = append(results, result)
	}
	data, err = json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "minimization-probe.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
