package csharp

import (
	"context"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/astutil"
	"github.com/poizdev/lookup/internal/language/csharp/rules"
	csharpsemantics "github.com/poizdev/lookup/internal/language/csharp/semantics"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
	sittercsharp "github.com/smacker/go-tree-sitter/csharp"
	"sort"
	"strings"
)

type Adapter struct{}

var _ language.LanguageAdapter = (*Adapter)(nil)
var _ language.SemanticAdapter = (*Adapter)(nil)

func (*Adapter) Language() language.Language { return language.CSharp }
func (*Adapter) Capabilities() language.Capabilities {
	return language.Capabilities{Parse: true, Structural: true, Semantic: true}
}
func (*Adapter) Parse(ctx context.Context, src []byte) (*graph.RawAST, error) {
	return astutil.Parse(ctx, "C#", sittercsharp.GetLanguage(), src)
}
func (*Adapter) ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error) {
	return csharpsemantics.Extract(ast)
}
func (*Adapter) LanguageRules() []language.AnalysisRule { return rules.All() }
func csRoot(ast *graph.RawAST) *sitter.Node {
	if ast == nil {
		return nil
	}
	n, _ := ast.Root.(*sitter.Node)
	return n
}
func (*Adapter) ExtractFunctions(ast *graph.RawAST) []graph.Function {
	var out []graph.Function
	for _, n := range astutil.Descendants(csRoot(ast), "method_declaration", "constructor_declaration") {
		name := astutil.Text(n.ChildByFieldName("name"), ast.Source)
		receiver := ""
		if typ := astutil.Ancestor(n, "class_declaration", "struct_declaration", "interface_declaration"); typ != nil {
			receiver = astutil.Text(typ.ChildByFieldName("name"), ast.Source)
		}
		params := csParams(n.ChildByFieldName("parameters"), ast.Source)
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		returns := []string{}
		if typ := n.ChildByFieldName("type"); typ != nil {
			returns = []string{astutil.Text(typ, ast.Source)}
		}
		out = append(out, graph.Function{Name: name, Receiver: receiver, Params: params, Returns: returns, File: ast.FilePath, StartLine: s, EndLine: el, Body: astutil.Text(n, ast.Source)})
	}
	return out
}
func (*Adapter) ExtractTypes(ast *graph.RawAST) []graph.TypeDef {
	var out []graph.TypeDef
	for _, n := range astutil.Descendants(csRoot(ast), "class_declaration", "struct_declaration", "interface_declaration") {
		kind := strings.TrimSuffix(n.Type(), "_declaration")
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		var fields []string
		for _, p := range astutil.Descendants(n, "property_declaration") {
			fields = append(fields, astutil.Text(p.ChildByFieldName("name"), ast.Source))
		}
		out = append(out, graph.TypeDef{Name: astutil.Text(n.ChildByFieldName("name"), ast.Source), Kind: kind, Fields: fields, File: ast.FilePath, StartLine: s, EndLine: el})
	}
	return out
}
func (*Adapter) ExtractImports(ast *graph.RawAST) []graph.Import {
	var out []graph.Import
	for _, n := range astutil.Descendants(csRoot(ast), "using_directive") {
		text := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(astutil.Text(n, ast.Source)), "using "), ";")
		path, alias := text, ""
		if p := strings.Split(text, " = "); len(p) == 2 {
			alias, path = strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
		}
		line, _, _, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Import{Path: path, Alias: alias, File: ast.FilePath, Line: line})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}
func (*Adapter) ExtractCalls(ast *graph.RawAST) []graph.Call {
	var out []graph.Call
	for _, n := range astutil.Descendants(csRoot(ast), "invocation_expression") {
		fn := n.ChildByFieldName("function")
		callee := astutil.Text(fn, ast.Source)
		receiver := ""
		method := false
		if fn != nil && fn.Type() == "member_access_expression" {
			receiver = astutil.Text(fn.ChildByFieldName("expression"), ast.Source)
			callee = astutil.Text(fn.ChildByFieldName("name"), ast.Source)
			method = true
		}
		caller, callerReceiver := csEnclosing(n, ast.Source)
		line, _, _, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Call{CallerName: caller, CallerReceiver: callerReceiver, CalleeName: callee, CalleeReceiver: receiver, File: ast.FilePath, Line: line, IsMethod: method})
	}
	return out
}
func (a *Adapter) ExtractSymbols(ast *graph.RawAST) []graph.Symbol {
	var out []graph.Symbol
	for _, f := range a.ExtractFunctions(ast) {
		out = append(out, graph.Symbol{Name: f.Name, Kind: graph.NodeFunction, File: f.File, StartLine: f.StartLine, EndLine: f.EndLine})
	}
	for _, t := range a.ExtractTypes(ast) {
		out = append(out, graph.Symbol{Name: t.Name, Kind: graph.NodeType, File: t.File, StartLine: t.StartLine, EndLine: t.EndLine})
	}
	for _, n := range astutil.Descendants(csRoot(ast), "property_declaration", "variable_declarator") {
		name := astutil.Text(n.ChildByFieldName("name"), ast.Source)
		if name == "" {
			continue
		}
		s, _, el, _ := astutil.Location(ast.FilePath, n)
		out = append(out, graph.Symbol{Name: name, Kind: graph.NodeVariable, File: ast.FilePath, StartLine: s, EndLine: el})
	}
	return out
}
func csParams(n *sitter.Node, src []byte) []string {
	var out []string
	for _, p := range astutil.Descendants(n, "parameter") {
		if name := astutil.Text(p.ChildByFieldName("name"), src); name != "" {
			out = append(out, name)
		}
	}
	return out
}
func csEnclosing(n *sitter.Node, src []byte) (string, string) {
	fn := astutil.Ancestor(n, "method_declaration", "constructor_declaration")
	if fn == nil {
		return "", ""
	}
	receiver := ""
	if typ := astutil.Ancestor(fn, "class_declaration", "struct_declaration", "interface_declaration"); typ != nil {
		receiver = astutil.Text(typ.ChildByFieldName("name"), src)
	}
	return astutil.Text(fn.ChildByFieldName("name"), src), receiver
}
