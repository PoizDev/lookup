package semantics

import (
	"fmt"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language/astutil"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
	"sort"
	"strings"
)

type extractor struct {
	ast  *graph.RawAST
	root *sitter.Node
	doc  *semantic.Document
}

func Extract(ast *graph.RawAST) (*semantic.Document, error) {
	if ast == nil {
		return nil, fmt.Errorf("extract Rust semantics: nil AST")
	}
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return nil, fmt.Errorf("extract Rust semantics: unsupported AST root %T", ast.Root)
	}
	e := &extractor{ast: ast, root: root, doc: &semantic.Document{Path: ast.FilePath, Language: ast.Language, Source: append([]byte(nil), ast.Source...)}}
	e.extractImports()
	for _, n := range astutil.Descendants(root, "function_item", "function_signature_item") {
		e.function(n)
	}
	e.walk(root)
	sort.SliceStable(e.doc.Facts, func(i, j int) bool {
		a, b := e.doc.Facts[i], e.doc.Facts[j]
		if a.Location.StartLine != b.Location.StartLine {
			return a.Location.StartLine < b.Location.StartLine
		}
		if a.Location.StartColumn != b.Location.StartColumn {
			return a.Location.StartColumn < b.Location.StartColumn
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Operation < b.Operation
	})
	return e.doc, nil
}
func (e *extractor) extractImports() {
	for _, n := range astutil.Descendants(e.root, "use_declaration") {
		v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(astutil.Text(n, e.ast.Source)), "use "), ";")
		if p := strings.Split(v, " as "); len(p) > 0 {
			e.doc.Imports = append(e.doc.Imports, strings.TrimSpace(p[0]))
		}
	}
	for _, n := range astutil.Descendants(e.root, "mod_item") {
		if n.ChildByFieldName("body") == nil {
			e.doc.Imports = append(e.doc.Imports, astutil.Text(n.ChildByFieldName("name"), e.ast.Source))
		}
	}
	sort.Strings(e.doc.Imports)
}
func (e *extractor) function(n *sitter.Node) {
	name := astutil.Text(n.ChildByFieldName("name"), e.ast.Source)
	if impl := astutil.Ancestor(n, "impl_item"); impl != nil {
		name = astutil.Text(impl.ChildByFieldName("type"), e.ast.Source) + "." + name
	}
	fn := semantic.Function{Name: name, Location: location(e.ast.FilePath, n)}
	params := n.ChildByFieldName("parameters")
	if params != nil {
		for i := 0; i < int(params.NamedChildCount()); i++ {
			p := params.NamedChild(i)
			pattern := p.ChildByFieldName("pattern")
			if p.Type() == "self_parameter" {
				fn.Parameters = append(fn.Parameters, "self")
			} else if value := astutil.Text(pattern, e.ast.Source); value != "" {
				fn.Parameters = append(fn.Parameters, value)
			}
			text := astutil.Text(p, e.ast.Source)
			sourceOp := ""
			switch {
			case e.hasImport("axum") && (strings.Contains(text, "Query<") || strings.Contains(text, "Query(")):
				sourceOp = "http.query"
			case e.hasImport("axum") && strings.Contains(text, "Path<"):
				sourceOp = "http.path"
			case e.hasImport("axum") && (strings.Contains(text, "Json<") || strings.Contains(text, "Form<")):
				sourceOp = "http.body"
			case e.hasImport("actix_web") && (strings.Contains(text, "web::Query") || strings.Contains(text, "web::Path") || strings.Contains(text, "web::Form") || strings.Contains(text, "web::Json")):
				sourceOp = "http.request"
			case e.hasImport("tonic") && strings.Contains(text, "Request<"):
				sourceOp = "rpc.request"
			}
			if sourceOp != "" {
				outputs := lowercaseIDs(p, e.ast.Source)
				if len(outputs) > 0 {
					e.add(p, semantic.FactHTTPInput, sourceOp, nil, outputs)
					e.add(p, semantic.FactSource, sourceOp, nil, outputs)
				}
			}
		}
	}
	e.doc.Functions = append(e.doc.Functions, fn)
	body := n.ChildByFieldName("body")
	if body != nil && len(astutil.Descendants(body, "return_expression")) == 0 && body.NamedChildCount() > 0 {
		tail := body.NamedChild(int(body.NamedChildCount()) - 1)
		if tail.Type() != "let_declaration" && tail.Type() != "empty_statement" {
			e.add(tail, semantic.FactReturn, "return", ids(tail, e.ast.Source), nil)
		}
	}
}
func (e *extractor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "trait_item":
		e.add(n, semantic.FactControl, "trait", nil, nil)
	case "unsafe_block":
		e.add(n, semantic.FactControl, "rust.unsafe_block", nil, nil)
	case "let_declaration":
		e.add(n, semantic.FactPropagation, "binding", ids(n.ChildByFieldName("value"), e.ast.Source), ids(n.ChildByFieldName("pattern"), e.ast.Source))
	case "assignment_expression":
		e.add(n, semantic.FactPropagation, "assignment", ids(n.ChildByFieldName("right"), e.ast.Source), ids(n.ChildByFieldName("left"), e.ast.Source))
	case "call_expression":
		e.call(n)
	case "return_expression":
		e.add(n, semantic.FactReturn, "return", ids(n, e.ast.Source), nil)
	case "if_expression", "for_expression", "while_expression", "match_expression", "loop_expression":
		e.add(n, semantic.FactControl, strings.TrimSuffix(n.Type(), "_expression"), nil, nil)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		e.walk(n.Child(i))
	}
}
func (e *extractor) call(n *sitter.Node) {
	fn := n.ChildByFieldName("function")
	op := astutil.Text(fn, e.ast.Source)
	args := n.ChildByFieldName("arguments")
	f := e.fact(n, semantic.FactCall, op)
	if args != nil {
		for i := 0; i < int(args.NamedChildCount()); i++ {
			arg := args.NamedChild(i)
			f.Arguments = append(f.Arguments, astutil.Text(arg, e.ast.Source))
			inputs := ids(arg, e.ast.Source)
			f.ArgumentInputs = append(f.ArgumentInputs, inputs)
			f.Inputs = append(f.Inputs, inputs...)
		}
	}
	e.classifyCall(&f, n)
	e.doc.Facts = append(e.doc.Facts, f)
}
func (e *extractor) classifyCall(f *semantic.Fact, n *sitter.Node) {
	expr := f.Expression
	switch {
	case e.hasImport("tokio") && strings.HasPrefix(expr, "std::thread::sleep(") && isAsync(n, e.ast.Source) && !insideSpawnBlocking(n, e.ast.Source):
		f.Kind = semantic.FactGuard
		f.Operation = "rust.async.blocking_sleep"
	case e.hasImport("sqlx") && (f.Operation == "sqlx::query" || f.Operation == "sqlx::query_as" || f.Operation == "sqlx::query_scalar") && len(f.Arguments) > 0 && !rustLiteral(f.Arguments[0]):
		f.Kind = semantic.FactSink
		f.Operation = "rust.sql.dynamic"
	case e.hasImport("diesel") && strings.HasPrefix(expr, "diesel::sql_query(") && len(f.Arguments) > 0 && !rustLiteral(f.Arguments[0]):
		f.Kind = semantic.FactSink
		f.Operation = "rust.sql.dynamic"
	case strings.Contains(expr, "std::process::Command::new(") && (strings.Contains(expr, "\"sh\"") || strings.Contains(expr, "\"bash\"")) && strings.Contains(expr, ".arg(\"-c\")") && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "rust.shell.command"
	case strings.HasPrefix(expr, "std::fs::") && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "rust.filesystem.path"
	case e.hasImport("reqwest") && (strings.HasPrefix(expr, "reqwest::get(") || strings.Contains(expr, "Client::get(")) && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "rust.http.outbound_url"
	case e.hasImport("reqwest") && directRustCall(n, e.ast.Source, "danger_accept_invalid_certs", "true"):
		f.Kind = semantic.FactGuard
		f.Operation = "rust.tls.invalid_certs"
	}
}

