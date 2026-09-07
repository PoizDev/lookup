// Package semantics extracts normalized analyzer facts from a parsed Go Tree-sitter AST.
package semantics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
	sitter "github.com/smacker/go-tree-sitter"
)

type extractor struct {
	ast       *graph.RawAST
	root      *sitter.Node
	document  *semantic.Document
	imports   map[string]string
	ecosystem map[semantic.Ecosystem]bool
	// variableTypes records conservative, function-local receiver provenance.
	// Framework APIs are only normalized when their receiver is known.
	variableTypes           map[string]map[string]string
	staticStrings           map[string]map[string]bool
	globalTypes             map[string]string
	fieldTypes              map[string]map[string]string
	finiteCollections       map[string]map[string]bool
	finiteCollectionFields  map[string]map[string]bool
	finiteCollectionGlobals map[string]bool
}

func Extract(ast *graph.RawAST) (*semantic.Document, error) {
	if ast == nil {
		return nil, fmt.Errorf("extract Go semantics: nil AST")
	}
	root, ok := ast.Root.(*sitter.Node)
	if !ok || root == nil {
		return nil, fmt.Errorf("extract Go semantics: unsupported AST root %T", ast.Root)
	}
	e := &extractor{
		ast:                     ast,
		root:                    root,
		imports:                 make(map[string]string),
		ecosystem:               make(map[semantic.Ecosystem]bool),
		variableTypes:           make(map[string]map[string]string),
		staticStrings:           make(map[string]map[string]bool),
		globalTypes:             make(map[string]string),
		fieldTypes:              make(map[string]map[string]string),
		finiteCollections:       make(map[string]map[string]bool),
		finiteCollectionFields:  make(map[string]map[string]bool),
		finiteCollectionGlobals: make(map[string]bool),
		document:                &semantic.Document{Path: ast.FilePath, Language: ast.Language, Source: append([]byte(nil), ast.Source...)},
	}
	e.extractImports()
	e.extractTypeProvenance()
	e.walk(root)
	sort.SliceStable(e.document.Facts, func(i, j int) bool {
		left, right := e.document.Facts[i], e.document.Facts[j]
		if left.Location.StartLine != right.Location.StartLine {
			return left.Location.StartLine < right.Location.StartLine
		}
		if left.Location.StartColumn != right.Location.StartColumn {
			return left.Location.StartColumn < right.Location.StartColumn
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Operation < right.Operation
	})
	return e.document, nil
}

func (e *extractor) extractTypeProvenance() {
	for _, spec := range descendants(e.root, "var_spec") {
		if enclosingFunction(spec, e.ast.Source) != "" {
			continue
		}
		typeName := e.canonicalType(nodeText(spec.ChildByFieldName("type"), e.ast.Source))
		finite := isFiniteCollectionType(nodeText(spec.ChildByFieldName("type"), e.ast.Source))
		for _, name := range assignmentIdentifiers(spec.ChildByFieldName("name"), e.ast.Source) {
			if typeName != "" {
				e.globalTypes[name] = typeName
			}
			if finite {
				e.finiteCollectionGlobals[name] = true
			}
		}
	}
	for _, spec := range descendants(e.root, "type_spec") {
		name := nodeText(spec.ChildByFieldName("name"), e.ast.Source)
		if name == "" {
			continue
		}
		for _, field := range descendants(spec.ChildByFieldName("type"), "field_declaration") {
			rawFieldType := parameterTypeText(field, e.ast.Source)
			fieldType := e.canonicalType(rawFieldType)
			for index := 0; index < int(field.NamedChildCount()); index++ {
				child := field.NamedChild(index)
				if child.Type() == "field_identifier" || child.Type() == "identifier" {
					if e.fieldTypes[name] == nil {
						e.fieldTypes[name] = make(map[string]string)
					}
					fieldName := nodeText(child, e.ast.Source)
					e.fieldTypes[name][fieldName] = fieldType
					if isFiniteCollectionType(rawFieldType) {
						if e.finiteCollectionFields[name] == nil {
							e.finiteCollectionFields[name] = make(map[string]bool)
						}
						e.finiteCollectionFields[name][fieldName] = true
					}
				}
			}
		}
	}
}

func (e *extractor) extractImports() {
	for _, node := range descendants(e.root, "import_spec") {
		pathNode := firstDescendant(node, "interpreted_string_literal", "raw_string_literal")
		path := strings.Trim(nodeText(pathNode, e.ast.Source), "\"`")
		if path == "" {
			continue
		}
		alias := importBase(path)
		if strings.HasPrefix(path, "github.com/gofiber/fiber/v") {
			alias = "fiber"
		}
		if aliasNode := firstDirectChild(node, "package_identifier", "dot", "blank_identifier"); aliasNode != nil {
			alias = nodeText(aliasNode, e.ast.Source)
		}
		e.imports[alias] = path
		e.document.Imports = append(e.document.Imports, path)
		switch {
		case path == "net/http":
			e.ecosystem[semantic.EcosystemNetHTTP] = true
		case path == "database/sql":
			e.ecosystem[semantic.EcosystemDatabaseSQL] = true
		case path == "os/exec":
			e.ecosystem[semantic.EcosystemOSExec] = true
		case strings.HasPrefix(path, "crypto/"):
			e.ecosystem[semantic.EcosystemCrypto] = true
		case path == "html/template" || path == "text/template":
			e.ecosystem[semantic.EcosystemTemplate] = true
		case path == "os" || path == "path/filepath":
			e.ecosystem[semantic.EcosystemFilesystem] = true
		case path == "github.com/gin-gonic/gin":
			e.ecosystem[semantic.EcosystemGin] = true
		case strings.HasPrefix(path, "github.com/gofiber/fiber/v"):
			e.ecosystem[semantic.EcosystemFiber] = true
		case path == "gorm.io/gorm":
			e.ecosystem[semantic.EcosystemGORM] = true
		}
	}
	sort.Strings(e.document.Imports)
}

func (e *extractor) walk(node *sitter.Node) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "function_declaration", "method_declaration":
		e.extractFunction(node)
	case "short_var_declaration", "assignment_statement", "var_spec":
		e.extractAssignment(node)
	case "call_expression":
		e.extractCall(node)
	case "go_statement":
		operation := "goroutine.start"
		metadata := map[string]string(nil)
		bound, expression, loop := e.enclosingLoopBound(node)
		if loop != nil && bound != "static_numeric" && !looksLikeAcceptLoop(loop, e.ast.Source) {
			operation = "goroutine.loop"
			metadata = map[string]string{"loop_bound": bound}
			if expression != "" {
				metadata["loop_expression"] = expression
			}
		}
		e.addFact(node, semantic.FactConcurrencyOperation, operation, nil, valueIdentifiers(node, e.ast.Source), metadata)
	case "defer_statement":
		if enclosingLoopInFunction(node) != nil {
			e.addFact(node, semantic.FactConcurrencyOperation, "defer.in_loop", nil, valueIdentifiers(node, e.ast.Source), nil)
		}
	case "send_statement":
		e.addFact(node, semantic.FactConcurrencyOperation, "channel.send", nil, valueIdentifiers(node, e.ast.Source), nil)
	case "receive_statement":
		e.addFact(node, semantic.FactConcurrencyOperation, "channel.receive", nil, valueIdentifiers(node, e.ast.Source), nil)
	case "if_statement":
		e.addFact(node, semantic.FactControl, "if", nil, nil, nil)
		e.extractGuardSanitizer(node)
	case "for_statement":
		e.addFact(node, semantic.FactControl, "for", nil, nil, nil)
	case "expression_switch_statement", "type_switch_statement":
		e.addFact(node, semantic.FactControl, "switch", nil, nil, nil)
	case "expression_case", "type_case":
		if !strings.HasPrefix(strings.TrimSpace(nodeText(node, e.ast.Source)), "default") {
			e.addFact(node, semantic.FactControl, "case", nil, nil, nil)
		}
	case "select_statement":
		e.addFact(node, semantic.FactControl, "select", nil, nil, nil)
	case "communication_case":
		if !strings.HasPrefix(strings.TrimSpace(nodeText(node, e.ast.Source)), "default") {
			e.addFact(node, semantic.FactControl, "case", nil, nil, nil)
		}
	case "binary_expression":
		operator := childByFieldText(node, "operator", e.ast.Source)
		if operator == "&&" || operator == "||" {
			e.addFact(node, semantic.FactControl, operator, nil, nil, nil)
		}
	case "keyed_element":
		e.extractKeyedElement(node)
	case "type_assertion_expression":
		e.extractTypeAssertion(node)
	case "selector_expression":
		if nodeText(node.ChildByFieldName("field"), e.ast.Source) == "Error" && strings.HasPrefix(e.selectorReceiverType(node), "gorm.io/gorm.DB") {
			e.addFact(node, semantic.FactGuard, "gorm.error.checked", nil, valueIdentifiers(node.ChildByFieldName("operand"), e.ast.Source), nil)
		}
	case "return_statement":
		e.addFact(node, semantic.FactReturn, "return", nil, valueIdentifiers(node, e.ast.Source), nil)
	}
	for index := 0; index < int(node.ChildCount()); index++ {
		e.walk(node.Child(index))
	}
}

