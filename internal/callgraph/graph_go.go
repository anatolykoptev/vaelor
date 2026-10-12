package callgraph

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anatolykoptev/vaelor/internal/fsutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

const (
	langGo = "go"

	// goHeaderLimit bounds how much of a Go file is read to find its package
	// clause and import block, which always sit at the top.
	goHeaderLimit = 64 << 10

	goTestPkgSuffix = "_test"
	goDotImport     = "."
	goBlankImport   = "_"
)

// goImport is one import spec of a file. alias is "" when the spec has none.
type goImport struct {
	alias string
	path  string
}

// goFileScope is what a Go file's header says about the names it can use.
type goFileScope struct {
	pkg     string // declared package clause name
	imports []goImport
}

// goResolver resolves Go call sites under Go's scoping rules. The generic
// resolver binds a site to a same-named symbol with no regard for what the
// syntax can denote, which for the heuristic argument-position / struct-literal
// captures (@call.argref, @call.argref_method) meant a LOCAL VARIABLE `f`
// passed as an argument bound to the repo's only method called `f`, and a field
// read `Run: p.Run` bound to the repo's only function called `Run`. Typed
// resolution does not cover those positions, and MergeCallGraphs appends every
// tree-sitter edge the typed pass did not produce, so the enhanced tier carried
// them too.
//
// The rules are language-spec facts, read from each file's own header (package
// clause + import specs, parsed with go/parser) rather than guessed from
// directory names:
//
//   - a bare identifier names a function of its OWN package (same directory AND
//     same package clause: `package x_test` is a different package from `x`,
//     which it reaches through an ordinary import of the same directory),
//     or an exported function of a dot-imported package; never a method;
//   - a qualified `x.Name` whose Name is a function must have x name an
//     imported package: the import's alias, else the imported package's
//     declared name (which differs from the directory base for `foo-go` or
//     `vx/v2`). A function of the caller's own package is never reachable
//     through a qualifier, so `p.Run` with p a value is not a call of Run;
//   - a method is reached through any value qualifier.
//
// When a file's header cannot be read (synthetic symbols, a refused file), the
// resolver falls back to directory-name matching. A missing edge beats a wrong
// edge (issue #793).
type goResolver struct {
	byFile, byDir, byName map[string][]*parser.Symbol
	scopes                map[string]*goFileScope // nil value = unreadable
}

func newGoResolver(byFile, byDir, byName map[string][]*parser.Symbol) *goResolver {
	return &goResolver{byFile: byFile, byDir: byDir, byName: byName, scopes: map[string]*goFileScope{}}
}

// scope returns the header scope of file, or nil when it cannot be read.
func (g *goResolver) scope(file string) *goFileScope {
	if sc, ok := g.scopes[file]; ok {
		return sc
	}
	sc := readGoScope(file)
	g.scopes[file] = sc
	return sc
}

func readGoScope(file string) *goFileScope {
	src, err := fsutil.ReadRepoFilePrefix(filepath.Dir(file), filepath.Base(file), goHeaderLimit)
	if err != nil {
		return nil
	}
	f, err := goparser.ParseFile(token.NewFileSet(), "", src, goparser.ImportsOnly)
	if f == nil || f.Name == nil {
		_ = err
		return nil
	}
	sc := &goFileScope{pkg: f.Name.Name}
	for _, spec := range f.Imports {
		imp := goImport{path: unquoteImport(spec)}
		if spec.Name != nil {
			imp.alias = spec.Name.Name
		}
		sc.imports = append(sc.imports, imp)
	}
	return sc
}

func unquoteImport(spec *ast.ImportSpec) string {
	p, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return strings.Trim(spec.Path.Value, "\"`")
	}
	return p
}

// pkgName is the declared package name of file, "" when unknown.
func (g *goResolver) pkgName(file string) string {
	if sc := g.scope(file); sc != nil {
		return sc.pkg
	}
	return ""
}

// samePackage reports whether sym lives in the call site's own package: same
// directory and, when both headers are readable, the same package clause.
func (g *goResolver) samePackage(cs *parser.CallSite, sym *parser.Symbol) bool {
	if filepath.Dir(sym.File) != filepath.Dir(cs.File) {
		return false
	}
	a, b := g.pkgName(cs.File), g.pkgName(sym.File)
	return a == "" || b == "" || a == b
}

