package codegraph

import (
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildImportsGraph creates IMPORTS edges and external Package vertices from
// the fileImports map. Local packages (already in pkgDirs) are not duplicated.
func buildImportsGraph(pkgDirs map[string]struct{}, fileImports map[string][]string) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData
	importedPkgs := make(map[string]bool)

	for relFile, imports := range fileImports {
		for _, imp := range imports {
			edges = append(edges, edgeData{
				FromLabel: "File",
				FromKey:   relFile,
				ToLabel:   "Package",
				ToKey:     imp,
				EdgeLabel: "IMPORTS",
				Props:     map[string]string{},
			})

			if importedPkgs[imp] {
				continue
			}
			importedPkgs[imp] = true

			if _, isLocal := pkgDirs[imp]; isLocal {
				continue
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
			FromKey:   r.Subject + ":" + subjectRelFile,
			ToLabel:   "Symbol",
			ToKey:     targetSym.Name + ":" + targetRelFile,
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
