package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

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

// InjectHookEdges adds synthetic call edges for WordPress hook connections.
//
// For each hook name that has both a registration (add_action/add_filter,
// Side="server") and an invocation (do_action/apply_filters, Side="client"),
// it creates a CallEdge from the invoking function to the callback function.
//
// hookRoutes must come from routes.ExtractAll("php", ...) and symbols from
// the same repository's parsed symbol table.
func InjectHookEdges(cg *CallGraph, hookRoutes []HookRoute) {
	callbacks := make(map[string][]string)
	for _, r := range hookRoutes {
		if r.Side == "server" && r.Handler != "" {
			callbacks[r.Path] = append(callbacks[r.Path], r.Handler)
		}
	}
	if len(callbacks) == 0 {
		return
	}

	byName := indexByName(cg.Symbols)

	clientHooks := make(map[string]bool)
	for _, r := range hookRoutes {
		if r.Side == "client" {
			clientHooks[r.Path] = true
		}
	}

	// Client-side hook invocations → edges to all registered callbacks.
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
				cg.Edges = append(cg.Edges, CallEdge{CalleeName: cbName, Line: r.Line})
				continue
			}
			for _, target := range targets {
				cg.Edges = append(cg.Edges, CallEdge{Callee: target, CalleeName: cbName, Line: r.Line})
			}
		}
	}

	var cbCollected []string
	for _, r := range hookRoutes {
		if r.Side == "server" && r.Handler != "" {
			cbCollected = append(cbCollected, r.Handler)
		}
	}
	cg.HookCallbacks = cbCollected

	// Server-only hooks (do_action lives in WP core, not in the plugin code).
	// add_action registration itself proves the callback is alive.
	for hookName, cbNames := range callbacks {
		if clientHooks[hookName] {
			continue
		}
		for _, cbName := range cbNames {
			targets := byName[cbName]
			if len(targets) == 0 {
				cg.Edges = append(cg.Edges, CallEdge{CalleeName: cbName})
				continue
			}
			for _, target := range targets {
				cg.Edges = append(cg.Edges, CallEdge{Callee: target, CalleeName: cbName})
			}
		}
	}
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
	m := make(map[string][]*parser.Symbol, len(symbols))
	for _, sym := range symbols {
		dir := filepath.Dir(sym.File)
		m[dir] = append(m[dir], sym)
	}
	return m
}
