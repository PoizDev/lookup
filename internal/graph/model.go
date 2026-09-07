package graph

import (
	"fmt"
	"sort"
	"sync"

	"github.com/poizdev/lookup/internal/dataflow"
	"github.com/poizdev/lookup/internal/semantic"
)

type NodeID string
type EdgeID string

type NodeKind int

const (
	NodeFile NodeKind = iota
	NodeFunction
	NodeMethod
	NodeType
	NodeVariable
	NodeConstant
	NodeImport
	NodePackage
)

func (k NodeKind) String() string {
	switch k {
	case NodeFile:
		return "NodeFile"
	case NodeFunction:
		return "NodeFunction"
	case NodeMethod:
		return "NodeMethod"
	case NodeType:
		return "NodeType"
	case NodeVariable:
		return "NodeVariable"
	case NodeConstant:
		return "NodeConstant"
	case NodeImport:
		return "NodeImport"
	case NodePackage:
		return "NodePackage"
	default:
		return fmt.Sprintf("NodeKind(%d)", k)
	}
}

type EdgeKind int

const (
	EdgeCalls EdgeKind = iota
	EdgeImplements
	EdgeContains
	EdgeImports
	EdgeReturns
	EdgeReferences
)

func (k EdgeKind) String() string {
	switch k {
	case EdgeCalls:
		return "EdgeCalls"
	case EdgeImplements:
		return "EdgeImplements"
	case EdgeContains:
		return "EdgeContains"
	case EdgeImports:
		return "EdgeImports"
	case EdgeReturns:
		return "EdgeReturns"
	case EdgeReferences:
		return "EdgeReferences"
	default:
		return fmt.Sprintf("EdgeKind(%d)", k)
	}
}

type Node struct {
	ID        NodeID
	Kind      NodeKind
	Name      string
	File      string
	StartLine int
	EndLine   int
	Metadata  map[string]any
}

type Edge struct {
	ID   EdgeID
	Kind EdgeKind
	From NodeID
	To   NodeID
}

type Graph struct {
	Nodes     map[NodeID]*Node
	Edges     []*Edge
	AdjList   map[NodeID][]*Edge // outgoing edges from a node
	RevAdj    map[NodeID][]*Edge // incoming edges to a node (for CalledBy queries)
	documents map[string]*semantic.Document
	indexOnce sync.Once
	index     *semantic.Index
	flowOnce  sync.Once
	flows     []dataflow.Trace
	mu        sync.RWMutex
}

func NewGraph() *Graph {
	return &Graph{
		Nodes:     make(map[NodeID]*Node),
		Edges:     make([]*Edge, 0),
		AdjList:   make(map[NodeID][]*Edge),
		RevAdj:    make(map[NodeID][]*Edge),
		documents: make(map[string]*semantic.Document),
	}
}

func (g *Graph) AddDocument(document *semantic.Document) {
	if document == nil || document.Path == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.documents[document.Path] = document
	g.indexOnce = sync.Once{}
	g.index = nil
	g.flowOnce = sync.Once{}
	g.flows = nil
}

func (g *Graph) Documents() []*semantic.Document {
	g.mu.RLock()
	defer g.mu.RUnlock()
	documents := make([]*semantic.Document, 0, len(g.documents))
	for _, document := range g.documents {
		documents = append(documents, document)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return documents
}

func (g *Graph) SemanticIndex() *semantic.Index {
	g.indexOnce.Do(func() {
		g.index = semantic.NewIndex(g.Documents())
	})
	return g.index
}

func (g *Graph) DataflowTraces() []dataflow.Trace {
	g.flowOnce.Do(func() {
		g.flows = dataflow.Analyze(g.SemanticIndex(), dataflow.DefaultLimits())
	})
	// The graph is immutable during analysis; callers must treat this cached slice as read-only.
	return g.flows
}

func (g *Graph) AddNode(n *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Nodes[n.ID] = n
}

func (g *Graph) AddEdge(e *Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Edges = append(g.Edges, e)
	g.AdjList[e.From] = append(g.AdjList[e.From], e)
	g.RevAdj[e.To] = append(g.RevAdj[e.To], e)
}

func (g *Graph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.Nodes)
}

func (g *Graph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.Edges)
}

// Mu returns the graph's RWMutex for external read-locking during analysis.
func (g *Graph) Mu() *sync.RWMutex {
	return &g.mu
}

// RawAST is the intermediate representation from tree-sitter parsing
type RawAST struct {
	Language  string
	FilePath  string
	Root      any // tree-sitter root node (opaque to graph package)
	Source    []byte
	HasErrors bool
	closeOnce sync.Once
	closeErr  error
	cleanup   func() error
}

// NewRawAST creates a parsed result with a language-private cleanup hook.
func NewRawAST(language string, root any, source []byte, cleanup func() error) *RawAST {
	return &RawAST{Language: language, Root: root, Source: source, cleanup: cleanup}
}

// Close releases parser-owned resources exactly once.
func (a *RawAST) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		if a.cleanup != nil {
			a.closeErr = a.cleanup()
		}
	})
	return a.closeErr
}

// Extracted symbol types from Language Adapters
type Symbol struct {
	Name      string
	Kind      NodeKind
	File      string
	StartLine int
	EndLine   int
	Metadata  map[string]any
}

type Function struct {
	Name      string
	Receiver  string // empty for plain functions
	Params    []string
	Returns   []string
	File      string
	StartLine int
	EndLine   int
	Body      string // raw source of function body
}

type TypeDef struct {
	Name      string
	Kind      string // "struct", "interface", "enum", etc.
	File      string
	StartLine int
	EndLine   int
	Fields    []string
	Methods   []string
}

type Import struct {
	Path  string
	Alias string
	File  string
	Line  int
}

type Call struct {
	CallerName     string
	CallerReceiver string
	CalleeName     string
	CalleeReceiver string
	File           string
	Line           int
	IsMethod       bool
}
