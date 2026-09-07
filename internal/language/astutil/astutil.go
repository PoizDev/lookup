// Package astutil contains small language-neutral Tree-sitter helpers.
package astutil

import (
	"context"
	"fmt"

	"github.com/poizdev/lookup/internal/graph"
	sitter "github.com/smacker/go-tree-sitter"
)

func Parse(ctx context.Context, languageName string, grammar *sitter.Language, src []byte) (*graph.RawAST, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parser := sitter.NewParser()
	parser.SetLanguage(grammar)
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		if tree != nil {
			tree.Close()
		}
		parser.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("parse %s source: %w", languageName, err)
	}
	root := tree.RootNode()
	ast := graph.NewRawAST(languageName, root, append([]byte(nil), src...), func() error { tree.Close(); parser.Close(); return nil })
	ast.HasErrors = HasErrors(root)
	return ast, nil
}

func HasErrors(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.Type() == "ERROR" || node.IsMissing() {
		return true
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if HasErrors(node.Child(i)) {
			return true
		}
	}
	return false
}

func Descendants(node *sitter.Node, kinds ...string) []*sitter.Node {
	wanted := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		wanted[kind] = true
	}
	var result []*sitter.Node
	var walk func(*sitter.Node)
	walk = func(current *sitter.Node) {
		if current == nil {
			return
		}
		if wanted[current.Type()] {
			result = append(result, current)
		}
		for i := 0; i < int(current.ChildCount()); i++ {
			walk(current.Child(i))
		}
	}
	walk(node)
	return result
}

func Text(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start, end := node.StartByte(), node.EndByte()
	if end < start || int(end) > len(src) {
		return ""
	}
	return string(src[start:end])
}

func Location(file string, node *sitter.Node) (int, int, int, int) {
	if node == nil {
		return 0, 0, 0, 0
	}
	return int(node.StartPoint().Row) + 1, int(node.StartPoint().Column) + 1, int(node.EndPoint().Row) + 1, int(node.EndPoint().Column) + 1
}

func Ancestor(node *sitter.Node, kinds ...string) *sitter.Node {
	wanted := map[string]bool{}
	for _, kind := range kinds {
		wanted[kind] = true
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		if wanted[current.Type()] {
			return current
		}
	}
	return nil
}