func (e *extractor) extractGuardSanitizer(node *sitter.Node) {
	condition := node.ChildByFieldName("condition")
	consequence := node.ChildByFieldName("consequence")
	if condition == nil || consequence == nil || !strings.HasPrefix(strings.TrimSpace(nodeText(condition, e.ast.Source)), "!") || firstDescendant(consequence, "return_statement") == nil {
		return
	}
	call := firstDescendant(condition, "call_expression")
	if call == nil {
		return
	}
	name := strings.ToLower(callName(call, e.ast.Source))
	if !strings.Contains(name, "allowed") && !strings.Contains(name, "valid") && !strings.Contains(name, "validate") {
		return
	}
	outputs := valueIdentifiers(call.ChildByFieldName("arguments"), e.ast.Source)
	if len(outputs) > 0 {
		e.addFactWithExpression(node, semantic.FactSanitizer, "validation.allowlist", outputs, outputs, map[string]string{"validator": callName(call, e.ast.Source)})
	}
}

func (e *extractor) extractTypeAssertion(node *sitter.Node) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() != "short_var_declaration" && current.Type() != "assignment_statement" {
			continue
		}
		left := current.ChildByFieldName("left")
		if left == nil || left.NamedChildCount() < 2 {
			e.addFact(node, semantic.FactGuard, "type_assertion.unchecked", assignmentIdentifiers(left, e.ast.Source), valueIdentifiers(node, e.ast.Source), nil)
		}
		return
	}
}

func (e *extractor) extractFunction(node *sitter.Node) {
	name := functionSymbol(node, e.ast.Source)
	parameters := node.ChildByFieldName("parameters")
	function := semantic.Function{Name: name, Location: location(e.ast.FilePath, node)}
	if e.variableTypes[name] == nil {
		e.variableTypes[name] = make(map[string]string)
	}
	if e.finiteCollections[name] == nil {
		e.finiteCollections[name] = make(map[string]bool)
	}
	if e.staticStrings[name] == nil {
		e.staticStrings[name] = make(map[string]bool)
	}
	if parameters != nil {
		for _, declaration := range descendants(parameters, "parameter_declaration") {
			rawType := parameterTypeText(declaration, e.ast.Source)
			typeName := e.canonicalType(rawType)
			for index := 0; index < int(declaration.NamedChildCount()); index++ {
				child := declaration.NamedChild(index)
				if child.Type() == "identifier" {
					parameter := nodeText(child, e.ast.Source)
					function.Parameters = append(function.Parameters, parameter)
					if typeName != "" {
						e.variableTypes[name][parameter] = typeName
					}
					if isFiniteCollectionType(rawType) {
						e.finiteCollections[name][parameter] = true
					}
				}
			}
		}
	}
	if receiver := node.ChildByFieldName("receiver"); receiver != nil {
		for _, declaration := range descendants(receiver, "parameter_declaration") {
			typeName := e.canonicalType(parameterTypeText(declaration, e.ast.Source))
			for index := 0; index < int(declaration.NamedChildCount()); index++ {
				child := declaration.NamedChild(index)
				if child.Type() == "identifier" && typeName != "" {
					e.variableTypes[name][nodeText(child, e.ast.Source)] = typeName
				}
			}
		}
	}
	e.document.Functions = append(e.document.Functions, function)
}

