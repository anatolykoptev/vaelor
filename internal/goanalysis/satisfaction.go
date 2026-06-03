package goanalysis

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// Satisfaction is one structural interface-satisfaction fact: the named concrete
// type Type (declared in TypeFile) satisfies the named interface Interface.
// Computed via go/types, which Go's structural typing makes invisible to
// tree-sitter (no `implements` keyword).
//
// Names are bare (package qualifier stripped), matching how the codegraph Symbol
// vertices are keyed (name + file). TypeFile is an absolute path from the FileSet;
// the caller maps it to repo-relative form for the graph key. The interface
// endpoint is resolved by the codegraph caller via the symbol table (name lookup
// in buildRelationshipEdges), so the interface's declaration file is not carried
// here.
type Satisfaction struct {
	Type      string // concrete (non-interface) named type, e.g. "GitHubForge"
	TypeFile  string // absolute path of the concrete type's declaration
	Interface string // interface named type, e.g. "Forge"
}

// ComputeSatisfactions returns every (concrete type, interface) pair across the
// loaded package set where the type — by value OR by pointer — implements the
// interface. The empty interface (zero methods) is skipped: every type trivially
// satisfies it, so emitting those edges would be pure noise and O(types) blow-up.
//
// Scope is bounded to the loaded packages (the module's own ./...), NOT the
// transitive dependency universe: gatherNamedNonInterface and the interface walk
// both iterate only pkgs, so a type T is checked only against interfaces declared
// in the same loaded set. This keeps the satisfaction loop at
// O(localTypes × localInterfaces) rather than O(types × universe).
//
// The pairing logic mirrors mapInterfaceImpls (which builds the call-resolution
// concreteTypes map and discards the type→interface direction); both share the
// types.Implements(T, I) || types.Implements(*T, I) test.
func ComputeSatisfactions(pkgs []*packages.Package) []Satisfaction {
	named := gatherNamedNonInterface(pkgs)
	if len(named) == 0 {
		return nil
	}

	// Build a package→FileSet lookup so an object declared in any loaded package
	// can be resolved to its absolute filename. Packages loaded in one
	// packages.Load call share a FileSet, but resolving through the declaring
	// package's Fset is correct regardless.
	fsetOf := fsetByPackage(pkgs)

	var out []Satisfaction
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, n := range scope.Names() {
			ifaceNamed, iface, ok := namedInterface(scope.Lookup(n))
			if !ok {
				continue
			}
			// Skip the empty interface: every type satisfies it.
			if iface.NumMethods() == 0 {
				continue
			}
			ifaceName := ifaceNamed.Obj().Name()
			for _, cn := range named {
				if !implementsByValueOrPointer(cn, iface) {
					continue
				}
				out = append(out, Satisfaction{
					Type:      cn.Obj().Name(),
					TypeFile:  objFile(cn, fsetOf),
					Interface: ifaceName,
				})
			}
		}
	}
	return out
}

// fsetByPackage maps each loaded package's *types.Package to its *token.FileSet
// so an object's position can be resolved to a filename via its declaring package.
func fsetByPackage(pkgs []*packages.Package) map[*types.Package]*token.FileSet {
	m := make(map[*types.Package]*token.FileSet, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Types != nil && pkg.Fset != nil {
			m[pkg.Types] = pkg.Fset
		}
	}
	return m
}

// namedInterface returns the *types.Named and its *types.Interface underlying
// when obj is a TypeName whose type is a named interface; ok=false otherwise.
func namedInterface(obj types.Object) (*types.Named, *types.Interface, bool) {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, nil, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, nil, false
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return nil, nil, false
	}
	return named, iface, true
}

// implementsByValueOrPointer reports whether the named type satisfies iface
// either by value receiver or by pointer receiver. Pointer-receiver methods are
// the common Go idiom (mutating methods), so checking only the value type would
// miss most real implementations.
func implementsByValueOrPointer(cn *types.Named, iface *types.Interface) bool {
	return types.Implements(cn, iface) || types.Implements(types.NewPointer(cn), iface)
}

// objFile returns the absolute file path where a named type's object is declared,
// resolved through the declaring package's FileSet. Returns "" when the position
// is invalid or the package's FileSet is unavailable.
func objFile(n *types.Named, fsetOf map[*types.Package]*token.FileSet) string {
	obj := n.Obj()
	if obj == nil {
		return ""
	}
	pos := obj.Pos()
	if !pos.IsValid() {
		return ""
	}
	fset := fsetOf[obj.Pkg()]
	if fset == nil {
		return ""
	}
	return fset.Position(pos).Filename
}
