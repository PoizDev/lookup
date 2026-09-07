package rust

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
	}{{"positive.rs", []string{"RUST-ASYNC-001", "RUST-CMD-SEC-001", "RUST-FS-SEC-001", "RUST-SQL-SEC-001", "RUST-SSRF-SEC-001", "RUST-TLS-SEC-001"}}, {"negative.rs", nil}, {"near_miss.rs", nil}}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/rust/intelligence", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parseRust(t, string(src))
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
			if !sameRust(got, tc.want) {
				t.Fatalf("IDs=%v want=%v facts=%#v", got, tc.want, doc.Facts)
			}
		})
	}
}
func sameRust(a, b []string) bool {
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
