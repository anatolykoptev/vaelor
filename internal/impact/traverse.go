package impact

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// findTarget returns the first function/method with the given name.
func findTarget(symbols []*parser.Symbol, name string) *parser.Symbol {
	// A "Receiver.Name" query matches by receiver too, through the matcher
	// call_trace and understand share, so "OpenAi.transcribe" works the same
	// way "Type.Method" does for Go.
	if strings.Contains(name, ".") {
		if matches := callgraph.FindSymbols(symbols, name); len(matches) > 0 {
			return matches[0]
		}
		return nil
	}
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

// buildCallerIndex creates a reverse map: callee → edges where it's called.
func buildCallerIndex(edges []callgraph.CallEdge) map[*parser.Symbol][]callgraph.CallEdge {
	idx := make(map[*parser.Symbol][]callgraph.CallEdge)
	for _, e := range edges {
		if e.Callee != nil {
			idx[e.Callee] = append(idx[e.Callee], e)
		}
	}
	return idx
}

type bfsItem struct {
	sym   *parser.Symbol
	depth int
}

// traverseCallers runs BFS from target through the caller index,
// populating result.DirectCallers and result.TransitiveCallers.
// Returns the set of affected packages.
func traverseCallers(target *parser.Symbol, callerIndex map[*parser.Symbol][]callgraph.CallEdge,
	communityMap map[*parser.Symbol]int, maxDepth int, result *Result) map[string]bool {
	visited := map[*parser.Symbol]bool{target: true}
	queue := []bfsItem{{target, 0}}
	pkgSet := make(map[string]bool)

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		for _, edge := range callerIndex[item.sym] {
			caller := edge.Caller
			if caller == nil || visited[caller] {
				continue
			}
			visited[caller] = true

			distance := item.depth + 1
			affected := makeAffected(caller, distance, communityMap, edge.Line)
			pkgSet[affected.Package] = true

			if distance == 1 {
				result.DirectCallers = append(result.DirectCallers, affected)
			} else {
				result.TransitiveCallers = append(result.TransitiveCallers, affected)
			}

			queue = append(queue, bfsItem{caller, distance})
		}
	}

	return pkgSet
}

// makeAffected creates an AffectedSymbol with confidence decaying by distance.
// callLine is the 1-based line of the discovering edge — for a direct caller
// the call site of the target inside the caller's file.
func makeAffected(sym *parser.Symbol, distance int, communityMap map[*parser.Symbol]int, callLine uint32) AffectedSymbol {
	confidence := 1.0 - float64(distance-1)*confidenceDecayPerHop
	if confidence < minConfidence {
		confidence = minConfidence
	}
	comm := 0
	if communityMap != nil {
		comm = communityMap[sym]
	}
	return AffectedSymbol{
		Name:       sym.Name,
		File:       sym.File,
		Package:    filepath.Dir(sym.File),
		Distance:   distance,
		Confidence: confidence,
		Community:  comm,
		CallLine:   callLine,
	}
}

func classifyBlastRadius(callers, packages int) string {
	if callers == 0 {
		return "none"
	}
	if callers <= lowMaxCallers && packages <= lowMaxPackages {
		return "low"
	}
	if callers <= medMaxCallers && packages <= medMaxPackages {
		return "medium"
	}
	return "high"
}
