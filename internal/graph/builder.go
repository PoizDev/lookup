package graph

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/poizdev/lookup/internal/semantic"
)

type Builder struct {
	graph         *Graph
	nextEdgeID    int64
	byCaller      map[string][]NodeID
	byName        map[string][]NodeID
	byCanonical   map[string][]NodeID
	pendingCalls  []Call
	callsResolved bool
}

func (b *Builder) AddDocument(document *semantic.Document) {
	b.graph.AddDocument(document)
}

func NewBuilder() *Builder {
	return &Builder{
		graph:       NewGraph(),
		byCaller:    make(map[string][]NodeID),
		byName:      make(map[string][]NodeID),
		byCanonical: make(map[string][]NodeID),
	}
}

func (b *Builder) getEdgeID() EdgeID {
	id := atomic.AddInt64(&b.nextEdgeID, 1)
	return EdgeID(fmt.Sprintf("e%d", id))
}

func (b *Builder) AddFile(filePath string) {
	nodeID := NodeID(filePath)
	b.graph.AddNode(&Node{
		ID:   nodeID,
		Kind: NodeFile,
		Name: filePath,
		File: filePath,
	})
}

func (b *Builder) AddFunctions(fns []Function) {
	for _, fn := range fns {
		var kind NodeKind
		var nodeID NodeID
		var name string
		canonical := canonicalFunctionName(fn.Name, fn.Receiver)
		if fn.Receiver != "" {
			kind = NodeMethod
			name = canonical
			nodeID = NodeID(fmt.Sprintf("%s:%s", fn.File, name))
		} else {
			kind = NodeFunction
			name = fn.Name
			nodeID = NodeID(fmt.Sprintf("%s:%s", fn.File, name))
		}

		b.graph.AddNode(&Node{
			ID:        nodeID,
			Kind:      kind,
			Name:      name,
			File:      fn.File,
			StartLine: fn.StartLine,
			EndLine:   fn.EndLine,
			Metadata: map[string]any{
				"Params":  fn.Params,
				"Returns": fn.Returns,
				"Body":    fn.Body,
			},
		})
		b.byCaller[callerKey(fn.File, canonical)] = appendUniqueNodeID(b.byCaller[callerKey(fn.File, canonical)], nodeID)
		b.byName[fn.Name] = appendUniqueNodeID(b.byName[fn.Name], nodeID)
		b.byCanonical[canonical] = appendUniqueNodeID(b.byCanonical[canonical], nodeID)

		b.graph.AddEdge(&Edge{
			ID:   b.getEdgeID(),
			Kind: EdgeContains,
			From: NodeID(fn.File),
			To:   nodeID,
		})
	}
}

func (b *Builder) AddTypes(types []TypeDef) {
	for _, t := range types {
		nodeID := NodeID(fmt.Sprintf("%s:%s", t.File, t.Name))
		b.graph.AddNode(&Node{
			ID:        nodeID,
			Kind:      NodeType,
			Name:      t.Name,
			File:      t.File,
			StartLine: t.StartLine,
			EndLine:   t.EndLine,
			Metadata: map[string]any{
				"Kind":    t.Kind,
				"Fields":  t.Fields,
				"Methods": t.Methods,
			},
		})

		b.graph.AddEdge(&Edge{
			ID:   b.getEdgeID(),
			Kind: EdgeContains,
			From: NodeID(t.File),
			To:   nodeID,
		})
	}
}

func (b *Builder) AddImports(imports []Import) {
	for _, imp := range imports {
		name := imp.Path
		if imp.Alias != "" {
			name = imp.Alias + ":" + imp.Path
		}
		nodeID := NodeID(fmt.Sprintf("import:%s:%s", imp.File, imp.Path))
		b.graph.AddNode(&Node{
			ID:        nodeID,
			Kind:      NodeImport,
			Name:      name,
			File:      imp.File,
			StartLine: imp.Line,
			EndLine:   imp.Line,
			Metadata: map[string]any{
				"Path":  imp.Path,
				"Alias": imp.Alias,
			},
		})

		b.graph.AddEdge(&Edge{
			ID:   b.getEdgeID(),
			Kind: EdgeImports,
			From: NodeID(imp.File),
			To:   nodeID,
		})
	}
}

func (b *Builder) AddSymbols(symbols []Symbol) {
	for _, s := range symbols {
		if s.Kind == NodeFunction || s.Kind == NodeType || s.Kind == NodeFile {
			continue
		}
		nodeID := NodeID(fmt.Sprintf("%s:%s:%s", s.Kind, s.File, s.Name))
		b.graph.AddNode(&Node{
			ID:        nodeID,
			Kind:      s.Kind,
			Name:      s.Name,
			File:      s.File,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Metadata:  s.Metadata,
		})

		b.graph.AddEdge(&Edge{
			ID:   b.getEdgeID(),
			Kind: EdgeContains,
			From: NodeID(s.File),
			To:   nodeID,
		})
	}
}

func (b *Builder) AddCalls(calls []Call) {
	b.pendingCalls = append(b.pendingCalls, calls...)
}

func (b *Builder) resolveCalls() {
	if b.callsResolved {
		return
	}
	b.callsResolved = true
	calls := append([]Call(nil), b.pendingCalls...)
	sort.SliceStable(calls, func(i, j int) bool {
		left, right := calls[i], calls[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.CallerReceiver != right.CallerReceiver {
			return left.CallerReceiver < right.CallerReceiver
		}
		if left.CallerName != right.CallerName {
			return left.CallerName < right.CallerName
		}
		if left.CalleeReceiver != right.CalleeReceiver {
			return left.CalleeReceiver < right.CalleeReceiver
		}
		return left.CalleeName < right.CalleeName
	})
	for _, call := range calls {
		callers := b.byCaller[callerKey(call.File, canonicalFunctionName(call.CallerName, call.CallerReceiver))]
		var callees []NodeID
		if call.CalleeReceiver != "" {
			callees = b.byCanonical[canonicalFunctionName(call.CalleeName, call.CalleeReceiver)]
		} else {
			for _, candidate := range b.byName[call.CalleeName] {
				node := b.graph.Nodes[candidate]
				if node != nil && node.Kind == NodeFunction && node.File == call.File {
					callees = append(callees, candidate)
				}
			}
		}
		if len(callers) == 1 && len(callees) == 1 {
			b.graph.AddEdge(&Edge{
				ID:   b.getEdgeID(),
				Kind: EdgeCalls,
				From: callers[0],
				To:   callees[0],
			})
		}
	}
}

func canonicalFunctionName(name, receiver string) string {
	receiver = normalizeReceiver(receiver)
	if receiver == "" {
		return name
	}
	return receiver + "." + name
}

func normalizeReceiver(receiver string) string {
	receiver = strings.TrimSpace(strings.Trim(receiver, "()"))
	fields := strings.Fields(receiver)
	if len(fields) > 1 {
		receiver = fields[len(fields)-1]
	}
	return strings.TrimLeft(strings.TrimSpace(receiver), "*")
}

func callerKey(file, canonical string) string { return file + "\x00" + canonical }

func appendUniqueNodeID(ids []NodeID, id NodeID) []NodeID {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func (b *Builder) Build() *Graph {
	b.resolveCalls()
	return b.graph
}
