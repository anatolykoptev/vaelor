package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// ConvertToCallGraph converts typed edges to the existing CallGraph format by
// matching callers/callees against tree-sitter symbols by name and file.
func ConvertToCallGraph(typedEdges []goanalysis.TypedEdge, tsSymbols []*parser.Symbol) *CallGraph {
	byNameFile, byName := buildConvertIndexes(tsSymbols)

	edges := make([]CallEdge, 0, len(typedEdges))
	for _, te := range typedEdges {
		caller := resolveSymbol(te.CallerName, te.CallerFile, byNameFile, byName)
		callee := resolveSymbol(te.CalleeName, te.CalleeFile, byNameFile, byName)
		edges = append(edges, CallEdge{
			Caller:      caller,
			Callee:      callee,
			CalleeName:  te.CalleeName,
			Receiver:    te.ReceiverType,
			Line:        te.Line,
			IsInterface: te.IsInterface,
		})
	}

	return &CallGraph{
		Edges:   edges,
		Symbols: tsSymbols,
	}
}

// MergeCallGraphs merges a tree-sitter call graph with a typed call graph.
// Typed edges take priority; unmatched tree-sitter edges are appended.
// HookCallbacks are preserved from the tree-sitter graph.
// Returns nil only if both inputs are nil.
func MergeCallGraphs(tsGraph, typedGraph *CallGraph) *CallGraph {
	if typedGraph == nil {
		return tsGraph
	}
	if tsGraph == nil {
		return typedGraph
	}

	// Build dedup key set from typed edges (typed takes priority).
	seen := make(map[string]struct{}, len(typedGraph.Edges))
	for _, e := range typedGraph.Edges {
		seen[edgeKey(e)] = struct{}{}
	}

	// Start with all typed edges; append unmatched tree-sitter edges.
	merged := make([]CallEdge, len(typedGraph.Edges), len(typedGraph.Edges)+len(tsGraph.Edges))
	copy(merged, typedGraph.Edges)
	for _, e := range tsGraph.Edges {
		if _, dup := seen[edgeKey(e)]; !dup {
			merged = append(merged, e)
		}
	}

	symbols := mergeSymbols(typedGraph.Symbols, tsGraph.Symbols)

	return &CallGraph{
		Edges:         merged,
		Symbols:       symbols,
		HookCallbacks: tsGraph.HookCallbacks,
	}
}

// buildConvertIndexes creates lookup maps for symbol resolution during conversion.
func buildConvertIndexes(symbols []*parser.Symbol) (byNameFile, byName map[string]*parser.Symbol) {
	byNameFile = make(map[string]*parser.Symbol, len(symbols))
	byName = make(map[string]*parser.Symbol, len(symbols))
	for _, sym := range symbols {
		nf := sym.Name + ":" + filepath.Base(sym.File)
		if _, exists := byNameFile[nf]; !exists {
			byNameFile[nf] = sym
		}
		if _, exists := byName[sym.Name]; !exists {
			byName[sym.Name] = sym
		}
	}
	return byNameFile, byName
}

// resolveSymbol looks up a symbol by name+file, falling back to name only.
func resolveSymbol(name, file string, byNameFile, byName map[string]*parser.Symbol) *parser.Symbol {
	if name == "" {
		return nil
	}
	if file != "" {
		key := name + ":" + filepath.Base(file)
		if sym, ok := byNameFile[key]; ok {
			return sym
		}
	}
	return byName[name]
}

// edgeKey returns a deduplication key for a CallEdge.
func edgeKey(e CallEdge) string {
	callerName := ""
	if e.Caller != nil {
		callerName = e.Caller.Name
	}
	return callerName + "->" + e.CalleeName
}

// mergeSymbols merges two symbol slices, deduplicating by "name:file".
func mergeSymbols(primary, secondary []*parser.Symbol) []*parser.Symbol {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	result := make([]*parser.Symbol, 0, len(primary)+len(secondary))

	for _, sym := range primary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	for _, sym := range secondary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	return result
}