func (e *extractor) extractAssignment(node *sitter.Node) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if node.Type() == "var_spec" {
		left = node.ChildByFieldName("name")
		right = node.ChildByFieldName("value")
	}
	if left == nil || right == nil {
		return
	}
	outputs := assignmentIdentifiers(left, e.ast.Source)
	inputs := valueIdentifiers(right, e.ast.Source)
	operation := "assignment"
	call := firstDescendant(right, "call_expression")
	if call != nil {
		operation = callName(call, e.ast.Source)
	}
	e.addPropagation(node, operation, outputs, inputs, call)
	e.inferAssignmentTypes(node, right, call, outputs, inputs)
	e.inferStaticStrings(node, right, outputs, inputs)

	if call == nil {
		return
	}
	if httpOperation, framework, ok := e.httpSource(call); ok {
		metadata := map[string]string{"framework": framework, "user_controlled": "true"}
		sourceOutputs := e.httpSourceOutputs(call, outputs)
		e.addFactWithExpression(call, semantic.FactHTTPInput, httpOperation, sourceOutputs, nil, metadata)
		e.addFactWithExpression(call, semantic.FactSource, httpOperation, sourceOutputs, nil, metadata)
	}
	name := callName(call, e.ast.Source)
	if e.importedCall(name, "os", "Getenv", "LookupEnv") {
		e.addFactWithExpression(call, semantic.FactSource, "environment", firstOutput(outputs), nil, map[string]string{"user_controlled": "external"})
	}
	if e.importedCall(name, "os", "ReadFile") {
		e.addFactWithExpression(call, semantic.FactSource, "file.input", firstOutput(outputs), nil, map[string]string{"user_controlled": "external"})
	}
	if e.isContextAcquire(name) && len(outputs) > 0 {
		e.addFactWithExpression(call, semantic.FactResourceAcquire, "context.cancel", []string{outputs[len(outputs)-1]}, inputs, nil)
	}
	if resourceOperation := e.acquireOperation(name); resourceOperation != "" {
		e.addFactWithExpression(call, semantic.FactResourceAcquire, resourceOperation, outputs, inputs, nil)
	}
	if e.importedCall(name, "bufio", "NewScanner") {
		e.addFactWithExpression(call, semantic.FactResourceAcquire, "scanner", outputs, inputs, nil)
	}
	if e.isDatabaseSQLCall(call) && (lastSelector(name) == "Query" || lastSelector(name) == "QueryContext") {
		e.addFactWithExpression(call, semantic.FactResourceAcquire, "database.rows", outputs, inputs, nil)
	}
	if lastSelector(name) == "Group" && e.isFrameworkRouterCall(call) {
		arguments := argumentTexts(call.ChildByFieldName("arguments"), e.ast.Source)
		metadata := map[string]string{"protection_semantics": "unknown"}
		if len(arguments) > 0 {
			metadata["path"] = unquote(arguments[0])
		}
		if len(outputs) > 0 {
			metadata["group"] = outputs[0]
		}
		e.addFactWithExpression(call, semantic.FactRoute, "http.route.group", outputs, inputs, metadata)
	}
	if (lastSelector(name) == "Begin" || lastSelector(name) == "BeginTx") && (e.isGORMCall(call) || e.isDatabaseSQLCall(call)) {
		operation := "database.transaction"
		if e.isGORMCall(call) {
			operation = "gorm.transaction"
		}
		e.addFactWithExpression(call, semantic.FactResourceAcquire, operation, outputs, inputs, nil)
	}
	if errorIndex, ok := e.knownErrorResultIndex(call); ok && assignmentTargetIsBlank(left, errorIndex, e.ast.Source) {
		e.addFactWithExpression(call, semantic.FactGuard, "error.ignored", nil, inputs, map[string]string{"callee": name})
	}
}

func (e *extractor) addPropagation(node *sitter.Node, operation string, outputs, inputs []string, call *sitter.Node) {
	fact := semantic.NewFact(semantic.FactPropagation, operation, location(e.ast.FilePath, node))
	fact.Function = enclosingFunction(node, e.ast.Source)
	fact.Expression = nodeText(node, e.ast.Source)
	fact.Outputs = unique(outputs)
	fact.Inputs = unique(inputs)
	fact.Metadata = map[string]string{"conditional": boolString(hasAnyAncestor(node, "if_statement", "expression_switch_statement", "type_switch_statement", "select_statement", "for_statement"))}
	if call != nil {
		fact.Arguments = argumentTexts(call.ChildByFieldName("arguments"), e.ast.Source)
		fact.ArgumentInputs = e.callArgumentInputs(call)
		if e.importedOperation(operation) {
			fact.Metadata["external_import"] = "true"
		}
		if e.isGORMCall(call) {
			fact.Metadata["database"] = "gorm"
			right := node.ChildByFieldName("right")
			if right == nil {
				right = node.ChildByFieldName("value")
			}
			if strings.HasSuffix(strings.TrimSpace(nodeText(right, e.ast.Source)), ".Error") {
				fact.Metadata["gorm_error_selected"] = "true"
			}
		} else if e.isDatabaseSQLCall(call) {
			fact.Metadata["database"] = "database/sql"
		}
	}
	e.document.Facts = append(e.document.Facts, fact)
}

func (e *extractor) extractCall(node *sitter.Node) {
	name := callName(node, e.ast.Source)
	method := lastSelector(name)
	argumentsNode := node.ChildByFieldName("arguments")
	arguments := argumentTexts(argumentsNode, e.ast.Source)
	inputs := valueIdentifiers(argumentsNode, e.ast.Source)
	inputs = append(inputs, e.nestedHTTPSourceMarkers(node)...)
	if node.Parent() != nil && node.Parent().Type() == "expression_statement" && e.knownErrorReturningCall(node) {
		e.addFactWithExpression(node, semantic.FactGuard, "error.ignored", nil, inputs, map[string]string{"callee": name})
	}

	if operation, framework, ok := e.httpSource(node); ok && !isAssignedCall(node) {
		metadata := map[string]string{"framework": framework, "user_controlled": "true"}
		outputs := e.httpSourceOutputs(node, []string{sourceMarker(node)})
		e.addFactWithExpression(node, semantic.FactHTTPInput, operation, outputs, nil, metadata)
		e.addFactWithExpression(node, semantic.FactSource, operation, outputs, nil, metadata)
	}

	if operation, parameterized, provider, ok := e.databaseOperation(node, name, arguments); ok {
		dynamic := e.dynamicSQLFirstArgument(node)
		metadata := map[string]string{"parameterized": boolString(parameterized), "dynamic": boolString(dynamic), "database": provider}
		if provider == "gorm" && callErrorSelected(node, e.ast.Source) {
			metadata["error_checked_inline"] = "true"
		}
		e.addFactWithExpression(node, semantic.FactDatabaseOperation, operation, nil, inputs, metadata)
		if dynamic && !parameterized && (operation == "sql.raw" || operation == "sql.exec" || operation == "gorm.where.dynamic" || strings.HasPrefix(operation, "database/sql.")) {
			e.addFactWithExpression(node, semantic.FactSink, operation, nil, e.sqlExpressionInputs(node), metadata)
		}
	}

	if e.ecosystem[semantic.EcosystemOSExec] && e.importedCall(name, "os/exec", "Command", "CommandContext") {
		e.addFactWithExpression(node, semantic.FactCommandExecution, "command.exec", nil, inputs, nil)
		if dangerousInputs, sensitive := e.sensitiveCommandInputs(node, name, arguments); sensitive {
			e.addFactWithExpression(node, semantic.FactSink, "command.exec", nil, dangerousInputs, nil)
		}
	}
	if e.importedCall(name, "net/http", "Redirect") || (method == "Redirect" && e.isFrameworkContextCall(node)) {
		e.addFactWithExpression(node, semantic.FactSink, "http.redirect", nil, inputs, nil)
	}

	if operation := e.fileOperation(name); operation != "" {
		metadata := map[string]string{"arguments": strings.Join(arguments, "\x1f"), "callee": name, "mode_index": strconv.Itoa(e.fileModeArgumentIndex(name))}
		e.addFactWithExpression(node, semantic.FactFileOperation, operation, nil, inputs, metadata)
		if operation == "file.open" || operation == "file.read" || operation == "file.write" {
			e.addFactWithExpression(node, semantic.FactSink, operation, nil, e.argumentInputs(node, 0), metadata)
		}
	}

	if operation := e.cryptoOperation(name); operation != "" {
		e.addFactWithExpression(node, semantic.FactCryptoOperation, operation, nil, inputs, nil)
	}

	if method == "Close" || method == "Stop" || method == "Commit" || method == "Rollback" || (method == "Unlock" && e.isSyncLockCall(node)) || (method == name && isCancelIdentifier(name)) {
		operation := strings.ToLower(method)
		if isCancelIdentifier(name) {
			operation = "context.cancel"
			inputs = append(inputs, name)
		}
		releaseInputs := append(receiverIdentifiers(node, e.ast.Source), inputs...)
		if receiver := strings.TrimSuffix(name, "."+method); receiver != name {
			releaseInputs = append(releaseInputs, receiver)
		}
		e.addFactWithExpression(node, semantic.FactResourceRelease, operation, nil, releaseInputs, map[string]string{"deferred": boolString(hasAncestor(node, "defer_statement"))})
	}
	if method == "Lock" && e.isSyncLockCall(node) {
		receiver := strings.TrimSuffix(name, "."+method)
		e.addFactWithExpression(node, semantic.FactResourceAcquire, "mutex", []string{receiver}, nil, nil)
	}

	if routeMethod(method) && e.isFrameworkRouterCall(node) {
		group := firstString(receiverIdentifiers(node, e.ast.Source))
		function := enclosingFunction(node, e.ast.Source)
		middlewareChain := e.middlewareChain(function, group)
		metadata := map[string]string{"method": method, "group": group, "middleware_attached": boolString(len(middlewareChain) > 0), "middleware_chain": strings.Join(middlewareChain, ","), "protection_semantics": "unknown"}
		if groupPath := e.groupPath(function, group); groupPath != "" {
			metadata["group_path"] = groupPath
		}
		if len(arguments) > 0 {
			metadata["path"] = unquote(arguments[0])
		}
		if len(arguments) > 1 {
			metadata["handler"] = arguments[len(arguments)-1]
		}
		e.addFactWithExpression(node, semantic.FactRoute, "http.route", nil, inputs, metadata)
	}
	if method == "Use" && e.isFrameworkRouterCall(node) {
		group := firstString(receiverIdentifiers(node, e.ast.Source))
		e.addFactWithExpression(node, semantic.FactMiddleware, "http.middleware", nil, inputs, map[string]string{"attached": "true", "group": group, "chain": strings.Join(inputs, ","), "protection_semantics": "unknown"})
	}
	if method == "Add" && hasAncestor(node, "go_statement") && e.receiverType(node) == "sync.WaitGroup" {
		e.addFactWithExpression(node, semantic.FactConcurrencyOperation, "waitgroup.add_in_goroutine", nil, receiverIdentifiers(node, e.ast.Source), nil)
	}
	if e.isGORMCall(node) && (method == "Delete" || method == "Updates") && strings.Contains(name, ".Unscoped().") {
		e.addFactWithExpression(node, semantic.FactDatabaseOperation, "gorm.unscoped."+strings.ToLower(method), nil, inputs, nil)
	}

	e.addCallFact(node, name, arguments, inputs)
}

