package semantics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language/astutil"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
)

type extractor struct {
	ast      *graph.RawAST
	root     *sitter.Node
	doc      *semantic.Document
	declared map[string]bool
	db       map[string]bool
	cursors  map[string]bool
}

func Extract(ast *graph.RawAST) (*semantic.Document, error) {
	if ast == nil {
		return nil, fmt.Errorf("extract Python semantics: nil AST")
	}
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return nil, fmt.Errorf("extract Python semantics: unsupported AST root %T", ast.Root)
	}
	e := &extractor{ast: ast, root: root, declared: map[string]bool{}, db: map[string]bool{}, cursors: map[string]bool{}, doc: &semantic.Document{Path: ast.FilePath, Language: ast.Language, Source: append([]byte(nil), ast.Source...)}}
	e.imports()
	for _, fn := range astutil.Descendants(root, "function_definition") {
		e.declared[astutil.Text(fn.ChildByFieldName("name"), ast.Source)] = true
		e.function(fn)
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
	for _, n := range astutil.Descendants(e.root, "import_statement", "import_from_statement") {
		text := strings.TrimSpace(astutil.Text(n, e.ast.Source))
		if strings.HasPrefix(text, "import ") {
			for _, p := range strings.Split(strings.TrimPrefix(text, "import "), ",") {
				path, _ := splitAlias(p)
				e.doc.Imports = append(e.doc.Imports, path)
			}
		} else {
			parts := strings.Fields(text)
			if len(parts) >= 4 && parts[0] == "from" {
				module := parts[1]
				names := strings.Split(strings.Join(parts[3:], " "), ",")
				for _, name := range names {
					path, _ := splitAlias(name)
					e.doc.Imports = append(e.doc.Imports, module+"."+path)
				}
			}
		}
	}
	sort.Strings(e.doc.Imports)
}
func (e *extractor) function(n *sitter.Node) {
	name := astutil.Text(n.ChildByFieldName("name"), e.ast.Source)
	if class := astutil.Ancestor(n, "class_definition"); class != nil {
		name = astutil.Text(class.ChildByFieldName("name"), e.ast.Source) + "." + name
	}
	fn := semantic.Function{Name: name, Location: loc(e.ast.FilePath, n)}
	params := n.ChildByFieldName("parameters")
	if params != nil {
		for i := 0; i < int(params.NamedChildCount()); i++ {
			p := params.NamedChild(i)
			id := p
			if p.Type() != "identifier" {
				id = p.ChildByFieldName("name")
				if id == nil {
					id = first(p, "identifier")
				}
			}
			name := astutil.Text(id, e.ast.Source)
			if name != "" {
				fn.Parameters = append(fn.Parameters, name)
			}
			value := p.ChildByFieldName("value")
			if value != nil && e.hasImport("fastapi") {
				valueText, operation := astutil.Text(value, e.ast.Source), ""
				for _, candidate := range []struct{ prefix, operation string }{{"Query(", "http.query"}, {"Path(", "http.path"}, {"Body(", "http.body"}, {"Header(", "http.header"}, {"Form(", "http.form"}} {
					if strings.Contains(valueText, candidate.prefix) {
						operation = candidate.operation
						break
					}
				}
				if operation != "" {
					e.add(p, semantic.FactHTTPInput, operation, nil, []string{name})
					e.add(p, semantic.FactSource, operation, nil, []string{name})
				}
			}
			if value != nil && (value.Type() == "list" || value.Type() == "dictionary" || value.Type() == "set") {
				e.add(p, semantic.FactGuard, "python.mutable_default", nil, nil)
			}
		}
	}
	e.doc.Functions = append(e.doc.Functions, fn)
	e.add(n, semantic.FactControl, "function", nil, nil)
}
func (e *extractor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "class_definition":
		e.add(n, semantic.FactControl, "class", nil, nil)
	case "assignment", "augmented_assignment":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		outs := identifiers(left, e.ast.Source)
		ins := identifiers(right, e.ast.Source)
		e.add(n, semantic.FactPropagation, "assignment", ins, outs)
		e.assignmentFacts(n, right, outs)
		if right != nil && (right.Type() == "string" || right.Type() == "concatenated_string") {
			e.add(right, semantic.FactPropagation, "string.construction", ins, outs)
		}
	case "call":
		e.call(n)
	case "return_statement":
		e.add(n, semantic.FactReturn, "return", identifiers(n, e.ast.Source), nil)
	case "if_statement":
		e.add(n, semantic.FactControl, "if", identifiers(n, e.ast.Source), nil)
		text := astutil.Text(n.ChildByFieldName("condition"), e.ast.Source)
		if strings.Contains(text, "None") {
			e.add(n, semantic.FactGuard, "python.opencv.image_checked", identifiers(n, e.ast.Source), nil)
		}
	case "for_statement", "while_statement", "try_statement", "with_statement", "match_statement":
		e.add(n, semantic.FactControl, strings.TrimSuffix(n.Type(), "_statement"), nil, nil)
	case "interpolation":
		e.add(n, semantic.FactPropagation, "string.interpolation", identifiers(n, e.ast.Source), nil)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		e.walk(n.Child(i))
	}
}

