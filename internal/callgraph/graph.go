// Package callgraph builds and queries call relationships between functions.
package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// CallEdge is a resolved (or unresolved) call from one function to another.
type CallEdge struct {
	Caller      *parser.Symbol // function containing the call
	Callee      *parser.Symbol // target function (nil if unresolved)
	CalleeName  string         // original name from source
	Receiver    string         // qualifier if method call
	Line        uint32         // 1-based call site line
	IsInterface bool           // true when resolved via interface dispatch (go/types or SCIP)
}

// CallGraph holds all call relationships for a repository.
type CallGraph struct {
	Edges         []CallEdge
	Symbols       []*parser.Symbol
	TypeRels      []parser.TypeRelationship // interface/extends/embeds relationships
	HookCallbacks []string                  // function names registered as hook callbacks
	Tier          string                    // "basic" (tree-sitter), "enhanced" (go/types merged), "full" (future)
	Backend       string                    // resolution backend: "tree-sitter", "tree-sitter+go/types", "tree-sitter+scip"
	// Warm records the go/types warm state this graph was built under
	// (issue #746). It is stamped ONCE before the graph enters cgCache and
	// is never mutated on a cached entry — when the per-root registry
	// (goTypesWarm) disagrees with the stamp, callers get a shallow copy
	// with the live state instead. The authority is the registry; this
	// field is the rendered note's payload so consumers never consult the
	// registry themselves.
	//
	//   - ""         (WarmNone)    — no warm pending: enhanced, or the
	//     synchronous load succeeded with zero typed edges (basic-final)
	//   - "warming"  (WarmPending) — a background go/types load is in
	//     flight; a retry will return the enhanced tier
	//   - "failed"   (WarmFailed)  — the last warm failed durably; a retry
	//     returns the same basic-tier graph until the failed record
	//     expires (cgCacheTTL)
	//
	// Use WarmNote to render the agent-facing string for a state.
	Warm WarmState
	// WarmCause is stamped alongside Warm when the stamp is WarmPending:
	// which wait (priming export data vs queued for the memory budget)
	// degraded this graph, so WarmNote can say the real reason (issue
	// #894). Same write-once rules as Warm; empty when the degrade was
	// neither wait or the entry predates the cause field.
	WarmCause WarmCause
	// UsesIndex maps a target file's relative path to a list of relative paths
	// of Astro files that render it as a component (<Foo />). Populated by
	// ResolveTemplateRefs during BuildFromRepo. Enables impact_analysis to
	// report file-level USES callers for Astro components.
	UsesIndex map[string][]string
}

// BuildOpts controls how parser.CallSite entries are converted into call
// graph edges.
type BuildOpts struct {
	// IncludeFieldAccess keeps heuristic argref/field-access call sites even
	// when they don't resolve to a known function symbol. Default false —
	// unresolved CallSite.IsArgRef entries are dropped to avoid reporting
	// vars (`ctx`, `localPath`) and member access (`opts.Slug`) as callees.
	IncludeFieldAccess bool
}

// BuildCallGraph resolves call sites against the symbol table.
// Resolution: same-file -> same-package (directory) -> global name match.
//
// Heuristic argref sites (parser.CallSite.IsArgRef==true) are dropped when
// they don't resolve to a function/method symbol — this filters noise like
// member access (`opts.Slug`) and local variable references (`ctx`,
// `localPath`) that the parser captures inside argument lists.
func BuildCallGraph(symbols []*parser.Symbol, calls []parser.CallSite) *CallGraph {
	return BuildCallGraphWithOpts(symbols, calls, BuildOpts{})
}

