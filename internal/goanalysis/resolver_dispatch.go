package goanalysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

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
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil // field access (Var), not a method call
	}
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
