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
	ast      *graph.RawAST
	root     *sitter.Node
	doc      *semantic.Document
	dynamic  map[string]map[string]bool
	db       map[string]map[string]bool
	external map[string]map[string]bool
}

func Extract(ast *graph.RawAST) (*semantic.Document, error) {
	if ast == nil {
		return nil, fmt.Errorf("extract C# semantics: nil AST")
	}
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return nil, fmt.Errorf("extract C# semantics: unsupported AST root %T", ast.Root)
	}
	e := &extractor{ast: ast, root: root, dynamic: map[string]map[string]bool{}, db: map[string]map[string]bool{}, external: map[string]map[string]bool{}, doc: &semantic.Document{Path: ast.FilePath, Language: ast.Language, Source: append([]byte(nil), ast.Source...)}}
	// Top-level statements and initializers have no enclosing method. Keep the
	// existing empty-symbol scope writable just like registered method scopes.
	e.dynamic[""] = map[string]bool{}
	e.external[""] = map[string]bool{}
	e.imports()
	for _, n := range astutil.Descendants(root, "method_declaration", "constructor_declaration") {
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
func (e *extractor) imports() {
	for _, n := range astutil.Descendants(e.root, "using_directive") {
		v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(astutil.Text(n, e.ast.Source)), "using "), ";")
		if p := strings.Split(v, " = "); len(p) == 2 {
			v = strings.TrimSpace(p[1])
		}
		e.doc.Imports = append(e.doc.Imports, v)
	}
	sort.Strings(e.doc.Imports)
}
func (e *extractor) function(n *sitter.Node) {
	name := symbol(n, e.ast.Source)
	fn := semantic.Function{Name: name, Location: location(e.ast.FilePath, n)}
	if e.dynamic[name] == nil {
		e.dynamic[name] = map[string]bool{}
	}
	if e.db[name] == nil {
		e.db[name] = map[string]bool{}
	}
	if e.external[name] == nil {
		e.external[name] = map[string]bool{}
	}
	parameters := astutil.Descendants(n.ChildByFieldName("parameters"), "parameter")
	eventHandler := len(parameters) == 2
	for index, p := range parameters {
		param := astutil.Text(p.ChildByFieldName("name"), e.ast.Source)
		if param != "" {
			fn.Parameters = append(fn.Parameters, param)
		}
		typ := astutil.Text(p.ChildByFieldName("type"), e.ast.Source)
		if index == 0 && typ != "object" {
			eventHandler = false
		}
		if index == 1 && !strings.HasSuffix(typ, "EventArgs") {
			eventHandler = false
		}
		if strings.HasSuffix(typ, "DbContext") {
			e.db[name][param] = true
		}
		if e.hasImport("Microsoft.AspNetCore") {
			text, operation := astutil.Text(p, e.ast.Source), ""
			switch {
			case strings.Contains(text, "FromQuery"):
				operation = "http.query"
			case strings.Contains(text, "FromRoute"):
				operation = "http.path"
			case strings.Contains(text, "FromBody"):
				operation = "http.body"
			case strings.Contains(text, "FromForm"):
				operation = "http.form"
			case strings.Contains(text, "FromHeader"):
				operation = "http.header"
			}
			if operation != "" && param != "" {
				e.external[name][param] = true
				e.add(p, semantic.FactHTTPInput, operation, nil, []string{param})
				e.add(p, semantic.FactSource, operation, nil, []string{param})
			}
		}
	}
	e.doc.Functions = append(e.doc.Functions, fn)
	async := false
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child.Type() == "modifier" && astutil.Text(child, e.ast.Source) == "async" {
			async = true
		}
	}
	if async {
		ret := astutil.Text(n.ChildByFieldName("returns"), e.ast.Source)
		if ret == "void" && !eventHandler {
			e.add(n, semantic.FactGuard, "csharp.async_void", nil, nil)
		} else {
			e.add(n, semantic.FactControl, "csharp.async_task", nil, nil)
		}
	}
}
func (e *extractor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "variable_declarator":
		left := n.ChildByFieldName("name")
		right := n.ChildByFieldName("value")
		if right == nil {
			right = n
		}
		e.assignment(n, left, right)
	case "assignment_expression":
		e.assignment(n, n.ChildByFieldName("left"), n.ChildByFieldName("right"))
	case "invocation_expression":
		e.call(n)
	case "return_statement":
		e.add(n, semantic.FactReturn, "return", ids(n, e.ast.Source), nil)
	case "if_statement", "for_statement", "while_statement", "switch_statement", "try_statement", "using_statement":
		e.add(n, semantic.FactControl, strings.TrimSuffix(n.Type(), "_statement"), nil, nil)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		e.walk(n.Child(i))
	}
}
func (e *extractor) assignment(n, left, right *sitter.Node) {
	outs := ids(left, e.ast.Source)
	ins := ids(right, e.ast.Source)
	e.add(n, semantic.FactPropagation, "assignment", ins, outs)
	fn := enclosingSymbol(n, e.ast.Source)
	text := astutil.Text(right, e.ast.Source)
	dynamic := right != nil && (right.Type() != "string_literal" && (strings.Contains(text, "+") || strings.Contains(text, "$") || strings.Contains(text, "String.Format")))
	for _, out := range outs {
		e.dynamic[fn][out] = dynamic
		for _, in := range ins {
			if e.external[fn][in] {
				e.external[fn][out] = true
			}
		}
	}
	if e.hasImport("Microsoft.AspNetCore") {
		op := ""
		switch {
		case strings.Contains(text, ".Query"):
			op = "http.query"
		case strings.Contains(text, ".RouteValues"):
			op = "http.path"
		case strings.Contains(text, ".Headers"):
			op = "http.header"
		case strings.Contains(text, ".Form"):
			op = "http.form"
		case strings.Contains(text, ".Body"):
			op = "http.body"
		}
		if op != "" && len(outs) > 0 {
			for _, out := range outs {
				e.external[fn][out] = true
			}
			e.add(n, semantic.FactHTTPInput, op, nil, outs)
			e.add(n, semantic.FactSource, op, nil, outs)
		}
	}
	full := astutil.Text(n, e.ast.Source)
	if e.hasImport("System.Net.Http") && strings.Contains(full, "ServerCertificateCustomValidationCallback") && strings.Contains(full, "=> true") {
		e.add(n, semantic.FactGuard, "csharp.tls.validation_bypass", nil, nil)
	}
}
func (e *extractor) call(n *sitter.Node) {
	fnNode := n.ChildByFieldName("function")
	op := astutil.Text(fnNode, e.ast.Source)
	args := n.ChildByFieldName("arguments")
	f := e.fact(n, semantic.FactCall, op)
	var firstText string
	var firstInputs []string
	if args != nil {
		for i := 0; i < int(args.NamedChildCount()); i++ {
			arg := args.NamedChild(i)
			text := astutil.Text(arg, e.ast.Source)
			f.Arguments = append(f.Arguments, text)
			inputs := ids(arg, e.ast.Source)
			f.ArgumentInputs = append(f.ArgumentInputs, inputs)
			f.Inputs = append(f.Inputs, inputs...)
			if i == 0 {
				firstText = text
				firstInputs = inputs
			}
		}
	}
	if member := fnNode; member != nil && member.Type() == "member_access_expression" {
		method := astutil.Text(member.ChildByFieldName("name"), e.ast.Source)
		receiver := astutil.Text(member.ChildByFieldName("expression"), e.ast.Source)
		fn := enclosingSymbol(n, e.ast.Source)
		if (method == "ExecuteSqlRaw" || method == "FromSqlRaw") && strings.HasSuffix(receiver, ".Database") {
			root := strings.TrimSuffix(receiver, ".Database")
			if e.db[fn][root] && e.hasEFImport() {
				dynamic := len(firstInputs) > 0 && (e.dynamic[fn][firstInputs[0]] || e.external[fn][firstInputs[0]])
				if strings.HasPrefix(strings.TrimSpace(firstText), "$") {
					dynamic = true
				}
				if dynamic {
					f.Kind = semantic.FactSink
					f.Operation = "csharp.ef.raw_sql.dynamic"
				} else {
					f.Kind = semantic.FactDatabaseOperation
					f.Operation = "csharp.ef.raw_sql.parameterized"
				}
			}
		} else {
			f.Operation = method
		}
	}
	e.classifyCall(&f, n)
	e.doc.Facts = append(e.doc.Facts, f)
}
func (e *extractor) classifyCall(f *semantic.Fact, n *sitter.Node) {
	expr := f.Expression
	switch {
	case e.hasImport("System.Diagnostics") && strings.HasPrefix(expr, "Process.Start(") && len(f.Arguments) > 1 && (strings.Contains(f.Arguments[0], "cmd.exe") || strings.Contains(f.Arguments[0], "powershell") || strings.Contains(f.Arguments[0], "pwsh")) && strings.Contains(f.Arguments[1], "/c"):
		f.Kind = semantic.FactSink
		f.Operation = "csharp.shell.command"
	case e.hasImport("System.IO") && (strings.HasPrefix(expr, "File.") || strings.HasPrefix(expr, "Directory.")) && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "csharp.filesystem.path"
	case e.hasImport("System.Net.Http") && strings.Contains(expr, "HttpClient") && (strings.Contains(expr, ".GetAsync(") || strings.Contains(expr, ".SendAsync(")) && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "csharp.http.outbound_url"
	case e.hasImport("System.Threading.Tasks") && strings.HasSuffix(expr, ".Wait()") && strings.Contains(expr, "Task.") && isAsyncContext(n, e.ast.Source):
		f.Kind = semantic.FactGuard
		f.Operation = "csharp.task.blocking"
	}
}
func (e *extractor) hasImport(prefix string) bool {
	for _, v := range e.doc.Imports {
		if v == prefix || strings.HasPrefix(v, prefix+".") {
			return true
		}
	}
	return false
}
func isStringLiteral(v string) bool { return strings.HasPrefix(strings.TrimSpace(v), "\"") }
func isAsyncContext(n *sitter.Node, src []byte) bool {
	if fn := astutil.Ancestor(n, "method_declaration"); fn != nil {
		return strings.Contains(astutil.Text(fn, src), "async")
	}
	return false
}
func (e *extractor) hasEFImport() bool {
	for _, v := range e.doc.Imports {
		if v == "Microsoft.EntityFrameworkCore" {
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
	f.Function = enclosingSymbol(n, e.ast.Source)
	return f
}
func location(file string, n *sitter.Node) semantic.Location {
	s, c, el, ec := astutil.Location(file, n)
	return semantic.Location{File: file, StartLine: s, StartColumn: c, EndLine: el, EndColumn: ec}
}
func symbol(n *sitter.Node, src []byte) string {
	name := astutil.Text(n.ChildByFieldName("name"), src)
	if typ := astutil.Ancestor(n, "class_declaration", "struct_declaration", "interface_declaration"); typ != nil {
		return astutil.Text(typ.ChildByFieldName("name"), src) + "." + name
	}
	return name
}
func enclosingSymbol(n *sitter.Node, src []byte) string {
	if fn := astutil.Ancestor(n, "method_declaration", "constructor_declaration"); fn != nil {
		return symbol(fn, src)
	}
	return ""
}
func ids(n *sitter.Node, src []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range astutil.Descendants(n, "identifier") {
		v := astutil.Text(id, src)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
