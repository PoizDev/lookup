package graph

import (
	"sort"
)

type GraphStats struct {
	NodeCount      int
	EdgeCount      int
	FunctionCount  int
	TypeCount      int
	FileCount      int
	TopCalledFuncs []NodeCallCount
}

type NodeCallCount struct {
	Node      *Node
	CallCount int
}

func (g *Graph) CallChain(fn NodeID, depth int) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[NodeID]bool)
	var result []*Node

	// BFS queue
	queue := []struct {
		id NodeID
		d  int
	}{{fn, 0}}
	visited[fn] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.d > depth {
			continue
		}

		node, ok := g.Nodes[curr.id]
		if ok && curr.id != fn {
			result = append(result, node)
		}

		edges := g.AdjList[curr.id]
		for _, e := range edges {
			if e.Kind == EdgeCalls && !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, struct {
					id NodeID
					d  int
				}{e.To, curr.d + 1})
			}
		}
	}

	return result
}

func (g *Graph) CalledBy(fn NodeID) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Node
	edges := g.RevAdj[fn]
	visited := make(map[NodeID]bool)

	for _, e := range edges {
		if e.Kind == EdgeCalls && !visited[e.From] {
			visited[e.From] = true
			if node, ok := g.Nodes[e.From]; ok {
				result = append(result, node)
			}
		}
	}
	return result
}

func (g *Graph) SymbolsByKind(kind NodeKind) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Node
	for _, node := range g.Nodes {
		if node.Kind == kind {
			result = append(result, node)
		}
	}
	return result
}

func (g *Graph) DataFlowPath(src, dst NodeID) [][]*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var paths [][]*Node
	maxDepth := 10

	// BFS with path tracking
	type state struct {
		id   NodeID
		path []*Node
	}

	startNode, ok := g.Nodes[src]
	if !ok {
		return nil
	}

	queue := []state{{src, []*Node{startNode}}}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.id == dst && len(curr.path) > 1 {
			// Found a path
			pathCopy := make([]*Node, len(curr.path))
			copy(pathCopy, curr.path)
			paths = append(paths, pathCopy)
			continue
		}

		if len(curr.path) > maxDepth {
			continue
		}

		for _, e := range g.AdjList[curr.id] {
			if e.Kind == EdgeCalls {
				// Avoid cycles
				cycle := false
				for _, n := range curr.path {
					if n.ID == e.To {
						cycle = true
						break
					}
				}
				if cycle {
					continue
				}

				nextNode, ok := g.Nodes[e.To]
				if ok {
					newPath := make([]*Node, len(curr.path), len(curr.path)+1)
					copy(newPath, curr.path)
					newPath = append(newPath, nextNode)
					queue = append(queue, state{e.To, newPath})
				}
			}
		}
	}

	return paths
}

func (g *Graph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := GraphStats{
		NodeCount: len(g.Nodes),
		EdgeCount: len(g.Edges),
	}

	callCounts := make(map[NodeID]int)

	for _, node := range g.Nodes {
		switch node.Kind {
		case NodeFunction, NodeMethod:
			stats.FunctionCount++
		case NodeType:
			stats.TypeCount++
		case NodeFile:
			stats.FileCount++
		}
	}

	for _, edge := range g.Edges {
		if edge.Kind == EdgeCalls {
			callCounts[edge.To]++
		}
	}

	var topCalls []NodeCallCount
	for id, count := range callCounts {
		if node, ok := g.Nodes[id]; ok {
			topCalls = append(topCalls, NodeCallCount{
				Node:      node,
				CallCount: count,
			})
		}
	}

	sort.Slice(topCalls, func(i, j int) bool {
		return topCalls[i].CallCount > topCalls[j].CallCount
	})

	if len(topCalls) > 10 {
		topCalls = topCalls[:10]
	}
	stats.TopCalledFuncs = topCalls

	return stats
}