func (e *extractor) httpSourceOutputs(call *sitter.Node, fallback []string) []string {
	method := lastSelector(callName(call, e.ast.Source))
	if oneOf(method, "ShouldBind", "ShouldBindJSON", "Bind", "BindJSON") || (method == "Body" && call.ChildByFieldName("arguments") != nil && call.ChildByFieldName("arguments").NamedChildCount() > 0) {
		arguments := call.ChildByFieldName("arguments")
		if arguments != nil && arguments.NamedChildCount() > 0 {
			if outputs := valueIdentifiers(arguments.NamedChild(0), e.ast.Source); len(outputs) > 0 {
				return outputs
			}
		}
	}
	return fallback
}

func (e *extractor) addCallFact(node *sitter.Node, operation string, arguments, inputs []string) {
	fact := semantic.NewFact(semantic.FactCall, operation, location(e.ast.FilePath, node))
	fact.Function = enclosingFunction(node, e.ast.Source)
	fact.Expression = nodeText(node, e.ast.Source)
	fact.Arguments = append([]string(nil), arguments...)
	fact.ArgumentInputs = e.callArgumentInputs(node)
	fact.Inputs = unique(inputs)
	fact.Metadata = map[string]string{"arguments": strings.Join(arguments, "\x1f"), "in_loop": boolString(hasAncestor(node, "for_statement"))}
	if e.importedOperation(operation) {
		fact.Metadata["external_import"] = "true"
	}
	e.document.Facts = append(e.document.Facts, fact)
}

func (e *extractor) hasMiddleware(function, group string) bool {
	return len(e.middlewareChain(function, group)) > 0
}

func (e *extractor) middlewareChain(function, group string) []string {
	var chain []string
	for _, fact := range e.document.Facts {
		if fact.Kind == semantic.FactMiddleware && fact.Function == function && fact.Metadata["group"] == group {
			chain = append(chain, fact.Inputs...)
		}
	}
	return unique(chain)
}

func (e *extractor) groupPath(function, group string) string {
	for _, fact := range e.document.Facts {
		if fact.Kind == semantic.FactRoute && fact.Operation == "http.route.group" && fact.Function == function && fact.Metadata["group"] == group {
			return fact.Metadata["path"]
		}
	}
	return ""
}

func (e *extractor) extractKeyedElement(node *sitter.Node) {
	var named []*sitter.Node
	for index := 0; index < int(node.NamedChildCount()); index++ {
		named = append(named, node.NamedChild(index))
	}
	if len(named) < 2 {
		return
	}
	key := strings.TrimSpace(nodeText(named[0], e.ast.Source))
	value := strings.TrimSpace(nodeText(named[len(named)-1], e.ast.Source))
	compositeType := e.enclosingCompositeType(node)
	if key == "InsecureSkipVerify" && value == "true" && compositeType == "crypto/tls.Config" {
		e.addFact(node, semantic.FactCryptoOperation, "tls.insecure_skip_verify", nil, nil, nil)
	}
	if key == "AllowGlobalUpdate" && value == "true" && compositeType == "gorm.io/gorm.Session" {
		e.addFact(node, semantic.FactDatabaseOperation, "gorm.allow_global_update", nil, nil, nil)
	}
}

func (e *extractor) enclosingCompositeType(node *sitter.Node) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() != "composite_literal" {
			continue
		}
		typeNode := current.ChildByFieldName("type")
		if typeNode == nil && current.NamedChildCount() > 0 {
			typeNode = current.NamedChild(0)
		}
		return e.canonicalType(nodeText(typeNode, e.ast.Source))
	}
	return ""
}

func (e *extractor) nestedHTTPSourceMarkers(node *sitter.Node) []string {
	var markers []string
	for _, nested := range descendants(node, "call_expression") {
		if nested.Equal(node) {
			continue
		}
		if _, _, ok := e.httpSource(nested); ok {
			markers = append(markers, sourceMarker(nested))
		}
	}
	return unique(markers)
}