// BuildCallGraphWithOpts is BuildCallGraph with explicit options.
func BuildCallGraphWithOpts(symbols []*parser.Symbol, calls []parser.CallSite, opts BuildOpts) *CallGraph {
	byName := indexByName(symbols)
	byFile := indexByFile(symbols)
	byDir := indexByDir(symbols)

	edges := make([]CallEdge, 0, len(calls))
	for i := range calls {
		cs := &calls[i]
		caller := findCaller(byFile[cs.File], cs.Line)
		callee := resolveCall(cs, byFile, byDir, byName)

		if cs.IsArgRef {
			if callee != nil {
				recordCallee(cs.File, "argref_kept")
			} else if opts.IncludeFieldAccess {
				recordCallee(cs.File, "argref_kept_legacy")
			} else {
				// Unresolved argref — drop to avoid reporting member access
				// and locals as callees. This is the noise filter.
				recordCallee(cs.File, "argref_dropped_unresolved")
				continue
			}
		} else {
			recordCallee(cs.File, "call")
		}

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

// resolveCall finds the target symbol. Priority: same file -> same dir ->
// global UNIQUE name match.
//
// No edge over a wrong edge (issue #793): the global tier emits only an
// unambiguous name. A bare `x.Close()`/`main()` matching 2+ same-named repo
// symbols resolves to nil rather than the previous closest-directory guess —
// the guess bound every caller to one arbitrary candidate, which is how 8
// distinct `Close` methods merged 349 callers into one answer and poisoned
// PageRank/who_calls/surprises. Unqualified calls to language builtins
// (`len`, `append`, `print`, …) skip the global tier entirely: a repo
// symbol that shares a builtin's name is not the call's target. Same-file
// and same-package resolution still run first, so a package that genuinely
// shadows a builtin keeps working.
func resolveCall(cs *parser.CallSite, byFile, byDir, byName map[string][]*parser.Symbol) *parser.Symbol {
	if parser.DetectLanguageFromPath(cs.File) == langGo {
		return resolveGoCall(cs, byFile, byDir, byName)
	}
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

	// Unqualified language builtins never resolve cross-package (issue
	// #793): `len(x)` is not a call to the repo's own `len` — that edge is
	// what gave a random set.go:len thousands of phantom callers.
	if cs.Receiver == "" && isBuiltinCallName(parser.DetectLanguageFromPath(cs.File), name) {
		return nil
	}

	// Global: only when the name is unique across the whole repo (issue
	// #793). A closest-dir pick among 2+ same-named functions silently
	// merges call sites of unrelated `Close`/`main`/`handle` symbols —
	// the edge poisons PageRank, who_calls and surprises far more than a
	// missing edge does.
	if cands := byName[name]; len(cands) == 1 {
		return cands[0]
	}

	return nil
}

func findByName(symbols []*parser.Symbol, name string) *parser.Symbol {
	return findByNameWhere(symbols, name, func(sym *parser.Symbol) bool {
		return sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod
	})
}

// findByNameWhere returns the first symbol called name that satisfies ok.
func findByNameWhere(symbols []*parser.Symbol, name string, ok func(*parser.Symbol) bool) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && ok(sym) {
			return sym
		}
	}
	return nil
}

const langGo = "go"

// resolveGoCall is resolveCall under Go's scoping rules. The generic resolver
// binds a site to a same-named symbol with no regard for what the syntax can
// denote, which for the heuristic argument-position / struct-literal captures
// (@call.argref, @call.argref_method) meant a LOCAL VARIABLE `f` passed as an
// argument bound to the repo's only method called `f`, and a field read
// `Run: p.Run` bound to the repo's only function called `Run`. Typed
// resolution does not cover those positions, and MergeCallGraphs appends every
// tree-sitter edge the typed pass did not produce, so the enhanced tier carried
// them too. The rules below are language-spec facts, not heuristics:
//
//   - a bare identifier (no qualifier) names a package-level function of its
//     OWN package — never a method (Go has no implicit receiver) and never a
//     symbol of another package (no dot-imports are modelled);
//   - a qualified `x.Name` whose Name is a package-level function of the SAME
//     package cannot be a call or reference to it (a package never imports
//     itself), so x is a value and Name a field or method;
//   - a qualified reference to a function of ANOTHER package must be spelled
//     with that package's name (the directory base approximates it), or it is
//     a value selector that merely shares the function's name. Plain call
//     sites keep the older permissive behaviour here: import aliases are
//     legal, and in the enhanced tier the typed edge decides.
//
// A missing edge beats a wrong edge (issue #793).
func resolveGoCall(cs *parser.CallSite, byFile, byDir, byName map[string][]*parser.Symbol) *parser.Symbol {
	callDir := filepath.Dir(cs.File)
	samePackage := func(sym *parser.Symbol) bool { return goSamePackageTarget(cs, sym) }

	if found := findByNameWhere(byFile[cs.File], cs.Name, samePackage); found != nil {
		return found
	}
	if found := findByNameWhere(byDir[callDir], cs.Name, samePackage); found != nil {
		return found
	}
	if cs.Receiver == "" {
		return nil
	}
	cands := byName[cs.Name]
	if len(cands) != 1 || filepath.Dir(cands[0].File) == callDir {
		return nil
	}
	if sym := cands[0]; goCrossPackageTarget(cs, sym) {
		return sym
	}
	return nil
}

// goSamePackageTarget reports whether sym, declared in the call site's own
// package, can be what cs denotes: a bare identifier names a function, a
// qualified one names a method (the qualifier is a value).
func goSamePackageTarget(cs *parser.CallSite, sym *parser.Symbol) bool {
	if cs.Receiver == "" {
		return sym.Kind == parser.KindFunction
	}
	return sym.Kind == parser.KindMethod
}

// goCrossPackageTarget reports whether sym, declared in another package, can be
// what the qualified site cs denotes. Methods are reached through any value
// qualifier. A function needs a qualifier naming its package for heuristic
// reference captures; plain call sites keep the permissive name match.
func goCrossPackageTarget(cs *parser.CallSite, sym *parser.Symbol) bool {
	switch sym.Kind {
	case parser.KindMethod:
		return true
	case parser.KindFunction:
		return !cs.IsArgRef || filepath.Base(filepath.Dir(sym.File)) == cs.Receiver
	}
	return false
}
