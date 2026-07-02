package goanalysis

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/tools/go/packages"
)

// TypedEdge is a call edge with full type information from the Go compiler.
type TypedEdge struct {
	CallerName   string // function containing the call
	CallerFile   string // absolute path
	CallerLine   uint32 // line of the caller function definition
	CalleeName   string // called function/method name
	CalleeFile   string // absolute path (empty if external)
	CalleePkg    string // package path of callee
	ReceiverType string // receiver type for method calls (empty for functions)
	Line         uint32 // line of the call site
	IsInterface  bool   // true if this is interface dispatch
}

// concreteTypes maps interface types to their concrete implementations.
type concreteTypes map[*types.Interface][]*types.Named

// funcValueAliasEdgesTotal is a burn-in counter for the func-value-alias
// shape-class below: every CALLS edge resolveIdent/resolveSelector produce
// by resolving through a *types.Var's single static func-valued initializer
// (var-func-binding or method-value dispatch), rather than a direct
// *types.Func use, bumps this. The shape is conservative-by-construction
// (single static initializer, no reassignment anywhere) so a wrong resolve
// should be rare — but a dead-code tool's false-negative ("not dead" when it
// actually is) is a worse trust failure than a false-positive, so this is a
// human spot-check signal, not a correctness gate. See the callgraph-seam
// unification plan (2026-07-02), ADR risk "func-value-alias shape
// over-resolves a multi-assigned var".
var funcValueAliasEdgesTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gocode_goanalysis_func_value_alias_edges_total",
	Help: "Count of CALLS edges resolved via the func-value-alias shape-class (var-func-binding / method-value dispatch through a package-level var's single static initializer).",
})

// Resolve walks loaded packages and extracts type-aware call edges.
func Resolve(pkgs []*packages.Package) []TypedEdge {
	concrete := collectConcreteTypes(pkgs)
	aliases := collectFuncValueAliases(pkgs)
	var edges []TypedEdge

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			edges = append(edges, extractFileEdges(pkg, file, concrete, aliases)...)
		}
	}
	return edges
}

// funcValueAliases maps a *types.Var with a single static function-valued
// initializer to the underlying *types.Func it aliases — e.g.
// `var workFn = realWork` (var-func-binding) or `var greetFn =
// defaultGreeter.Greet` (method-value dispatch). resolveIdent/resolveSelector
// resolve a call through such a var to this underlying func instead of
// dropping the edge.
//
// Conservative by construction, matching the resolver's existing
// no-edge-over-wrong-edge posture: only a package-level var declaration with
// EXACTLY one name and one value (`var a = f`, never `var a, b = f, g`)
// qualifies, its RHS must be a bare func identifier or a non-interface
// method-value selector (not a call, not a func literal, not any other
// expression), and the var must never be the target of a plain assignment
// anywhere in the loaded packages (a reassignment means the single static
// initializer no longer describes every call through the var). Anything
// ambiguous resolves to no alias, same as today's behavior for any other
// unresolved callee — no edge, never a guessed one.
type funcValueAliases map[*types.Var]*types.Func

func collectFuncValueAliases(pkgs []*packages.Package) funcValueAliases {
	aliases := make(funcValueAliases)
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			collectVarFuncInitializers(pkg.TypesInfo, file, aliases)
		}
	}
	if len(aliases) == 0 {
		return aliases
	}

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			dropReassignedAliases(pkg.TypesInfo, file, aliases)
		}
	}
	return aliases
}

// collectVarFuncInitializers scans file's top-level `var` declarations for
// single-name/single-value specs whose RHS is a func-valued expression, and
// records each as a candidate alias. Only package-level (file.Decls)
// declarations qualify — a function-local `:=` binding is a different,
// broader shape (reassignment/shadowing analysis this conservative pass does
// not attempt) and is deliberately left unresolved, same as today.
func collectVarFuncInitializers(info *types.Info, file *ast.File, aliases funcValueAliases) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue // parallel/multi-value decl — ambiguous, no alias
			}
			obj, ok := info.Defs[vs.Names[0]].(*types.Var)
			if !ok {
				continue
			}
			fn := funcValueExpr(info, vs.Values[0])
			if fn == nil {
				continue
			}
			aliases[obj] = fn
		}
	}
}

// funcValueExpr returns the *types.Func a func-valued expression statically
// refers to — a bare function identifier or a non-interface method-value
// selector — nil for anything else (a call, a func literal, a composite
// literal, an interface-typed method value, ...). Interface method values are
// deliberately excluded: resolving one precisely needs the same
// concrete-type fan-out resolveInterfaceDispatch already performs for direct
// interface calls, which is a different, broader shape than this
// single-conservative-class pass.
func funcValueExpr(info *types.Info, expr ast.Expr) *types.Func {
	switch e := expr.(type) {
	case *ast.Ident:
		fn, _ := info.Uses[e].(*types.Func)
		return fn

	case *ast.SelectorExpr:
		selection, ok := info.Selections[e]
		if !ok {
			// Qualified package-level func (pkg.Func), not a method value.
			fn, _ := info.Uses[e.Sel].(*types.Func)
			return fn
		}
		fn, ok := selection.Obj().(*types.Func)
		if !ok {
			return nil // field access, not a method value
		}
		if isInterfaceReceiver(selection.Recv()) {
			return nil
		}
		return fn

	default:
		return nil
	}
}