// samePackageFamily is samePackage widened to a package and its external test
// package (`x` / `x_test`, same directory). Used for METHODS reached through a
// value qualifier: without types the receiver's package is unknown, and a
// test of x calling `reg.Register(...)` on a value built from x targets x's
// method, not some other package's namesake.
func (g *goResolver) samePackageFamily(cs *parser.CallSite, sym *parser.Symbol) bool {
	if g.samePackage(cs, sym) {
		return true
	}
	if filepath.Dir(sym.File) != filepath.Dir(cs.File) {
		return false
	}
	a, b := g.pkgName(cs.File), g.pkgName(sym.File)
	return a == b+goTestPkgSuffix || b == a+goTestPkgSuffix
}

func (g *goResolver) resolve(cs *parser.CallSite) *parser.Symbol {
	callDir := filepath.Dir(cs.File)
	bare := cs.Receiver == ""

	// Same package: a bare identifier names a function, a qualified one a
	// method (its qualifier is a value).
	wantKind := parser.KindMethod
	if bare {
		wantKind = parser.KindFunction
	}
	inPackage := func(sym *parser.Symbol) bool {
		if sym.Kind != wantKind {
			return false
		}
		if bare {
			return g.samePackage(cs, sym)
		}
		return g.samePackageFamily(cs, sym)
	}
	if found := findByNameWhere(g.byFile[cs.File], cs.Name, inPackage); found != nil {
		return found
	}
	if found := findByNameWhere(g.byDir[callDir], cs.Name, inPackage); found != nil {
		return found
	}

	scope := g.scope(cs.File)
	if bare {
		return g.resolveDotImported(cs, scope)
	}
	return g.resolveQualified(cs, scope)
}

// resolveDotImported resolves a bare identifier through `import . "pkg"`: the
// exported functions of the imported packages are in file scope. Resolves only
// when exactly one dot-imported package declares the name.
func (g *goResolver) resolveDotImported(cs *parser.CallSite, scope *goFileScope) *parser.Symbol {
	if scope == nil || !isExported(cs.Name) {
		return nil
	}
	var hit *parser.Symbol
	for _, imp := range scope.imports {
		if imp.alias != goDotImport {
			continue
		}
		for _, sym := range g.byName[cs.Name] {
			if sym.Kind != parser.KindFunction || !dirMatchesImport(filepath.Dir(sym.File), imp.path) {
				continue
			}
			if hit != nil && hit != sym {
				return nil
			}
			hit = sym
		}
	}
	return hit
}

// resolveQualified resolves `q.Name` to a function of an imported package, or
// to a method reached through the value q.
func (g *goResolver) resolveQualified(cs *parser.CallSite, scope *goFileScope) *parser.Symbol {
	callDir := filepath.Dir(cs.File)
	cands := g.byName[cs.Name]

	if scope != nil {
		var hit *parser.Symbol
		for _, sym := range cands {
			if sym.Kind != parser.KindFunction || g.samePackage(cs, sym) || !g.importNames(scope, sym, cs.Receiver) {
				continue
			}
			if hit != nil {
				return nil
			}
			hit = sym
		}
		if hit != nil {
			return hit
		}
	}

	// Methods are reached through any value qualifier; without types the only
	// defensible binding is the repo-wide unique one in another package.
	if len(cands) != 1 || filepath.Dir(cands[0].File) == callDir {
		return nil
	}
	sym := cands[0]
	switch {
	case sym.Kind == parser.KindMethod:
		return sym
	case sym.Kind == parser.KindFunction && scope == nil:
		// Header unreadable: plain calls keep the permissive name match (alias
		// imports are legal), heuristic references need the directory name.
		if !cs.IsArgRef || filepath.Base(filepath.Dir(sym.File)) == cs.Receiver {
			return sym
		}
	}
	return nil
}

// importNames reports whether scope imports sym's package under the name q:
// the spec's alias, else the package's declared name.
func (g *goResolver) importNames(scope *goFileScope, sym *parser.Symbol, q string) bool {
	dir := filepath.Dir(sym.File)
	declared := g.pkgName(sym.File)
	if declared == "" {
		declared = filepath.Base(dir)
	}
	for _, imp := range scope.imports {
		if imp.alias == goDotImport || imp.alias == goBlankImport || !dirMatchesImport(dir, imp.path) {
			continue
		}
		name := imp.alias
		if name == "" {
			name = declared
		}
		if name == q {
			return true
		}
	}
	return false
}

// dirMatchesImport reports whether the import path ends with the directory's
// trailing path elements (the module prefix is not known here, so at least the
// last element must agree).
func dirMatchesImport(dir, importPath string) bool {
	d := strings.Split(filepath.ToSlash(dir), "/")
	p := strings.Split(importPath, "/")
	n := 0
	for i, j := len(d)-1, len(p)-1; i >= 0 && j >= 0 && d[i] == p[j]; i, j = i-1, j-1 {
		n++
	}
	return n > 0
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}
