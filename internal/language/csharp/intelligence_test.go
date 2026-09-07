package csharp

import (
	"github.com/poizdev/lookup/internal/graph"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestProductionIntelligenceFixtures(t *testing.T) {
	cases := []struct {
		file string
		want []string
	}{{"positive.cs", []string{"CSHARP-ASYNC-001", "CSHARP-CMD-SEC-001", "CSHARP-FS-SEC-001", "CSHARP-SEC-001", "CSHARP-SSRF-SEC-001", "CSHARP-TLS-SEC-001"}}, {"negative.cs", nil}, {"near_miss.cs", nil}}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/csharp/intelligence", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parseCSharp(t, string(src))
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			g := graph.NewGraph()
			g.AddDocument(doc)
			var got []string
			for _, rule := range a.LanguageRules() {
				for range rule.Check(g) {
					got = append(got, rule.ID())
				}
			}
			sort.Strings(got)
			if !same(got, tc.want) {
				t.Fatalf("IDs=%v want=%v facts=%#v", got, tc.want, doc.Facts)
			}
		})
	}
}
func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
