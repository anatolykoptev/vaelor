package codegraph

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildImportsGraph creates IMPORTS edges and external Package vertices from
// the fileImports map. A local package is NOT duplicated: its IMPORTS edge points
// at the container Package vertex (vertexKey = its path = the repo-relative dir),
// the same node buildGraph creates and hangs CONTAINS→File edges off. Only
// external imports get their own Package vertex.
//
// Why this needs more than a map lookup: pkgDirs is keyed by repo-relative dir
// (e.g. "internal/fleet/docker"), but a Go import is the full module path
// (e.g. "github.com/x/y/internal/fleet/docker"). A bare `pkgDirs[imp]` lookup
// therefore MISSES every Go local import, so the package would be created twice —
// once as the dir-keyed container (CONTAINS, no IMPORTS) and once as an
// import-path-keyed "external" node (IMPORTS, no CONTAINS) — fragmenting the
// package graph into two disconnected halves bridged only by base name.
// localPkgDir resolves the import back to its container dir so both edge kinds
// land on one node.
func buildImportsGraph(pkgDirs map[string]struct{}, fileImports map[string][]string) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData
	importedPkgs := make(map[string]bool)

	// Index local package dirs by base name for fast suffix resolution.
	pkgDirByBase := make(map[string][]string, len(pkgDirs))
	for d := range pkgDirs {
		base := filepath.Base(d)
		pkgDirByBase[base] = append(pkgDirByBase[base], d)
	}

	for relFile, imports := range fileImports {
		for _, imp := range imports {
			toKey := imp
			localDir, isLocal := localPkgDir(imp, pkgDirs, pkgDirByBase)
			if isLocal {
				toKey = localDir // point at the container vertex, not a duplicate
			}

			edges = append(edges, edgeData{
				FromLabel: "File",
				FromKey:   relFile,
				ToLabel:   "Package",
				ToKey:     toKey,
				EdgeLabel: "IMPORTS",
				Props:     map[string]string{},
			})

			if importedPkgs[imp] {
				continue
			}
			importedPkgs[imp] = true

			if isLocal {
				continue // container vertex already created by buildGraph
			}
			vertices = append(vertices, vertexData{
				Label: "Package",
				Props: map[string]string{
					"name": filepath.Base(imp),
					"path": imp,
					"repo": "external",
				},
			})
		}
	}

	return vertices, edges
}

// localPkgDir resolves an import path to the repo-relative package dir it refers
// to, when that package is indexed in this repo. Returns ("", false) for external
// imports.
//
// Matching: an import is local when it equals a pkgDir (languages whose imports
// are already repo-relative) OR ends with "/"+pkgDir (Go's full module paths).
// The longest matching dir wins so the most specific package is chosen. Indexed
// by base name so the scan is O(#dirs sharing the import's base), usually 1.
//
// Limitation: a dependency that literally vendors an identical relative path
// (e.g. some/other/module/internal/fleet/docker) would suffix-match. That
// collision is pathological for repo-internal packages and accepted here to keep
// the indexer language-agnostic (no go.mod parsing).
func localPkgDir(imp string, pkgDirs map[string]struct{}, pkgDirByBase map[string][]string) (string, bool) {
	if _, ok := pkgDirs[imp]; ok {
		return imp, true
	}
	best := ""
	for _, d := range pkgDirByBase[filepath.Base(imp)] {
		if strings.HasSuffix(imp, "/"+d) && len(d) > len(best) {
			best = d
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// buildRelationshipEdges resolves type relationships against the symbol table
// and creates INHERITS or IMPLEMENTS edges.
func buildRelationshipEdges(root string, rels []parser.TypeRelationship, symbols []*parser.Symbol) []edgeData {
	byName := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		byName[s.Name] = append(byName[s.Name], s)
	}

	var edges []edgeData
	for _, r := range rels {
		targets, ok := byName[r.Target]
		if !ok || len(targets) == 0 {
			continue
		}

		subjectRelFile := relPath(r.File, root)
		targetSym := closestByDir(targets, r.File)
		targetRelFile := relPath(targetSym.File, root)

		edgeLabel := edgeLabelInherits
		if r.Kind == parser.RelImplements {
			edgeLabel = edgeLabelImplements
		}

		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   r.Subject + compositeKeyDelim + subjectRelFile,
			ToLabel:   "Symbol",
			ToKey:     targetSym.Name + compositeKeyDelim + targetRelFile,
			EdgeLabel: edgeLabel,
			Props:     map[string]string{},
		})
	}

	return edges
}

// closestByDir returns the symbol from candidates closest to refFile by directory.
func closestByDir(candidates []*parser.Symbol, refFile string) *parser.Symbol {
	if len(candidates) == 1 {
		return candidates[0]
	}
	refDir := filepath.Dir(refFile)
	best := candidates[0]
	bestScore := 0
	for _, s := range candidates {
		score := commonPrefixLen(filepath.Dir(s.File), refDir)
		if score > bestScore {
			bestScore = score
			best = s
		}
	}
	return best
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