// isInterfaceReceiver reports whether t (after stripping one pointer
// indirection) is an interface type.
func isInterfaceReceiver(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// dropReassignedAliases removes any alias whose var is the target of a plain
// assignment (`x = ...`) anywhere in file, outside its own declaration — a
// reassigned var's declaration-time initializer no longer describes every
// call through it, so resolving through the alias would risk a wrong edge.
// Assignment targets land in info.Uses (not info.Defs, which is only for new
// declarations via `var`/`:=`), so this only ever matches a genuine
// reassignment, never the declaration itself.
func dropReassignedAliases(info *types.Info, file *ast.File, aliases funcValueAliases) {
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN {
			return true
		}
		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if v, ok := info.Uses[id].(*types.Var); ok {
				delete(aliases, v)
			}
		}
		return true
	})
}

// collectConcreteTypes maps each interface type to all named non-interface types
// that implement it, across all loaded packages.
func collectConcreteTypes(pkgs []*packages.Package) concreteTypes {
	named := gatherNamedNonInterface(pkgs)
	result := make(concreteTypes)
	for _, pkg := range pkgs {
		mapInterfaceImpls(pkg, named, result)
	}
	return result
}

// gatherNamedNonInterface collects all named non-interface types from all packages.
func gatherNamedNonInterface(pkgs []*packages.Package) []*types.Named {
	var named []*types.Named
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, n := range scope.Names() {
			obj := scope.Lookup(n)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named_, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if _, isIface := named_.Underlying().(*types.Interface); !isIface {
				named = append(named, named_)
			}
		}
	}
	return named
}

// mapInterfaceImpls finds all interfaces in pkg and maps them to implementing types.
func mapInterfaceImpls(pkg *packages.Package, named []*types.Named, result concreteTypes) {
	if pkg.Types == nil {
		return
	}
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		obj := scope.Lookup(n)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named_, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		iface, ok := named_.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for _, cn := range named {
			if types.Implements(cn, iface) || types.Implements(types.NewPointer(cn), iface) {
				result[iface] = append(result[iface], cn)
			}
		}
	}
}

func extractFileEdges(pkg *packages.Package, file *ast.File, concrete concreteTypes, aliases funcValueAliases) []TypedEdge {
	fset := pkg.Fset
	info := pkg.TypesInfo
	var edges []TypedEdge

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		callLine := uint32(fset.Position(call.Pos()).Line) //nolint:gosec // line numbers are always positive
		callerName, callerFile, callerLine := enclosingFunc(fset, file, call.Pos())

		switch fn := unwrapGenericInstantiation(call.Fun).(type) {
		case *ast.Ident:
			edges = append(edges, resolveIdent(info, fset, fn, callerName, callerFile, callerLine, callLine, aliases)...)

		case *ast.SelectorExpr:
			edges = append(edges, resolveSelector(info, fset, pkg, fn, callerName, callerFile, callerLine, callLine, concrete, aliases)...)
		}

		return true
	})
	return edges
}

// unwrapGenericInstantiation strips the type-argument wrapper off a generic
// function instantiation's callee expression. A call like `Fn[T](x)` has
// call.Fun == *ast.IndexExpr{X: Fn, Index: T} (single type arg), and
// `pkg.Fn[T1, T2](x)` has call.Fun == *ast.IndexListExpr{X: pkg.Fn, Indices:
// [T1, T2]} (2+ type args). Neither is an *ast.Ident/*ast.SelectorExpr, so
// without unwrapping, extractFileEdges' callee switch silently drops every
// generic-instantiation call site — the callee's underlying Ident/Selector
// (matching info.Uses to the generic decl) is what resolveIdent/
// resolveSelector expect.
func unwrapGenericInstantiation(fun ast.Expr) ast.Expr {
	for {
		switch e := fun.(type) {
		case *ast.IndexExpr:
			fun = e.X
		case *ast.IndexListExpr:
			fun = e.X
		default:
			return fun
		}
	}
}

// enclosingFunc finds the function declaration, or failing that the
// package-level var/const declaration, containing pos. A call inside a
// package-level var/const initializer (e.g. `var g = &T{f: pkg.Fn[K,V](n)}`)
// has no enclosing *ast.FuncDecl — falling through to "" previously produced
// an edge with an empty CallerName, which downstream symbol resolution
// (ConvertToCallGraph.resolveSymbol) treats as unresolved (nil Caller),
// hiding real callers of a generic constructor used only from package-level
// state (the go-code cache package's NewLRU is the confirmed repro). The
// var/const name matches the tree-sitter parser's KindVar/KindConst Symbol
// name (first name of the first spec — see internal/parser/handler_go.go
// mapVar/mapConst), so it resolves to a real symbol downstream, not a stub.
func enclosingFunc(fset *token.FileSet, file *ast.File, pos token.Pos) (name, file_ string, line uint32) {
	file_ = fset.Position(pos).Filename
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body != nil && d.Pos() <= pos && pos <= d.Body.End() {
				line = uint32(fset.Position(d.Pos()).Line) //nolint:gosec // line numbers are always positive
				name = d.Name.Name
				return
			}

		case *ast.GenDecl:
			if d.Tok != token.VAR && d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 {
					continue
				}
				if vs.Pos() <= pos && pos <= vs.End() {
					line = uint32(fset.Position(vs.Pos()).Line) //nolint:gosec // line numbers are always positive
					name = vs.Names[0].Name
					return
				}
			}
		}
	}
	return
}
