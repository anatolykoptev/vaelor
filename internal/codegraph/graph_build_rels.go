package codegraph

import (
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/importresolve"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/strutil"
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
// importresolve.Resolver (via localPkgDir for Go-style imports, resolveRelative for
// TS/JS-style "./x"/"../x" imports, and alias resolution for $lib/@scope when cfg
// is provided) maps each import back to its container dir so both edge kinds land
// on one node. fileSet is the set of all indexed file paths, needed to resolve a
// relative import to its target file's directory.
func buildImportsGraph(pkgDirs, fileSet map[string]struct{}, fileImports map[string][]string, cfg importresolve.Config) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData
	importedPkgs := make(map[string]bool)

	r := importresolve.New(pkgDirs, fileSet, cfg)

	for relFile, imports := range fileImports {
		importingDir := filepath.Dir(relFile)
		for _, imp := range imports {
			toKey := imp
			localDir, isLocal := r.Resolve(imp, importingDir)
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
		score := strutil.CommonPrefixLen(filepath.Dir(s.File), refDir)
		if score > bestScore {
			bestScore = score
			best = s
		}
	}
	return best
}