func (e *extractor) assignmentFacts(n, right *sitter.Node, outputs []string) {
	if right == nil || len(outputs) == 0 {
		return
	}
	text := astutil.Text(right, e.ast.Source)
	sourceOp := ""
	switch {
	case e.hasImport("flask") && strings.Contains(text, "request.args"):
		sourceOp = "http.query"
	case e.hasImport("flask") && strings.Contains(text, "request.form"):
		sourceOp = "http.form"
	case e.hasImport("flask") && (strings.Contains(text, "request.get_json") || strings.Contains(text, "request.json") || strings.Contains(text, "request.get_data")):
		sourceOp = "http.body"
	case e.hasImport("django") && strings.Contains(text, "request.GET"):
		sourceOp = "http.query"
	case e.hasImport("django") && strings.Contains(text, "request.POST"):
		sourceOp = "http.form"
	}
	if sourceOp != "" {
		e.add(n, semantic.FactHTTPInput, sourceOp, nil, outputs)
		e.add(n, semantic.FactSource, sourceOp, nil, outputs)
	}
	if strings.Contains(text, "sqlite3.connect(") {
		e.db[outputs[0]] = true
	}
	if strings.HasSuffix(strings.TrimSpace(text), ".cursor()") {
		receiver := strings.TrimSuffix(strings.TrimSpace(text), ".cursor()")
		if e.db[receiver] {
			e.cursors[outputs[0]] = true
		}
	}
	if e.hasImport("cv2") && strings.Contains(text, "cv2.imread(") {
		e.add(n, semantic.FactCall, "python.opencv.imread", identifiers(right, e.ast.Source), outputs)
	}
	if e.hasImport("cv2") && strings.Contains(text, "cv2.VideoCapture(") {
		e.add(n, semantic.FactResourceAcquire, "python.opencv.capture", nil, outputs)
	}
}
func (e *extractor) call(n *sitter.Node) {
	fn := n.ChildByFieldName("function")
	operation := astutil.Text(fn, e.ast.Source)
	kind := semantic.FactCall
	builtinDynamic := (operation == "eval" || operation == "exec") && !e.declared[operation]
	args := n.ChildByFieldName("arguments")
	var expressions []string
	var argInputs [][]string
	if args != nil {
		for i := 0; i < int(args.NamedChildCount()); i++ {
			arg := args.NamedChild(i)
			expressions = append(expressions, astutil.Text(arg, e.ast.Source))
			argInputs = append(argInputs, identifiers(arg, e.ast.Source))
		}
	}
	f := e.newFact(n, kind, operation)
	f.Arguments = expressions
	f.ArgumentInputs = argInputs
	for _, inputs := range argInputs {
		f.Inputs = append(f.Inputs, inputs...)
	}
	if builtinDynamic && len(f.Inputs) > 0 {
		f.Kind = semantic.FactSink
		f.Operation = "python.builtin." + operation
	}
	e.classifyCall(&f)
	e.doc.Facts = append(e.doc.Facts, f)
}

