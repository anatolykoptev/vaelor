package ranking

import "github.com/anatolykoptev/go-code/internal/parser"

// RefGraphInput provides data for building the file-level reference graph.
type RefGraphInput struct {
	Symbols     []*parser.Symbol
	Calls       []parser.CallSite
	ImportEdges map[string][]string // file→[]file from import resolution
}

// RefGraph is a weighted directed graph of file-to-file reference edges.
type RefGraph struct {
	edges map[string]map[string]float64
}

// BuildRefGraph constructs a file-level reference graph by resolving call sites
// to target symbol files and merging with import edges.
func BuildRefGraph(input RefGraphInput) *RefGraph {
	g := &RefGraph{edges: make(map[string]map[string]float64)}
	defIndex := buildDefIndex(input.Symbols)
	g.addCallEdges(input.Calls, defIndex)
	g.addImportEdges(input.ImportEdges)
	return g
}

// Weight returns edge weight from src to dst (0 if no edge).
func (g *RefGraph) Weight(src, dst string) float64 {
	if targets, ok := g.edges[src]; ok {
		return targets[dst]
	}
	return 0
}

// Adjacency returns the internal weighted adjacency map.
func (g *RefGraph) Adjacency() map[string]map[string]float64 {
	return g.edges
}

// Len returns total directed edge count.
func (g *RefGraph) Len() int {
	n := 0
	for _, targets := range g.edges {
		n += len(targets)
	}
	return n
}

// ToUnweighted converts to the format used by PageRank.
func (g *RefGraph) ToUnweighted() map[string][]string {
	out := make(map[string][]string, len(g.edges))
	for src, targets := range g.edges {
		for dst := range targets {
			out[src] = append(out[src], dst)
		}
	}
	return out
}

func (g *RefGraph) addEdge(src, dst string, weight float64) {
	if src == dst {
		return
	}
	if g.edges[src] == nil {
		g.edges[src] = make(map[string]float64)
	}
	g.edges[src][dst] += weight
}

// buildDefIndex maps symbol name to list of files defining it.
func buildDefIndex(symbols []*parser.Symbol) map[string][]string {
	idx := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if seen[sym.Name] == nil {
			seen[sym.Name] = make(map[string]bool)
		}
		if !seen[sym.Name][sym.File] {
			seen[sym.Name][sym.File] = true
			idx[sym.Name] = append(idx[sym.Name], sym.File)
		}
	}
	return idx
}

// addCallEdges resolves each call to its definition file(s).
// Weight: 1.0 / len(definers) to distribute credit for ambiguous names.
func (g *RefGraph) addCallEdges(calls []parser.CallSite, defIndex map[string][]string) {
	for _, cs := range calls {
		definers := defIndex[cs.Name]
		if len(definers) == 0 {
			continue
		}
		weight := 1.0 / float64(len(definers))
		for _, defFile := range definers {
			g.addEdge(cs.File, defFile, weight)
		}
	}
}

func (g *RefGraph) addImportEdges(importEdges map[string][]string) {
	for src, targets := range importEdges {
		for _, dst := range targets {
			g.addEdge(src, dst, 1.0)
		}
	}
}
