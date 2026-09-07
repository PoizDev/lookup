package python

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
)

func TestProductionIntelligenceFixtures(t *testing.T) {
	cases := []struct {
		file string
		want []string
	}{
		{"positive.py", []string{"PY-CMD-SEC-001", "PY-CV-COR-001", "PY-CV-RES-001", "PY-DESER-SEC-001", "PY-FS-SEC-001", "PY-ML-SEC-001", "PY-SQL-SEC-001", "PY-TLS-SEC-001"}},
		{"negative.py", nil}, {"near_miss.py", nil},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/python/intelligence", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parsePython(t, string(src))
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
			if !equalStrings(got, tc.want) {
				t.Fatalf("rule IDs=%v, want %v\nfacts=%#v", got, tc.want, doc.Facts)
			}
		})
	}
}

func TestAIAndWebEcosystemFacts(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("../../../testdata/python/intelligence/positive.py"))
	if err != nil {
		t.Fatal(err)
	}
	a, ast := parsePython(t, string(src))
	doc, err := a.ExtractSemantic(ast)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]bool{}
	for _, f := range doc.Facts {
		ops[f.Operation] = true
	}
	for _, op := range []string{"http.query", "python.torch.load.unsafe", "python.opencv.imread", "python.ultralytics.model"} {
		if !ops[op] {
			t.Errorf("missing operation %s", op)
		}
	}
}
func equalStrings(a, b []string) bool {
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
