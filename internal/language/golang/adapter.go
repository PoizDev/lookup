package golang

import (
	"context"
	"fmt"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/golang/rules"
	golangsemantics "github.com/poizdev/lookup/internal/language/golang/semantics"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
	sittergo "github.com/smacker/go-tree-sitter/golang"
)

type Adapter struct{}

var _ language.LanguageAdapter = (*Adapter)(nil)
var _ language.SemanticAdapter = (*Adapter)(nil)

func (a *Adapter) Language() language.Language { return language.Go }
func (a *Adapter) Capabilities() language.Capabilities {
	return language.Capabilities{Parse: true, Structural: true, Semantic: true}
}
func (a *Adapter) ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error) {
	return golangsemantics.Extract(ast)
}

func (a *Adapter) Parse(ctx context.Context, src []byte) (*graph.RawAST, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(sittergo.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		if tree != nil {
			tree.Close()
		}
		parser.Close()
		return nil, fmt.Errorf("failed to parse Go source: %w", err)
	}

	root := tree.RootNode()
	ast := graph.NewRawAST("Go", root, src, func() error {
		tree.Close()
		parser.Close()
		return nil
	})
	ast.HasErrors = hasErrorNodes(root)
	return ast, nil
}

func hasErrorNodes(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.Type() == "ERROR" {
		return true
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if hasErrorNodes(node.Child(i)) {
			return true
		}
	}
	return false
}

func (a *Adapter) ExtractFunctions(ast *graph.RawAST) []graph.Function {
	var funcs []graph.Function
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return funcs
	}

	fnNodes := iterateNodes(root, "function_declaration")
	methodNodes := iterateNodes(root, "method_declaration")
	allNodes := append(fnNodes, methodNodes...)

	for _, node := range allNodes {
		nameNode := findChild(node, "identifier")
		if nameNode == nil {
			nameNode = findChild(node, "field_identifier")
		}
		name := nodeText(nameNode, ast.Source)

		var receiver string
		if node.Type() == "method_declaration" {
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "parameter_list" {
					receiver = nodeText(child, ast.Source)
					break
				}
			}
		}

		var params []string
		var returns []string

		var paramListNode *sitter.Node
		if node.Type() == "method_declaration" {
			count := 0
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "parameter_list" {
					count++
					if count == 2 {
						paramListNode = child
						break
					}
				}
			}
		} else {
			paramListNode = findChild(node, "parameter_list")
		}

		if paramListNode != nil {
			params = append(params, nodeText(paramListNode, ast.Source))
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_identifier" || child.Type() == "parameter_list" && child != paramListNode && nodeText(child, ast.Source) != receiver {
				returns = append(returns, nodeText(child, ast.Source))
			}
		}

		funcs = append(funcs, graph.Function{
			Name:      name,
			Receiver:  receiver,
			Params:    params,
			Returns:   returns,
			StartLine: int(node.StartPoint().Row + 1),
			EndLine:   int(node.EndPoint().Row + 1),
			Body:      nodeText(node, ast.Source),
			File:      ast.FilePath,
		})
	}

	return funcs
}

func (a *Adapter) ExtractTypes(ast *graph.RawAST) []graph.TypeDef {
	var types []graph.TypeDef
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return types
	}

	typeDecls := iterateNodes(root, "type_declaration")
	for _, decl := range typeDecls {
		typeSpecs := iterateNodes(decl, "type_spec")
		for _, spec := range typeSpecs {
			nameNode := findChild(spec, "type_identifier")
			name := nodeText(nameNode, ast.Source)

			kind := ""
			var fields []string
			var methods []string

			structType := findChild(spec, "struct_type")
			if structType != nil {
				kind = "struct"
				fieldDecls := iterateNodes(structType, "field_declaration")
				for _, fd := range fieldDecls {
					idNode := findChild(fd, "field_identifier")
					if idNode != nil {
						fields = append(fields, nodeText(idNode, ast.Source))
					}
				}
			}

			interfaceType := findChild(spec, "interface_type")
			if interfaceType != nil {
				kind = "interface"
				methodSpecs := iterateNodes(interfaceType, "method_spec")
				for _, ms := range methodSpecs {
					idNode := findChild(ms, "field_identifier")
					if idNode != nil {
						methods = append(methods, nodeText(idNode, ast.Source))
					}
				}
			}

			if kind == "" {
				kind = "alias"
			}

			types = append(types, graph.TypeDef{
				Name:      name,
				Kind:      kind,
				Fields:    fields,
				Methods:   methods,
				File:      ast.FilePath,
				StartLine: int(spec.StartPoint().Row + 1),
				EndLine:   int(spec.EndPoint().Row + 1),
			})
		}
	}
	return types
}

func (a *Adapter) ExtractImports(ast *graph.RawAST) []graph.Import {
	var imports []graph.Import
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return imports
	}

	importDecls := iterateNodes(root, "import_declaration")
	for _, decl := range importDecls {
		importSpecs := iterateNodes(decl, "import_spec")
		for _, spec := range importSpecs {
			pathNode := findChild(spec, "interpreted_string_literal")
			if pathNode == nil {
				pathNode = findChild(spec, "raw_string_literal")
			}
			path := strings.Trim(nodeText(pathNode, ast.Source), "\"`")

			alias := ""
			aliasNode := findChild(spec, "package_identifier")
			if aliasNode != nil {
				alias = nodeText(aliasNode, ast.Source)
			} else if dotNode := findChild(spec, "dot"); dotNode != nil {
				alias = "."
			} else if blankNode := findChild(spec, "blank_identifier"); blankNode != nil {
				alias = "_"
			}

			imports = append(imports, graph.Import{
				Path:  path,
				Alias: alias,
				File:  ast.FilePath,
				Line:  int(spec.StartPoint().Row + 1),
			})
		}
	}
	return imports
}

