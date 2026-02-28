// Package callgraph builds and queries call relationships between functions.
package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// CallEdge is a resolved (or unresolved) call from one function to another.
type CallEdge struct {
	Caller     *parser.Symbol // function containing the call
	Callee     *parser.Symbol // target function (nil if unresolved)
	CalleeName string         // original name from source
	Receiver   string         // qualifier if method call
	Line       uint32         // 1-based call site line
}

// CallGraph holds all call relationships for a repository.
type CallGraph struct {
	Edges   []CallEdge
	Symbols []*parser.Symbol
}

// BuildCallGraph resolves call sites against the symbol table.
// Resolution: same-file -> same-package (directory) -> global name match.
func BuildCallGraph(symbols []*parser.Symbol, calls []parser.CallSite) *CallGraph {
	byName := indexByName(symbols)
	byFile := indexByFile(symbols)
	byDir := indexByDir(symbols)

	edges := make([]CallEdge, 0, len(calls))
	for i := range calls {
		cs := &calls[i]
		caller := findCaller(byFile[cs.File], cs.Line)
		callee := resolveCall(cs, byFile, byDir, byName)
		edges = append(edges, CallEdge{
			Caller:     caller,
			Callee:     callee,
			CalleeName: cs.Name,
			Receiver:   cs.Receiver,
			Line:       cs.Line,
		})
	}
	return &CallGraph{Edges: edges, Symbols: symbols}
}

// findCaller returns the narrowest function/method containing the given line.
func findCaller(fileSymbols []*parser.Symbol, line uint32) *parser.Symbol {
	var best *parser.Symbol
	for _, sym := range fileSymbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if line >= sym.StartLine && line <= sym.EndLine {
			if best == nil || (sym.EndLine-sym.StartLine) < (best.EndLine-best.StartLine) {
				best = sym
			}
		}
	}
	return best
}

// resolveCall finds the target symbol. Priority: same file -> same dir -> global.
func resolveCall(cs *parser.CallSite, byFile, byDir, byName map[string][]*parser.Symbol) *parser.Symbol {
	name := cs.Name

	if syms, ok := byFile[cs.File]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	dir := filepath.Dir(cs.File)
	if syms, ok := byDir[dir]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	if syms, ok := byName[name]; ok && len(syms) > 0 {
		callerDir := filepath.Dir(cs.File)
		return closestSymbol(syms, callerDir)
	}

	return nil
}

func findByName(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

func closestSymbol(symbols []*parser.Symbol, dir string) *parser.Symbol {
	if len(symbols) == 0 {
		return nil
	}
	best := symbols[0]
	bestLen := commonPrefixLen(filepath.Dir(best.File), dir)
	for _, sym := range symbols[1:] {
		cl := commonPrefixLen(filepath.Dir(sym.File), dir)
		if cl > bestLen {
			bestLen = cl
			best = sym
		}
	}
	return best
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func indexByName(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			m[sym.Name] = append(m[sym.Name], sym)
		}
	}
	return m
}

func indexByFile(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		m[sym.File] = append(m[sym.File], sym)
	}
	return m
}

func indexByDir(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		dir := filepath.Dir(sym.File)
		m[dir] = append(m[dir], sym)
	}
	return m
}
