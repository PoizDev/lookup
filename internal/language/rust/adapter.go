package rust

import (
	"context"
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/astutil"
	"github.com/poizdev/lookup/internal/language/rust/rules"
	rustsemantics "github.com/poizdev/lookup/internal/language/rust/semantics"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
	sitterrust "github.com/smacker/go-tree-sitter/rust"
)

type Adapter struct{}

var _ language.LanguageAdapter = (*Adapter)(nil)
var _ language.SemanticAdapter = (*Adapter)(nil)

func (*Adapter) Language() language.Language { return language.Rust }
func (*Adapter) Capabilities() language.Capabilities {
	return language.Capabilities{Parse: true, Structural: true, Semantic: true}
}
func (*Adapter) Parse(ctx context.Context, src []byte) (*graph.RawAST, error) {
	return astutil.Parse(ctx, "Rust", sitterrust.GetLanguage(), src)
}
func (*Adapter) ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error) {
	return rustsemantics.Extract(ast)
}
func (*Adapter) LanguageRules() []language.AnalysisRule { return rules.All() }
func rustRoot(ast *graph.RawAST) *sitter.Node {
	if ast == nil {
		return nil
	}
	n, _ := ast.Root.(*sitter.Node)
	return n
}

func (*Adapter) ExtractFunctions(ast *graph.RawAST) []graph.Function {
	var out []graph.Function
	for _, n := range astutil.Descendants(rustRoot(ast), "function_item", "function_signature_item") {
		name := astutil.Text(n.ChildByFieldName("name"), ast.Source)
		receiver := ""
		if impl := astutil.Ancestor(n, "impl_item"); impl != nil {
			receiver = strings.TrimSpace(astutil.Text(impl.ChildByFieldName("type"), ast.Source))
		}
		params := rustParams(n.ChildByFieldName("parameters"), ast.Source)
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Function{Name: name, Receiver: receiver, Params: params, File: ast.FilePath, StartLine: s, EndLine: el, Body: astutil.Text(n, ast.Source)})
	}
	return out
}
func (*Adapter) ExtractTypes(ast *graph.RawAST) []graph.TypeDef {
	var out []graph.TypeDef
	for _, n := range astutil.Descendants(rustRoot(ast), "struct_item", "enum_item", "trait_item") {
		kind := strings.TrimSuffix(n.Type(), "_item")
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.TypeDef{Name: astutil.Text(n.ChildByFieldName("name"), ast.Source), Kind: kind, File: ast.FilePath, StartLine: s, EndLine: el})
	}
	return out
}
func (*Adapter) ExtractImports(ast *graph.RawAST) []graph.Import {
	var out []graph.Import
	for _, n := range astutil.Descendants(rustRoot(ast), "use_declaration") {
		text := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(astutil.Text(n, ast.Source)), "use "), ";"))
		path, alias := rustAlias(text)
		line, _, _, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Import{Path: path, Alias: alias, File: ast.FilePath, Line: line})
	}
	for _, n := range astutil.Descendants(rustRoot(ast), "mod_item") {
		if n.ChildByFieldName("body") != nil {
			continue
		}
		line, _, _, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Import{Path: astutil.Text(n.ChildByFieldName("name"), ast.Source), File: ast.FilePath, Line: line})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}
func (*Adapter) ExtractCalls(ast *graph.RawAST) []graph.Call {
	var out []graph.Call
	for _, n := range astutil.Descendants(rustRoot(ast), "call_expression") {
		fn := n.ChildByFieldName("function")
		callee := astutil.Text(fn, ast.Source)
		receiver := ""
		isMethod := false
		if fn != nil && fn.Type() == "field_expression" {
			receiver = astutil.Text(fn.ChildByFieldName("value"), ast.Source)
			callee = astutil.Text(fn.ChildByFieldName("field"), ast.Source)
			isMethod = true
		}
		if !isMethod && rustLocalBindingShadows(n, callee, ast.Source) {
			continue
		}
		caller, callerReceiver := rustEnclosing(n, ast.Source)
		line, _, _, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Call{CallerName: caller, CallerReceiver: callerReceiver, CalleeName: callee, CalleeReceiver: receiver, File: ast.FilePath, Line: line, IsMethod: isMethod})
	}
	return out
}

func rustLocalBindingShadows(call *sitter.Node, name string, src []byte) bool {
	fn := astutil.Ancestor(call, "function_item")
	if fn == nil || name == "" {
		return false
	}
	for _, parameter := range rustParams(fn.ChildByFieldName("parameters"), src) {
		if parameter == name {
			return true
		}
	}
	for _, binding := range astutil.Descendants(fn.ChildByFieldName("body"), "let_declaration") {
		if binding.EndByte() > call.StartByte() {
			continue
		}
		if astutil.Text(binding.ChildByFieldName("pattern"), src) == name {
			return true
		}
	}
	return false
}
func (a *Adapter) ExtractSymbols(ast *graph.RawAST) []graph.Symbol {
	var out []graph.Symbol
	for _, f := range a.ExtractFunctions(ast) {
		out = append(out, graph.Symbol{Name: f.Name, Kind: graph.NodeFunction, File: f.File, StartLine: f.StartLine, EndLine: f.EndLine})
	}
	for _, t := range a.ExtractTypes(ast) {
		out = append(out, graph.Symbol{Name: t.Name, Kind: graph.NodeType, File: t.File, StartLine: t.StartLine, EndLine: t.EndLine})
	}
	for _, n := range astutil.Descendants(rustRoot(ast), "let_declaration", "const_item", "static_item") {
		nameNode := n.ChildByFieldName("pattern")
		if nameNode == nil {
			nameNode = n.ChildByFieldName("name")
		}
		name := astutil.Text(nameNode, ast.Source)
		if name == "" || strings.ContainsAny(name, "(),") {
			continue
		}
		kind := graph.NodeVariable
		if n.Type() == "const_item" {
			kind = graph.NodeConstant
		}
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Symbol{Name: name, Kind: kind, File: ast.FilePath, StartLine: s, EndLine: el})
	}
	return out
}
func rustAlias(v string) (string, string) {
	parts := strings.Split(v, " as ")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(v), ""
}
func rustParams(n *sitter.Node, src []byte) []string {
	var out []string
	if n == nil {
		return out
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		p := n.NamedChild(i)
		pattern := p.ChildByFieldName("pattern")
		if pattern == nil && p.Type() == "self_parameter" {
			out = append(out, "self")
			continue
		}
		name := astutil.Text(pattern, src)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
func rustEnclosing(n *sitter.Node, src []byte) (string, string) {
	fn := astutil.Ancestor(n, "function_item")
	if fn == nil {
		return "", ""
	}
	receiver := ""
	if impl := astutil.Ancestor(fn, "impl_item"); impl != nil {
		receiver = astutil.Text(impl.ChildByFieldName("type"), src)
	}
	return astutil.Text(fn.ChildByFieldName("name"), src), receiver
}
