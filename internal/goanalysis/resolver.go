package goanalysis

import (
	"go/ast"
	"go/token"
	"go/types"

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

// Resolve walks loaded packages and extracts type-aware call edges.
func Resolve(pkgs []*packages.Package) []TypedEdge {
	concrete := collectConcreteTypes(pkgs)
	var edges []TypedEdge

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			edges = append(edges, extractFileEdges(pkg, file, concrete)...)
		}
	}
	return edges
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

func extractFileEdges(pkg *packages.Package, file *ast.File, concrete concreteTypes) []TypedEdge {
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

		switch fn := call.Fun.(type) {
		case *ast.Ident:
			edges = append(edges, resolveIdent(info, fset, fn, callerName, callerFile, callerLine, callLine)...)

		case *ast.SelectorExpr:
			edges = append(edges, resolveSelector(info, fset, pkg, fn, callerName, callerFile, callerLine, callLine, concrete)...)
		}

		return true
	})
	return edges
}

// enclosingFunc finds the function declaration containing pos.
func enclosingFunc(fset *token.FileSet, file *ast.File, pos token.Pos) (name, file_ string, line uint32) {
	file_ = fset.Position(pos).Filename
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Body != nil && fd.Pos() <= pos && pos <= fd.Body.End() {
			line = uint32(fset.Position(fd.Pos()).Line) //nolint:gosec // line numbers are always positive
			name = fd.Name.Name
			return
		}
	}
	return
}