func (e *extractor) httpSource(call *sitter.Node) (operation, framework string, ok bool) {
	name := callName(call, e.ast.Source)
	method := lastSelector(name)
	receiverType := e.receiverType(call)
	if strings.HasPrefix(receiverType, "net/http.Request") {
		if strings.HasSuffix(name, ".URL.Query().Get") || strings.HasSuffix(name, ".FormValue") {
			return "http.query", "net/http", true
		}
		if strings.HasSuffix(name, ".Header.Get") {
			return "http.header", "net/http", true
		}
	}
	if receiverType == "github.com/gin-gonic/gin.Context" {
		switch method {
		case "Query", "DefaultQuery", "QueryArray", "QueryMap":
			return "http.query", "gin", true
		case "Param":
			return "http.path", "gin", true
		case "GetHeader":
			return "http.header", "gin", true
		case "ShouldBind", "ShouldBindJSON", "Bind", "BindJSON":
			return "http.body", "gin", true
		}
	}
	if isFiberContextType(receiverType) {
		switch method {
		case "Query", "Queries":
			return "http.query", "fiber", true
		case "Params", "AllParams":
			return "http.path", "fiber", true
		case "Get":
			return "http.header", "fiber", true
		case "Body", "BodyRaw", "Bind":
			return "http.body", "fiber", true
		}
	}
	return "", "", false
}

func (e *extractor) databaseOperation(call *sitter.Node, name string, arguments []string) (string, bool, string, bool) {
	method := lastSelector(name)
	dynamic := e.dynamicSQLFirstArgument(call)
	parameterized := !dynamic && isParameterizedAt(arguments, e.sqlArgumentIndex(call))
	if e.isGORMCall(call) {
		switch method {
		case "Raw", "Exec":
			return "sql." + strings.ToLower(method), parameterized, "gorm", true
		case "Where":
			label := "literal"
			if dynamic {
				label = "dynamic"
			} else if parameterized {
				label = "parameterized"
			}
			return "gorm.where." + label, parameterized, "gorm", true
		case "First", "Find", "Take", "Last", "Scan", "Create", "Save", "Delete", "Updates", "Transaction", "Begin", "Commit", "Rollback", "Unscoped", "Session":
			return "gorm." + strings.ToLower(method), true, "gorm", true
		}
	}
	if e.isDatabaseSQLCall(call) {
		switch method {
		case "Query", "QueryRow", "Exec", "QueryContext", "QueryRowContext", "ExecContext", "Prepare", "PrepareContext":
			return "database/sql." + strings.ToLower(method), parameterized, "database/sql", true
		}
	}
	return "", false, "", false
}

func callErrorSelected(call *sitter.Node, source []byte) bool {
	parent := call.Parent()
	return parent != nil && parent.Type() == "selector_expression" && parent.ChildByFieldName("operand") != nil && parent.ChildByFieldName("operand").Equal(call) && nodeText(parent.ChildByFieldName("field"), source) == "Error"
}

func (e *extractor) fileOperation(name string) string {
	if !e.ecosystem[semantic.EcosystemFilesystem] {
		return ""
	}
	switch {
	case e.importedCall(name, "os", "Open", "OpenFile"):
		return "file.open"
	case e.importedCall(name, "os", "ReadFile"):
		return "file.read"
	case e.importedCall(name, "os", "WriteFile", "Create"):
		return "file.write"
	case e.importedCall(name, "os", "Chmod", "Mkdir", "MkdirAll"):
		return "file.permission"
	case e.importedCall(name, "path/filepath", "Join", "Clean"):
		return "path.construct"
	default:
		return ""
	}
}

func (e *extractor) fileModeArgumentIndex(name string) int {
	switch {
	case e.importedCall(name, "os", "OpenFile", "WriteFile"):
		return 2
	case e.importedCall(name, "os", "Chmod", "Mkdir", "MkdirAll"):
		return 1
	default:
		return -1
	}
}

func (e *extractor) addFact(node *sitter.Node, kind semantic.FactKind, operation string, outputs, inputs []string, metadata map[string]string) {
	e.addFactWithExpression(node, kind, operation, outputs, inputs, metadata)
}

func (e *extractor) addFactWithExpression(node *sitter.Node, kind semantic.FactKind, operation string, outputs, inputs []string, metadata map[string]string) {
	fact := semantic.NewFact(kind, operation, location(e.ast.FilePath, node))
	fact.Function = enclosingFunction(node, e.ast.Source)
	fact.Expression = nodeText(node, e.ast.Source)
	fact.Outputs = unique(outputs)
	fact.Inputs = unique(inputs)
	fact.Metadata = metadata
	e.document.Facts = append(e.document.Facts, fact)
}

func location(file string, node *sitter.Node) semantic.Location {
	return semantic.Location{File: file, StartLine: int(node.StartPoint().Row) + 1, StartColumn: int(node.StartPoint().Column) + 1, EndLine: int(node.EndPoint().Row) + 1, EndColumn: int(node.EndPoint().Column) + 1}
}

func descendants(node *sitter.Node, types ...string) []*sitter.Node {
	var result []*sitter.Node
	if node == nil {
		return result
	}
	if oneOf(node.Type(), types...) {
		result = append(result, node)
	}
	for index := 0; index < int(node.ChildCount()); index++ {
		result = append(result, descendants(node.Child(index), types...)...)
	}
	return result
}

func firstDescendant(node *sitter.Node, types ...string) *sitter.Node {
	values := descendants(node, types...)
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func firstDirectChild(node *sitter.Node, types ...string) *sitter.Node {
	if node == nil {
		return nil
	}
	for index := 0; index < int(node.ChildCount()); index++ {
		child := node.Child(index)
		if oneOf(child.Type(), types...) {
			return child
		}
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func nodeText(node *sitter.Node, source []byte) string {
	if node == nil || int(node.EndByte()) > len(source) {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

func childByFieldText(node *sitter.Node, field string, source []byte) string {
	return nodeText(node.ChildByFieldName(field), source)
}

func assignmentIdentifiers(node *sitter.Node, source []byte) []string {
	var result []string
	for _, identifier := range descendants(node, "identifier") {
		result = append(result, nodeText(identifier, source))
	}
	return unique(result)
}

func enclosingLoopInFunction(node *sitter.Node) *sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Type() {
		case "function_declaration", "method_declaration", "func_literal":
			return nil
		case "for_statement":
			return current
		}
	}
	return nil
}

func looksLikeAcceptLoop(loop *sitter.Node, source []byte) bool {
	text := nodeText(loop, source)
	return strings.Contains(text, ".Accept()") || strings.Contains(text, ".AcceptTCP()")
}

func valueIdentifiers(node *sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var result []string
	for _, identifier := range descendants(node, "identifier") {
		text := nodeText(identifier, source)
		parent := identifier.Parent()
		if parent != nil && parent.Type() == "selector_expression" && parent.ChildByFieldName("field") != nil && parent.ChildByFieldName("field").Equal(identifier) {
			continue
		}
		result = append(result, text)
	}
	return unique(result)
}

func assignmentTargetIsBlank(node *sitter.Node, resultIndex int, source []byte) bool {
	if node == nil || resultIndex < 0 {
		return false
	}
	targets := make([]string, 0, node.NamedChildCount())
	if node.Type() == "identifier" || node.Type() == "blank_identifier" {
		targets = append(targets, nodeText(node, source))
	} else {
		for index := 0; index < int(node.NamedChildCount()); index++ {
			targets = append(targets, strings.TrimSpace(nodeText(node.NamedChild(index), source)))
		}
	}
	return resultIndex < len(targets) && targets[resultIndex] == "_"
}

func namedIdentifierCount(node *sitter.Node) int {
	return len(descendants(node, "identifier")) + len(descendants(node, "blank_identifier"))
}

func receiverIdentifiers(call *sitter.Node, source []byte) []string {
	function := call.ChildByFieldName("function")
	if function == nil || function.Type() != "selector_expression" {
		return nil
	}
	return valueIdentifiers(function.ChildByFieldName("operand"), source)
}

func callName(call *sitter.Node, source []byte) string {
	if call == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(call.ChildByFieldName("function"), source))
}

func lastSelector(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}

func argumentTexts(arguments *sitter.Node, source []byte) []string {
	if arguments == nil {
		return nil
	}
	var values []string
	for index := 0; index < int(arguments.NamedChildCount()); index++ {
		values = append(values, nodeText(arguments.NamedChild(index), source))
	}
	return values
}

func enclosingFunction(node *sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() == "function_declaration" || current.Type() == "method_declaration" {
			return functionSymbol(current, source)
		}
	}
	return ""
}

func functionSymbol(node *sitter.Node, source []byte) string {
	name := nodeText(node.ChildByFieldName("name"), source)
	if node == nil || node.Type() != "method_declaration" {
		return name
	}
	receiver := node.ChildByFieldName("receiver")
	types := descendants(receiver, "type_identifier")
	if len(types) == 0 {
		return name
	}
	return nodeText(types[len(types)-1], source) + "." + name
}

func hasAncestor(node *sitter.Node, nodeType string) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() == nodeType {
			return true
		}
	}
	return false
}