func directRustCall(n *sitter.Node, src []byte, callee, argument string) bool {
	if n == nil || n.Type() != "call_expression" {
		return false
	}
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	name := astutil.Text(fn, src)
	if fn.Type() == "field_expression" {
		name = astutil.Text(fn.ChildByFieldName("field"), src)
	}
	if name != callee {
		return false
	}
	args := n.ChildByFieldName("arguments")
	return args != nil && args.NamedChildCount() == 1 && strings.TrimSpace(astutil.Text(args.NamedChild(0), src)) == argument
}
func (e *extractor) hasImport(prefix string) bool {
	for _, v := range e.doc.Imports {
		if v == prefix || strings.HasPrefix(v, prefix+"::") || strings.HasPrefix(v, prefix+"{") {
			return true
		}
	}
	return false
}
func lowercaseIDs(n *sitter.Node, src []byte) []string {
	var out []string
	for _, v := range ids(n, src) {
		if v != "" && v[0] >= 'a' && v[0] <= 'z' {
			out = append(out, v)
		}
	}
	return out
}
func rustLiteral(v string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(v, "&")), "\"")
}
func isAsync(n *sitter.Node, src []byte) bool {
	if fn := astutil.Ancestor(n, "function_item"); fn != nil {
		return strings.HasPrefix(strings.TrimSpace(astutil.Text(fn, src)), "async ")
	}
	return false
}
func insideSpawnBlocking(n *sitter.Node, src []byte) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "function_item" {
			break
		}
		if p.Type() == "call_expression" && strings.Contains(astutil.Text(p, src), "spawn_blocking") {
			return true
		}
	}
	return false
}
func (e *extractor) add(n *sitter.Node, k semantic.FactKind, op string, in, out []string) {
	f := e.fact(n, k, op)
	f.Inputs = in
	f.Outputs = out
	e.doc.Facts = append(e.doc.Facts, f)
}
func (e *extractor) fact(n *sitter.Node, k semantic.FactKind, op string) semantic.Fact {
	f := semantic.NewFact(k, op, location(e.ast.FilePath, n))
	f.Expression = astutil.Text(n, e.ast.Source)
	if fn := astutil.Ancestor(n, "function_item"); fn != nil {
		f.Function = astutil.Text(fn.ChildByFieldName("name"), e.ast.Source)
		if impl := astutil.Ancestor(fn, "impl_item"); impl != nil {
			f.Receiver = astutil.Text(impl.ChildByFieldName("type"), e.ast.Source)
			f.Function = f.Receiver + "." + f.Function
		}
	}
	return f
}
func location(file string, n *sitter.Node) semantic.Location {
	s, c, el, ec := astutil.Location(file, n)
	return semantic.Location{File: file, StartLine: s, StartColumn: c, EndLine: el, EndColumn: ec}
}
func ids(n *sitter.Node, src []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range astutil.Descendants(n, "identifier", "self") {
		v := astutil.Text(id, src)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