func (e *extractor) classifyCall(f *semantic.Fact) {
	op := f.Operation
	receiver := ""
	if dot := strings.LastIndex(op, "."); dot > 0 {
		receiver = op[:dot]
	}
	switch {
	case op == "os.system" && e.hasImport("os"):
		f.Kind = semantic.FactSink
		f.Operation = "python.shell.command"
	case (op == "subprocess.run" || op == "subprocess.Popen" || op == "subprocess.call") && e.hasImport("subprocess") && containsArg(f.Arguments, "shell=True"):
		f.Kind = semantic.FactSink
		f.Operation = "python.shell.command"
	case strings.HasSuffix(op, ".execute") && e.cursors[receiver] && len(f.Arguments) > 0 && !isPythonLiteral(f.Arguments[0]):
		f.Kind = semantic.FactSink
		f.Operation = "python.sql.dynamic"
	case (op == "sqlalchemy.text" || op == "text") && e.hasImport("sqlalchemy") && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "python.sql.dynamic"
	case op == "open" && !e.declared["open"] && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "python.filesystem.path"
	case (op == "pickle.load" || op == "pickle.loads") && e.hasImport("pickle") && len(f.Inputs) > 0:
		f.Kind = semantic.FactSink
		f.Operation = "python.deserialize.unsafe"
	case (op == "requests.get" || op == "requests.post" || op == "requests.request") && e.hasImport("requests") && containsArg(f.Arguments, "verify=False"):
		f.Kind = semantic.FactGuard
		f.Operation = "python.tls.verify_disabled"
	case op == "cv2.resize" || op == "cv2.cvtColor":
		if e.hasImport("cv2") {
			f.Kind = semantic.FactGuard
			f.Operation = "python.opencv.image_consumer"
		}
	case strings.HasSuffix(op, ".release") && e.hasImport("cv2"):
		f.Kind = semantic.FactResourceRelease
		f.Operation = "python.opencv.capture.release"
		f.Inputs = []string{receiver}
	case op == "torch.load" && e.hasImport("torch"):
		if containsArg(f.Arguments, "weights_only=False") {
			f.Kind = semantic.FactSink
			f.Operation = "python.torch.load.unsafe"
		} else {
			f.Operation = "python.torch.load"
		}
	case op == "YOLO" && e.hasImport("ultralytics"):
		f.Operation = "python.ultralytics.model"
	}
}

func (e *extractor) hasImport(prefix string) bool {
	for _, path := range e.doc.Imports {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}
func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if strings.ReplaceAll(arg, " ", "") == want {
			return true
		}
	}
	return false
}
func isPythonLiteral(value string) bool {
	v := strings.TrimSpace(value)
	return strings.HasPrefix(v, "\"") || strings.HasPrefix(v, "'") || strings.HasPrefix(v, "b\"") || strings.HasPrefix(v, "b'")
}
func (e *extractor) add(n *sitter.Node, k semantic.FactKind, op string, inputs, outputs []string) {
	f := e.newFact(n, k, op)
	f.Inputs = inputs
	f.Outputs = outputs
	e.doc.Facts = append(e.doc.Facts, f)
}
func (e *extractor) newFact(n *sitter.Node, k semantic.FactKind, op string) semantic.Fact {
	f := semantic.NewFact(k, op, loc(e.ast.FilePath, n))
	f.Expression = astutil.Text(n, e.ast.Source)
	if fn := astutil.Ancestor(n, "function_definition"); fn != nil {
		f.Function = astutil.Text(fn.ChildByFieldName("name"), e.ast.Source)
		if class := astutil.Ancestor(fn, "class_definition"); class != nil {
			f.Receiver = astutil.Text(class.ChildByFieldName("name"), e.ast.Source)
			f.Function = f.Receiver + "." + f.Function
		}
	}
	return f
}
func loc(file string, n *sitter.Node) semantic.Location {
	s, c, el, ec := astutil.Location(file, n)
	return semantic.Location{File: file, StartLine: s, StartColumn: c, EndLine: el, EndColumn: ec}
}
func first(n *sitter.Node, kind string) *sitter.Node {
	for _, x := range astutil.Descendants(n, kind) {
		return x
	}
	return nil
}
func identifiers(n *sitter.Node, src []byte) []string {
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
func splitAlias(v string) (string, string) {
	parts := strings.Split(strings.TrimSpace(v), " as ")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(v), ""
}