func hasAnyAncestor(node *sitter.Node, nodeTypes ...string) bool {
	for _, nodeType := range nodeTypes {
		if hasAncestor(node, nodeType) {
			return true
		}
	}
	return false
}

// staticallyBoundedForLoop recognizes only the canonical increasing induction
// form. Comparisons inside the body, missing updates, and mismatched variables
// are deliberately not treated as proofs of bounded iteration.
func staticallyBoundedForLoop(loop *sitter.Node, source []byte) bool {
	clause := firstDirectChild(loop, "for_clause")
	if clause == nil {
		return false
	}
	initializer := clause.ChildByFieldName("initializer")
	condition := clause.ChildByFieldName("condition")
	update := clause.ChildByFieldName("update")
	if initializer == nil || condition == nil || update == nil || condition.Type() != "binary_expression" {
		return false
	}
	operator := childByFieldText(condition, "operator", source)
	left := condition.ChildByFieldName("left")
	right := condition.ChildByFieldName("right")
	if (operator != "<" && operator != "<=") || left == nil || left.Type() != "identifier" || right == nil || right.Type() != "int_literal" {
		return false
	}
	induction := strings.TrimSpace(nodeText(left, source))
	initialLeft := initializer.ChildByFieldName("left")
	initialRight := initializer.ChildByFieldName("right")
	if initialLeft == nil || initialRight == nil || strings.TrimSpace(nodeText(initialLeft, source)) != induction || initialRight.Type() != "expression_list" && initialRight.Type() != "int_literal" {
		return false
	}
	if initialRight.Type() == "expression_list" {
		literal := firstDirectChild(initialRight, "int_literal")
		if literal == nil || strings.TrimSpace(nodeText(initialRight, source)) != strings.TrimSpace(nodeText(literal, source)) {
			return false
		}
	}
	updateText := strings.ReplaceAll(strings.TrimSpace(nodeText(update, source)), " ", "")
	return updateText == induction+"++"
}

// enclosingLoopBound classifies the complete loop chain that can execute node.
// A finite collection label is valid only when every enclosing loop is either a
// canonical numeric loop or a range over a known finite collection.
func (e *extractor) enclosingLoopBound(node *sitter.Node) (string, string, *sitter.Node) {
	allNumeric, allFinite := true, true
	unknownRange := false
	var nearest *sitter.Node
	var rangeSource string
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Type() {
		case "function_declaration", "method_declaration":
			return loopBoundResult(nearest, allNumeric, allFinite, unknownRange, rangeSource)
		case "func_literal":
			if !immediatelyInvokedFunctionLiteral(current) {
				return loopBoundResult(nearest, allNumeric, allFinite, unknownRange, rangeSource)
			}
		case "for_statement":
			if nearest == nil {
				nearest = current
			}
			if staticallyBoundedForLoop(current, e.ast.Source) {
				continue
			}
			allNumeric = false
			if e.rangeIsFiniteCollection(current) {
				if rangeSource == "" {
					rangeSource = rangeExpression(current, e.ast.Source)
				}
				continue
			}
			allFinite = false
			if firstDirectChild(current, "range_clause") != nil {
				unknownRange = true
			}
		}
	}
	return loopBoundResult(nearest, allNumeric, allFinite, unknownRange, rangeSource)
}

func loopBoundResult(nearest *sitter.Node, allNumeric, allFinite, unknownRange bool, rangeSource string) (string, string, *sitter.Node) {
	if nearest == nil {
		return "", "", nil
	}
	if allNumeric {
		return "static_numeric", "", nearest
	}
	if allFinite {
		return "finite_collection", rangeSource, nearest
	}
	if unknownRange {
		return "unknown_range", rangeSource, nearest
	}
	return "unknown", rangeSource, nearest
}

func immediatelyInvokedFunctionLiteral(literal *sitter.Node) bool {
	if literal == nil || literal.Type() != "func_literal" {
		return false
	}
	function := literal
	parent := literal.Parent()
	for parent != nil && parent.Type() == "parenthesized_expression" {
		function = parent
		parent = parent.Parent()
	}
	return parent != nil && parent.Type() == "call_expression" && parent.ChildByFieldName("function") != nil && parent.ChildByFieldName("function").Equal(function)
}

func (e *extractor) rangeIsFiniteCollection(loop *sitter.Node) bool {
	clause := firstDirectChild(loop, "range_clause")
	if clause == nil {
		return false
	}
	right := clause.ChildByFieldName("right")
	if right == nil {
		right = clause.ChildByFieldName("value")
	}
	if right == nil {
		return false
	}
	if oneOf(right.Type(), "interpreted_string_literal", "raw_string_literal", "composite_literal") {
		return true
	}
	function := enclosingFunction(loop, e.ast.Source)
	text := strings.TrimSpace(nodeText(right, e.ast.Source))
	if right.Type() == "identifier" {
		return e.finiteCollections[function][text] || e.finiteCollectionGlobals[text]
	}
	if right.Type() == "selector_expression" {
		field := nodeText(right.ChildByFieldName("field"), e.ast.Source)
		receiver := nodeText(right.ChildByFieldName("operand"), e.ast.Source)
		receiver = strings.Split(receiver, ".")[0]
		typeName := e.variableTypes[function][receiver]
		if typeName == "" {
			typeName = e.globalTypes[receiver]
		}
		return e.finiteCollectionFields[typeName][field]
	}
	return false
}

func rangeExpression(loop *sitter.Node, source []byte) string {
	clause := firstDirectChild(loop, "range_clause")
	if clause == nil {
		return ""
	}
	right := clause.ChildByFieldName("right")
	if right == nil {
		right = clause.ChildByFieldName("value")
	}
	return strings.TrimSpace(nodeText(right, source))
}