func (a *Adapter) ExtractCalls(ast *graph.RawAST) []graph.Call {
	var calls []graph.Call
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return calls
	}

	callExprs := iterateNodes(root, "call_expression")
	for _, call := range callExprs {
		callerName, callerReceiver := findEnclosingFunction(call, ast.Source)

		var calleeName string
		isMethod := false

		funcNode := call.Child(0)
		if funcNode != nil {
			if funcNode.Type() == "identifier" {
				calleeName = nodeText(funcNode, ast.Source)
			} else if funcNode.Type() == "selector_expression" {
				isMethod = true
				fieldIdNode := findChild(funcNode, "field_identifier")
				if fieldIdNode != nil {
					calleeName = nodeText(fieldIdNode, ast.Source)
				} else {
					calleeName = nodeText(funcNode, ast.Source)
				}
			} else {
				calleeName = nodeText(funcNode, ast.Source)
			}
		}

		calls = append(calls, graph.Call{
			CallerName:     callerName,
			CallerReceiver: callerReceiver,
			CalleeName:     calleeName,
			File:           ast.FilePath,
			Line:           int(call.StartPoint().Row + 1),
			IsMethod:       isMethod,
		})
	}
	return calls
}

func (a *Adapter) ExtractSymbols(ast *graph.RawAST) []graph.Symbol {
	var symbols []graph.Symbol

	funcs := a.ExtractFunctions(ast)
	for _, f := range funcs {
		symbols = append(symbols, graph.Symbol{
			Name:      f.Name,
			Kind:      graph.NodeFunction,
			File:      f.File,
			StartLine: f.StartLine,
			EndLine:   f.EndLine,
		})
	}

	types := a.ExtractTypes(ast)
	for _, t := range types {
		kind := graph.NodeType
		symbols = append(symbols, graph.Symbol{
			Name:      t.Name,
			Kind:      kind,
			File:      t.File,
			StartLine: t.StartLine,
			EndLine:   t.EndLine,
		})
	}

	root, ok := ast.Root.(*sitter.Node)
	if ok && root != nil {
		varDecls := iterateNodes(root, "var_declaration")
		for _, decl := range varDecls {
			specs := iterateNodes(decl, "var_spec")
			for _, spec := range specs {
				idNode := findChild(spec, "identifier")
				if idNode != nil {
					symbols = append(symbols, graph.Symbol{
						Name:      nodeText(idNode, ast.Source),
						Kind:      graph.NodeVariable,
						File:      ast.FilePath,
						StartLine: int(spec.StartPoint().Row + 1),
						EndLine:   int(spec.EndPoint().Row + 1),
						Metadata: map[string]any{
							"Value": nodeText(spec, ast.Source),
							"Body":  nodeText(decl, ast.Source),
						},
					})
				}
			}
		}

		constDecls := iterateNodes(root, "const_declaration")
		for _, decl := range constDecls {
			specs := iterateNodes(decl, "const_spec")
			for _, spec := range specs {
				idNode := findChild(spec, "identifier")
				if idNode != nil {
					symbols = append(symbols, graph.Symbol{
						Name:      nodeText(idNode, ast.Source),
						Kind:      graph.NodeConstant,
						File:      ast.FilePath,
						StartLine: int(spec.StartPoint().Row + 1),
						EndLine:   int(spec.EndPoint().Row + 1),
						Metadata: map[string]any{
							"Value": nodeText(spec, ast.Source),
							"Body":  nodeText(decl, ast.Source),
						},
					})
				}
			}
		}

		pkgClause := findChild(root, "package_clause")
		if pkgClause != nil {
			idNode := findChild(pkgClause, "package_identifier")
			if idNode != nil {
				symbols = append(symbols, graph.Symbol{
					Name:      nodeText(idNode, ast.Source),
					Kind:      graph.NodePackage,
					File:      ast.FilePath,
					StartLine: int(pkgClause.StartPoint().Row + 1),
					EndLine:   int(pkgClause.EndPoint().Row + 1),
				})
			}
		}
	}

	return symbols
}

func (a *Adapter) LanguageRules() []language.AnalysisRule {
	return rules.All()
}

func iterateNodes(node *sitter.Node, nodeType string) []*sitter.Node {
	var matches []*sitter.Node
	if node == nil {
		return matches
	}
	if node.Type() == nodeType {
		matches = append(matches, node)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		matches = append(matches, iterateNodes(node.Child(i), nodeType)...)
	}
	return matches
}

func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if start > end || int(end) > len(src) {
		return ""
	}
	return string(src[start:end])
}

func findChild(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == nodeType {
			return child
		}
	}
	return nil
}

func findEnclosingFunction(node *sitter.Node, src []byte) (string, string) {
	if node == nil {
		return "", ""
	}
	curr := node.Parent()
	for curr != nil {
		if curr.Type() == "function_declaration" || curr.Type() == "method_declaration" {
			nameNode := findChild(curr, "identifier")
			if nameNode == nil {
				nameNode = findChild(curr, "field_identifier")
			}
			var receiver string
			if curr.Type() == "method_declaration" {
				receiver = nodeText(curr.ChildByFieldName("receiver"), src)
			}
			return nodeText(nameNode, src), receiver
		}
		curr = curr.Parent()
	}
	return "", ""
}
