package impact

import "path/filepath"

// appendUsesCallers populates result.DirectCallers from the UsesIndex for
// file-level USES relationships (Astro component imports).
//
// symbolName is treated as a relative file path (e.g. "src/components/Foo.astro").
// Each entry in UsesIndex[symbolName] is an Astro file that renders the target
// as a component and is reported as a direct caller with full confidence.
func appendUsesCallers(usesIndex map[string][]string, symbolName string, result *Result) {
	callers, ok := usesIndex[symbolName]
	if !ok {
		return
	}
	pkgSet := make(map[string]bool)
	for _, caller := range callers {
		pkg := filepath.Dir(caller)
		pkgSet[pkg] = true
		result.DirectCallers = append(result.DirectCallers, AffectedSymbol{
			Name:       caller,
			File:       caller,
			Package:    pkg,
			Distance:   1,
			Confidence: 1.0,
		})
	}
	result.TotalAffected = len(result.DirectCallers)
	for pkg := range pkgSet {
		result.AffectedPackages = append(result.AffectedPackages, pkg)
	}
}
