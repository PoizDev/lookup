package python

import (
	"context"
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/astutil"
	"github.com/poizdev/lookup/internal/language/python/rules"
	pythonsemantics "github.com/poizdev/lookup/internal/language/python/semantics"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
	sitterpython "github.com/smacker/go-tree-sitter/python"
)

type Adapter struct{}

var _ language.LanguageAdapter = (*Adapter)(nil)
var _ language.SemanticAdapter = (*Adapter)(nil)

func (*Adapter) Language() language.Language { return language.Python }
func (*Adapter) Capabilities() language.Capabilities {
	return language.Capabilities{Parse: true, Structural: true, Semantic: true}
}
func (*Adapter) Parse(ctx context.Context, src []byte) (*graph.RawAST, error) {
	return astutil.Parse(ctx, "Python", sitterpython.GetLanguage(), src)
}
func (*Adapter) ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error) {
	return pythonsemantics.Extract(ast)
}
func (*Adapter) LanguageRules() []language.AnalysisRule { return rules.All() }

func root(ast *graph.RawAST) *sitter.Node {
	if ast == nil {
		return nil
	}
	node, _ := ast.Root.(*sitter.Node)
	return node
}

func (*Adapter) ExtractFunctions(ast *graph.RawAST) []graph.Function {
	var result []graph.Function
	for _, node := range astutil.Descendants(root(ast), "function_definition") {
		name := astutil.Text(node.ChildByFieldName("name"), ast.Source)
		receiver := ""
		if class := astutil.Ancestor(node, "class_definition"); class != nil {
			receiver = astutil.Text(class.ChildByFieldName("name"), ast.Source)
		}
		params := parameterNames(node.ChildByFieldName("parameters"), ast.Source)
		start, _, end, _ := astutil.Location(ast.FilePath, node)
		result = append(result, graph.Function{Name: name, Receiver: receiver, Params: params, File: ast.FilePath, StartLine: start, EndLine: end, Body: astutil.Text(node, ast.Source)})
	}
	return result
}

func (*Adapter) ExtractTypes(ast *graph.RawAST) []graph.TypeDef {
	var result []graph.TypeDef
	for _, node := range astutil.Descendants(root(ast), "class_definition") {
		start, _, end, _ := astutil.Location(ast.FilePath, node)
		result = append(result, graph.TypeDef{Name: astutil.Text(node.ChildByFieldName("name"), ast.Source), Kind: "class", File: ast.FilePath, StartLine: start, EndLine: end})
	}
	return result
}

func (*Adapter) ExtractImports(ast *graph.RawAST) []graph.Import {
	var result []graph.Import
	for _, node := range astutil.Descendants(root(ast), "import_statement", "import_from_statement") {
		line, _, _, _ := astutil.Location(ast.FilePath, node)
		if node.Type() == "import_statement" {
			for _, child := range namedChildren(node) {
				text := astutil.Text(child, ast.Source)
				path, alias := splitAlias(text)
				result = append(result, graph.Import{Path: path, Alias: alias, File: ast.FilePath, Line: line})
			}
			continue
		}
		module := astutil.Text(node.ChildByFieldName("module_name"), ast.Source)
		if module == "" {
			module = astutil.Text(node.ChildByFieldName("module"), ast.Source)
		}
		for _, child := range namedChildren(node) {
			if child == node.ChildByFieldName("module_name") || child == node.ChildByFieldName("module") {
				continue
			}
			if child.Type() == "dotted_name" || child.Type() == "aliased_import" {
				name, alias := splitAlias(astutil.Text(child, ast.Source))
				result = append(result, graph.Import{Path: module + "." + name, Alias: alias, File: ast.FilePath, Line: line})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Path < result[j].Path
	})
	return result
}

func (a *Adapter) ExtractCalls(ast *graph.RawAST) []graph.Call {
	var result []graph.Call
	for _, node := range astutil.Descendants(root(ast), "call") {
		fn := node.ChildByFieldName("function")
		callee := astutil.Text(fn, ast.Source)
		receiver := ""
		isMethod := false
		if fn != nil && fn.Type() == "attribute" {
			receiver = astutil.Text(fn.ChildByFieldName("object"), ast.Source)
			callee = astutil.Text(fn.ChildByFieldName("attribute"), ast.Source)
			isMethod = true
		}
		caller, callerReceiver := enclosingFunction(node, ast.Source)
		line, _, _, _ := astutil.Location(ast.FilePath, node)
		result = append(result, graph.Call{CallerName: caller, CallerReceiver: callerReceiver, CalleeName: callee, CalleeReceiver: receiver, File: ast.FilePath, Line: line, IsMethod: isMethod})
	}
	return result
}

func (a *Adapter) ExtractSymbols(ast *graph.RawAST) []graph.Symbol {
	var result []graph.Symbol
	for _, fn := range a.ExtractFunctions(ast) {
		result = append(result, graph.Symbol{Name: fn.Name, Kind: graph.NodeFunction, File: fn.File, StartLine: fn.StartLine, EndLine: fn.EndLine})
	}
	for _, typ := range a.ExtractTypes(ast) {
		result = append(result, graph.Symbol{Name: typ.Name, Kind: graph.NodeType, File: typ.File, StartLine: typ.StartLine, EndLine: typ.EndLine})
	}
	for _, node := range astutil.Descendants(root(ast), "assignment") {
		if astutil.Ancestor(node, "function_definition", "class_definition") != nil {
			continue
		}
		left := node.ChildByFieldName("left")
		if left != nil && left.Type() == "identifier" {
			line, _, end, _ := astutil.Location(ast.FilePath, node)
			result = append(result, graph.Symbol{Name: astutil.Text(left, ast.Source), Kind: graph.NodeVariable, File: ast.FilePath, StartLine: line, EndLine: end, Metadata: map[string]any{"Value": astutil.Text(node.ChildByFieldName("right"), ast.Source)}})
		}
	}
	return result
}

func namedChildren(node *sitter.Node) []*sitter.Node {
	var result []*sitter.Node
	if node == nil {
		return result
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		result = append(result, node.NamedChild(i))
	}
	return result
}
func splitAlias(value string) (string, string) {
	parts := strings.Split(value, " as ")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(value), ""
}
func parameterNames(node *sitter.Node, src []byte) []string {
	var out []string
	for _, child := range astutil.Descendants(node, "identifier") {
		if astutil.Ancestor(child, "lambda") != nil {
			continue
		}
		out = append(out, astutil.Text(child, src))
	}
	return out
}
func enclosingFunction(node *sitter.Node, src []byte) (string, string) {
	fn := astutil.Ancestor(node, "function_definition")
	if fn == nil {
		return "", ""
	}
	receiver := ""
	if class := astutil.Ancestor(fn, "class_definition"); class != nil {
		receiver = astutil.Text(class.ChildByFieldName("name"), src)
	}
	return astutil.Text(fn.ChildByFieldName("name"), src), receiver
}