func isFiniteCollectionType(value string) bool {
	value = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(value), "*"))
	return strings.HasPrefix(value, "[]") || strings.HasPrefix(value, "[") || strings.HasPrefix(value, "map[") || value == "string"
}

func isAssignedCall(node *sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Type() {
		case "call_expression":
			// A nested call is an evaluated argument, not the value assigned by
			// the surrounding assignment. It needs its own source marker.
			return false
		case "short_var_declaration", "assignment_statement", "var_spec":
			return true
		case "expression_statement", "return_statement":
			return false
		}
	}
	return false
}

func sourceMarker(node *sitter.Node) string {
	return fmt.Sprintf("@source:%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1)
}

func importBase(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func isParameterizedAt(arguments []string, queryIndex int) bool {
	if queryIndex < 0 || len(arguments) <= queryIndex+1 {
		return false
	}
	query := unquote(arguments[queryIndex])
	return strings.Contains(query, "?") || containsDollarPlaceholder(query)
}

func (e *extractor) dynamicSQLFirstArgument(call *sitter.Node) bool {
	arguments := call.ChildByFieldName("arguments")
	argumentIndex := e.sqlArgumentIndex(call)
	if arguments == nil || argumentIndex >= int(arguments.NamedChildCount()) {
		return false
	}
	first := arguments.NamedChild(argumentIndex)
	if oneOf(first.Type(), "interpreted_string_literal", "raw_string_literal") {
		return false
	}
	if lastSelector(callName(call, e.ast.Source)) == "Where" && oneOf(first.Type(), "composite_literal", "unary_expression") {
		return false
	}
	if first.Type() == "identifier" {
		return !e.staticStrings[enclosingFunction(call, e.ast.Source)][nodeText(first, e.ast.Source)]
	}
	return true
}

func (e *extractor) sqlArgumentIndex(call *sitter.Node) int {
	if e.isDatabaseSQLCall(call) && strings.HasSuffix(lastSelector(callName(call, e.ast.Source)), "Context") {
		return 1
	}
	return 0
}

func (e *extractor) sqlExpressionInputs(call *sitter.Node) []string {
	arguments := call.ChildByFieldName("arguments")
	index := e.sqlArgumentIndex(call)
	if arguments == nil || index >= int(arguments.NamedChildCount()) {
		return nil
	}
	argument := arguments.NamedChild(index)
	inputs := valueIdentifiers(argument, e.ast.Source)
	for _, nested := range descendants(argument, "call_expression") {
		if _, _, ok := e.httpSource(nested); ok {
			inputs = append(inputs, sourceMarker(nested))
		}
	}
	return unique(inputs)
}

func (e *extractor) sensitiveCommandInputs(call *sitter.Node, name string, arguments []string) ([]string, bool) {
	executable := 0
	if lastSelector(name) == "CommandContext" {
		executable = 1
	}
	if executable >= len(arguments) {
		return nil, false
	}
	command := strings.TrimSpace(arguments[executable])
	if !isStringLiteral(command) {
		return e.argumentInputs(call, executable), true
	}
	shell := strings.ToLower(unquote(command))
	if !oneOf(shell, "sh", "bash", "dash", "zsh", "ksh", "cmd", "cmd.exe", "powershell", "pwsh") {
		return nil, false
	}
	flagIndex := executable + 1
	commandIndex := executable + 2
	if flagIndex >= len(arguments) || commandIndex >= len(arguments) || !oneOf(strings.ToLower(unquote(arguments[flagIndex])), "-c", "/c", "-command") {
		return nil, false
	}
	return e.argumentInputs(call, commandIndex), true
}

func (e *extractor) argumentInputs(call *sitter.Node, index int) []string {
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil || index >= int(arguments.NamedChildCount()) {
		return nil
	}
	argument := arguments.NamedChild(index)
	inputs := valueIdentifiers(argument, e.ast.Source)
	for _, nested := range descendants(argument, "call_expression") {
		if _, _, ok := e.httpSource(nested); ok {
			inputs = append(inputs, sourceMarker(nested))
		}
	}
	return unique(inputs)
}

func (e *extractor) callArgumentInputs(call *sitter.Node) [][]string {
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil {
		return nil
	}
	result := make([][]string, 0, arguments.NamedChildCount())
	for index := 0; index < int(arguments.NamedChildCount()); index++ {
		result = append(result, e.argumentInputs(call, index))
	}
	return result
}

func (e *extractor) importedOperation(operation string) bool {
	root := operation
	if dot := strings.Index(root, "."); dot >= 0 {
		root = root[:dot]
	}
	_, imported := e.imports[root]
	return imported
}

func isStringLiteral(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '`' && value[len(value)-1] == '`'))
}

func containsDollarPlaceholder(value string) bool {
	for index := 0; index+1 < len(value); index++ {
		if value[index] == '$' && value[index+1] >= '1' && value[index+1] <= '9' {
			return true
		}
	}
	return false
}

func unquote(value string) string {
	return strings.Trim(value, "\"'`")
}

func (e *extractor) isContextAcquire(name string) bool {
	return e.importedCall(name, "context", "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithDeadlineCause", "WithTimeoutCause")
}

func (e *extractor) knownErrorReturningCall(call *sitter.Node) bool {
	_, ok := e.knownErrorResultIndex(call)
	return ok
}

func (e *extractor) knownErrorResultIndex(call *sitter.Node) (int, bool) {
	name := callName(call, e.ast.Source)
	if e.importedCall(name, "os", "WriteFile") {
		return 0, true
	}
	if e.importedCall(name, "net/http", "Get", "Post") ||
		e.importedCall(name, "os", "Open", "OpenFile", "Create", "ReadFile") ||
		e.importedCall(name, "strconv", "Atoi", "ParseBool", "ParseFloat", "ParseInt", "ParseUint") {
		return 1, true
	}
	method := lastSelector(name)
	receiverType := e.receiverType(call)
	if strings.HasPrefix(receiverType, "database/sql.") {
		if oneOf(method, "Scan", "Close") {
			return 0, true
		}
		if oneOf(method, "Query", "QueryContext", "Exec", "ExecContext") {
			return 1, true
		}
	}
	if receiverType == "os.File" {
		if method == "Close" {
			return 0, true
		}
		if oneOf(method, "Read", "Write") {
			return 1, true
		}
	}
	return 0, false
}

func (e *extractor) acquireOperation(name string) string {
	switch {
	case e.importedCall(name, "net/http", "Get", "Post"):
		return "http.response.body"
	case e.importedCall(name, "os", "Open", "OpenFile", "Create"):
		return "file"
	case e.importedCall(name, "time", "NewTicker"):
		return "ticker"
	default:
		return ""
	}
}

func isCancelIdentifier(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "cancel")
}

func (e *extractor) cryptoOperation(name string) string {
	switch {
	case e.importedCall(name, "crypto/md5", "New", "Sum"):
		return "crypto.weak.md5"
	case e.importedCall(name, "crypto/sha1", "New", "Sum"):
		return "crypto.weak.sha1"
	default:
		return ""
	}
}

func (e *extractor) importedCall(name, importPath string, methods ...string) bool {
	dot := strings.Index(name, ".")
	if dot <= 0 || e.imports[name[:dot]] != importPath {
		return false
	}
	return oneOf(lastSelector(name), methods...)
}

