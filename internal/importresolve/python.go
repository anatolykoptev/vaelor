package importresolve

import (
	"path/filepath"
	"strings"
)

// Python modules are files (`pkg/mod.py`, `pkg/__init__.py`), not the
// package directories Resolver maps imports to, and dotted names never carry a
// "/" for localPkgDir's suffix match to bite on. PythonModuleMatches is the
// file-level counterpart: pure string work over paths already in hand, so the
// call graph can use it without a Resolver (it has no repo root or file set).

// pyModuleParts returns the dotted-module components of a Python source file
// path ("/r/pkg/mod.py" -> [r pkg mod], "/r/pkg/__init__.py" -> [r pkg]).
func pyModuleParts(file string) []string {
	p := filepath.ToSlash(filepath.Clean(file))
	if !strings.HasSuffix(p, ".py") {
		return nil
	}
	p = strings.TrimSuffix(p, ".py")
	p = strings.TrimSuffix(p, "/__init__")
	return strings.Split(strings.Trim(p, "/"), "/")
}

// PythonModuleMatches reports whether classFile is the source of module as
// imported from importerFile. module is the dotted path as written in the
// import; leading dots mark a relative import (one dot = the importer's
// package). An absolute module matches by trailing path components, so
// `providers.open_ai` matches both `<root>/providers/open_ai.py` and
// `<root>/src/providers/open_ai.py` (Python source roots are not known here);
// a relative module must name exactly the sibling/parent file.
func PythonModuleMatches(module, importerFile, classFile string) bool {
	have := pyModuleParts(classFile)
	if len(have) == 0 || module == "" {
		return false
	}
	dots := len(module) - len(strings.TrimLeft(module, "."))
	rest := strings.Split(strings.TrimLeft(module, "."), ".")
	if rest[0] == "" {
		rest = nil
	}
	if dots == 0 {
		return hasTrailing(have, rest)
	}
	base := pyModuleParts(importerFile)
	if len(base) == 0 {
		return false
	}
	// The importer's package is its directory (drop the module component,
	// unless the importer is itself an __init__.py, already trimmed above).
	if !strings.HasSuffix(filepath.ToSlash(importerFile), "/__init__.py") {
		base = base[:len(base)-1]
	}
	up := dots - 1
	if up > len(base) {
		return false
	}
	want := append(append([]string{}, base[:len(base)-up]...), rest...)
	return len(want) == len(have) && hasTrailing(have, want)
}

// hasTrailing reports whether have ends with the components of want.
func hasTrailing(have, want []string) bool {
	if len(want) == 0 || len(want) > len(have) {
		return false
	}
	off := len(have) - len(want)
	for i, w := range want {
		if have[off+i] != w {
			return false
		}
	}
	return true
}
