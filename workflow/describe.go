package workflow

import (
	"context"
	"maps"
	"slices"
)

// GraphDescription is a readonly topology snapshot, not an executable checkpoint.
type GraphDescription struct {
	FormatVersion string
	Nodes         []NodeDescription
	Edges         []EdgeDescription
}

// NodeDescription lists stable node and explicit fork identities.
type NodeDescription struct {
	ID, Kind string
	Branches []string
}

// EdgeDescription distinguishes unconditional and candidate conditional edges.
type EdgeDescription struct{ From, To, Kind string }

// Describe validates a graph and returns a detached, deterministically ordered topology.
func (g *Graph[T]) Describe() (GraphDescription, error) {
	compiled, err := g.compile(context.Background())
	if err != nil {
		return GraphDescription{}, err
	}
	return describe(compiled), nil
}
func describe[T any](g *compiled[T]) GraphDescription {
	out := GraphDescription{FormatVersion: "goagent-graph-v1"}
	for _, id := range slices.Sorted(maps.Keys(g.nodes)) {
		node := NodeDescription{ID: id, Kind: "node"}
		if p, ok := g.parallel[id]; ok {
			node.Kind = "parallel"
			for _, b := range p.Branches {
				node.Branches = append(node.Branches, b.Name)
			}
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, from := range slices.Sorted(maps.Keys(g.edges)) {
		out.Edges = append(out.Edges, EdgeDescription{From: from, To: g.edges[from], Kind: "edge"})
	}
	for _, from := range slices.Sorted(maps.Keys(g.branches)) {
		for _, to := range slices.Sorted(maps.Keys(g.branches[from].targets)) {
			out.Edges = append(out.Edges, EdgeDescription{From: from, To: to, Kind: "branch"})
		}
	}
	return out
}
