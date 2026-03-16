package goanalysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

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

func resolveIdent(info *types.Info, fset *token.FileSet, id *ast.Ident, callerName, callerFile string, callerLine, callLine uint32) []TypedEdge {
	obj, ok := info.Uses[id]
	if !ok {
		return nil
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	calleeFile := posFile(fset, fn.Pos())
	return []TypedEdge{{
		CallerName: callerName,
		CallerFile: callerFile,
		CallerLine: callerLine,
		CalleeName: fn.Name(),
		CalleeFile: calleeFile,
		CalleePkg:  fn.Pkg().Path(),
		Line:       callLine,
	}}
}

func resolveSelector(info *types.Info, fset *token.FileSet, pkg *packages.Package, sel *ast.SelectorExpr, callerName, callerFile string, callerLine, callLine uint32, concrete concreteTypes) []TypedEdge {
	// Try method call via Selections.
	if selection, ok := info.Selections[sel]; ok {
		return resolveMethodSelection(info, fset, pkg, sel, selection, callerName, callerFile, callerLine, callLine, concrete)
	}

	// Qualified name (pkg.Func) via Uses.
	obj, ok := info.Uses[sel.Sel]
	if !ok {
		return nil
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	calleeFile := posFile(fset, fn.Pos())
	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}
	return []TypedEdge{{
		CallerName: callerName,
		CallerFile: callerFile,
		CallerLine: callerLine,
		CalleeName: fn.Name(),
		CalleeFile: calleeFile,
		CalleePkg:  pkgPath,
		Line:       callLine,
	}}
}

func resolveMethodSelection(info *types.Info, fset *token.FileSet, pkg *packages.Package, sel *ast.SelectorExpr, selection *types.Selection, callerName, callerFile string, callerLine, callLine uint32, concrete concreteTypes) []TypedEdge {
	recv := selection.Recv()
	recvType := typeName(recv)
	fn := selection.Obj().(*types.Func) //nolint:forcetypeassert // Selections always have Func obj
	calleeFile := posFile(fset, fn.Pos())
	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}

	// Check if receiver is an interface — dispatch to concrete types.
	underlying := recv
	if ptr, ok := underlying.(*types.Pointer); ok {
		underlying = ptr.Elem()
	}
	if iface, ok := underlying.Underlying().(*types.Interface); ok {
		return resolveInterfaceDispatch(info, fset, pkg, sel, iface, fn.Name(), callerName, callerFile, callerLine, callLine, recvType, concrete)
	}

	return []TypedEdge{{
		CallerName:   callerName,
		CallerFile:   callerFile,
		CallerLine:   callerLine,
		CalleeName:   fn.Name(),
		CalleeFile:   calleeFile,
		CalleePkg:    pkgPath,
		ReceiverType: recvType,
		Line:         callLine,
	}}
}

func resolveInterfaceDispatch(_ *types.Info, fset *token.FileSet, _ *packages.Package, _ *ast.SelectorExpr, iface *types.Interface, methodName, callerName, callerFile string, callerLine, callLine uint32, recvType string, concrete concreteTypes) []TypedEdge {
	impls, ok := concrete[iface]
	if !ok {
		return []TypedEdge{{
			CallerName:   callerName,
			CallerFile:   callerFile,
			CallerLine:   callerLine,
			CalleeName:   methodName,
			ReceiverType: recvType,
			Line:         callLine,
			IsInterface:  true,
		}}
	}

	edges := make([]TypedEdge, 0, len(impls))
	for _, impl := range impls {
		calleeFile := ""
		pkgPath := ""
		// Find the method on the concrete type.
		for i := range impl.NumMethods() {
			m := impl.Method(i)
			if m.Name() == methodName {
				calleeFile = posFile(fset, m.Pos())
				if m.Pkg() != nil {
					pkgPath = m.Pkg().Path()
				}
				break
			}
		}
		edges = append(edges, TypedEdge{
			CallerName:   callerName,
			CallerFile:   callerFile,
			CallerLine:   callerLine,
			CalleeName:   methodName,
			CalleeFile:   calleeFile,
			CalleePkg:    pkgPath,
			ReceiverType: typeName(impl),
			Line:         callLine,
			IsInterface:  true,
		})
	}
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

// typeName returns a human-readable type name, stripping pointer indirection.
func typeName(t types.Type) string {
	switch v := t.(type) {
	case *types.Pointer:
		return typeName(v.Elem())
	case *types.Named:
		return v.Obj().Name()
	default:
		s := t.String()
		if idx := strings.LastIndex(s, "."); idx >= 0 {
			return s[idx+1:]
		}
		return s
	}
}

// posFile returns the absolute filename for a token position.
func posFile(fset *token.FileSet, pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	return fset.Position(pos).Filename
}