func parameterTypeText(declaration *sitter.Node, source []byte) string {
	if declaration == nil {
		return ""
	}
	if typeNode := declaration.ChildByFieldName("type"); typeNode != nil {
		return nodeText(typeNode, source)
	}
	for index := int(declaration.NamedChildCount()) - 1; index >= 0; index-- {
		child := declaration.NamedChild(index)
		if child.Type() != "identifier" {
			return nodeText(child, source)
		}
	}
	return ""
}

func (e *extractor) canonicalType(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "*[]")
	if dot := strings.Index(value, "."); dot > 0 {
		if importPath := e.imports[value[:dot]]; importPath != "" {
			return importPath + value[dot:]
		}
	}
	return value
}

func (e *extractor) inferAssignmentTypes(node, right, call *sitter.Node, outputs, inputs []string) {
	if len(outputs) == 0 {
		return
	}
	function := enclosingFunction(node, e.ast.Source)
	if e.variableTypes[function] == nil {
		e.variableTypes[function] = make(map[string]string)
	}
	if call == nil {
		if len(inputs) == 1 {
			if typeName := e.variableTypes[function][inputs[0]]; typeName != "" {
				e.variableTypes[function][outputs[0]] = typeName
			} else if typeName := e.globalTypes[inputs[0]]; typeName != "" {
				e.variableTypes[function][outputs[0]] = typeName
			}
		}
		return
	}

	name := callName(call, e.ast.Source)
	method := lastSelector(name)
	typeName := ""
	switch {
	case e.importedCall(name, "github.com/gin-gonic/gin", "New", "Default"):
		typeName = "github.com/gin-gonic/gin.Engine"
	case e.importedCall(name, "github.com/gofiber/fiber/v2", "New"):
		typeName = "github.com/gofiber/fiber/v2.App"
	case e.importedCall(name, "github.com/gofiber/fiber/v3", "New"):
		typeName = "github.com/gofiber/fiber/v3.App"
	case e.importedCall(name, "gorm.io/gorm", "Open"):
		typeName = "gorm.io/gorm.DB"
	case e.importedCall(name, "database/sql", "Open"):
		typeName = "database/sql.DB"
	case e.importedCall(name, "bufio", "NewScanner"):
		typeName = "bufio.Scanner"
	case e.importedCall(name, "net/http", "Get", "Post"):
		typeName = "net/http.Response"
	case e.importedCall(name, "os", "Open", "OpenFile", "Create"):
		typeName = "os.File"
	case e.importedCall(name, "time", "NewTicker"):
		typeName = "time.Ticker"
	case method == "Group" && e.isFrameworkRouterCall(call):
		if strings.HasPrefix(e.receiverType(call), "github.com/gin-gonic/gin.") {
			typeName = "github.com/gin-gonic/gin.RouterGroup"
		} else if strings.HasPrefix(e.receiverType(call), "github.com/gofiber/fiber/") {
			typeName = strings.TrimSuffix(e.receiverType(call), ".App") + ".Group"
		}
	case e.isGORMCall(call):
		typeName = "gorm.io/gorm.DB"
	case e.isDatabaseSQLCall(call) && oneOf(method, "Query", "QueryContext"):
		typeName = "database/sql.Rows"
	case e.isDatabaseSQLCall(call) && oneOf(method, "Begin", "BeginTx"):
		typeName = "database/sql.Tx"
	}
	if typeName != "" {
		e.variableTypes[function][outputs[0]] = typeName
	}
	_ = right
}

func (e *extractor) inferStaticStrings(node, right *sitter.Node, outputs, inputs []string) {
	if len(outputs) == 0 {
		return
	}
	if hasAnyAncestor(node, "if_statement", "expression_switch_statement", "type_switch_statement", "select_statement", "for_statement") {
		return
	}
	function := enclosingFunction(node, e.ast.Source)
	if e.staticStrings[function] == nil {
		e.staticStrings[function] = make(map[string]bool)
	}
	expression := firstExpression(right)
	static := expression != nil && oneOf(expression.Type(), "interpreted_string_literal", "raw_string_literal")
	if expression != nil && expression.Type() == "identifier" && len(inputs) == 1 {
		static = e.staticStrings[function][inputs[0]]
	}
	e.staticStrings[function][outputs[0]] = static
}

func firstExpression(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Type() != "expression_list" {
		return node
	}
	if node.NamedChildCount() == 0 {
		return nil
	}
	return node.NamedChild(0)
}

func (e *extractor) receiverType(call *sitter.Node) string {
	if call == nil {
		return ""
	}
	name := callName(call, e.ast.Source)
	root := name
	if dot := strings.Index(root, "."); dot >= 0 {
		root = root[:dot]
	}
	function := enclosingFunction(call, e.ast.Source)
	typeName := e.variableTypes[function][root]
	if typeName == "" {
		typeName = e.globalTypes[root]
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		if fieldType := e.fieldTypes[typeName][parts[1]]; fieldType != "" {
			return fieldType
		}
	}
	return typeName
}

func (e *extractor) selectorReceiverType(selector *sitter.Node) string {
	if selector == nil {
		return ""
	}
	root := strings.TrimSpace(nodeText(selector.ChildByFieldName("operand"), e.ast.Source))
	if dot := strings.Index(root, "."); dot >= 0 {
		root = root[:dot]
	}
	return e.variableTypes[enclosingFunction(selector, e.ast.Source)][root]
}

func (e *extractor) isGORMCall(call *sitter.Node) bool {
	return strings.HasPrefix(e.receiverType(call), "gorm.io/gorm.DB")
}

func (e *extractor) isDatabaseSQLCall(call *sitter.Node) bool {
	return strings.HasPrefix(e.receiverType(call), "database/sql.")
}

func (e *extractor) isSyncLockCall(call *sitter.Node) bool {
	switch e.receiverType(call) {
	case "sync.Mutex", "sync.RWMutex", "sync.Locker":
		return true
	default:
		return false
	}
}

func (e *extractor) isFrameworkContextCall(call *sitter.Node) bool {
	typeName := e.receiverType(call)
	return typeName == "github.com/gin-gonic/gin.Context" || isFiberContextType(typeName)
}

func (e *extractor) isFrameworkRouterCall(call *sitter.Node) bool {
	typeName := e.receiverType(call)
	return typeName == "github.com/gin-gonic/gin.Engine" || typeName == "github.com/gin-gonic/gin.RouterGroup" ||
		strings.HasSuffix(typeName, ".App") && strings.HasPrefix(typeName, "github.com/gofiber/fiber/") ||
		strings.HasSuffix(typeName, ".Group") && strings.HasPrefix(typeName, "github.com/gofiber/fiber/")
}

func isFiberContextType(typeName string) bool {
	return strings.HasPrefix(typeName, "github.com/gofiber/fiber/") && strings.HasSuffix(typeName, ".Ctx")
}

func routeMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE",
		"Get", "Post", "Put", "Patch", "Delete", "Head", "Options", "Connect", "Trace", "Any", "All", "Handle", "Add":
		return true
	default:
		return false
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || value == "_" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstOutput(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return []string{values[0]}
}
