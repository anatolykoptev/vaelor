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

// InjectHookEdges adds synthetic call edges for WordPress hook connections.
//
// For each hook name that has both a registration (add_action/add_filter,
// Side="server") and an invocation (do_action/apply_filters, Side="client"),
// it creates a CallEdge from the invoking function to the callback function.
//
// hookRoutes must come from routes.ExtractAll("php", ...) and symbols from
// the same repository's parsed symbol table.
func InjectHookEdges(cg *CallGraph, hookRoutes []HookRoute) {
	// Index server-side: hook name -> []callback name
	callbacks := make(map[string][]string)
	for _, r := range hookRoutes {
		if r.Side == "server" && r.Handler != "" {
			callbacks[r.Path] = append(callbacks[r.Path], r.Handler)
		}
	}
	if len(callbacks) == 0 {
		return
	}

	// Index symbols by name for resolution.
	byName := indexByName(cg.Symbols)

	// Track which hooks have client-side invocations.
	clientHooks := make(map[string]bool)
	for _, r := range hookRoutes {
		if r.Side == "client" {
			clientHooks[r.Path] = true
		}
	}

	// For each client-side hook invocation, create edges to all registered callbacks.
	for _, r := range hookRoutes {
		if r.Side != "client" {
			continue
		}
		cbs, ok := callbacks[r.Path]
		if !ok {
			continue
		}
		for _, cbName := range cbs {
			targets := byName[cbName]
			if len(targets) == 0 {
				// Unresolved callback — still record the edge.
				cg.Edges = append(cg.Edges, CallEdge{
					CalleeName: cbName,
					Line:       r.Line,
				})
				continue
			}
			for _, target := range targets {
				cg.Edges = append(cg.Edges, CallEdge{
					Callee:     target,
					CalleeName: cbName,
					Line:       r.Line,
				})
			}
		}
	}

	// Second pass: inject edges for server-only hooks (no client-side invocation
	// in the analyzed repo). These are typically WordPress core hooks like
	// admin_notices, init, enqueue_block_editor_assets, etc. — the do_action()
	// call lives in WordPress core, not in the plugin code. The add_action()
	// registration itself proves the callback is alive.
	for hookName, cbNames := range callbacks {
		if clientHooks[hookName] {
			continue // Already handled by client→server matching above.
		}
		for _, cbName := range cbNames {
			targets := byName[cbName]
			if len(targets) == 0 {
				cg.Edges = append(cg.Edges, CallEdge{
					CalleeName: cbName,
				})
				continue
			}
			for _, target := range targets {
				cg.Edges = append(cg.Edges, CallEdge{
					Callee:     target,
					CalleeName: cbName,
				})
			}
		}
	}
}

// HookRoute is a minimal representation of a WordPress hook route used by
// InjectHookEdges. It mirrors the fields needed from routes.Route without
// importing the routes package (to avoid circular dependencies).
type HookRoute struct {
	Method  string // "ACTION" or "FILTER"
	Path    string // hook name
	Handler string // callback function name (empty for client-side)
	Side    string // "server" or "client"
	Line    uint32
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
