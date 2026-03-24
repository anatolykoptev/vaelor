package codegraph

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

// Route side constants.
const (
	sideServer = "server"
	sideClient = "client"
)

// Edge label constants.
const (
	edgeLabelInherits   = "INHERITS"
	edgeLabelImplements = "IMPLEMENTS"
)

// buildGraph constructs vertices and edges from ingested files and parsed symbols.
// fileImports maps each file's relative path to the import paths declared in that file.
// rels contains type relationships (embeds/extends/implements) extracted by the parser.
func buildGraph(root string, files []*ingest.File, symbols []*parser.Symbol, cg *callgraph.CallGraph, fileImports map[string][]string, rels []parser.TypeRelationship) ([]vertexData, []edgeData) {
	// Collect unique packages (directories).
	pkgDirs := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		pkgDirs[dir] = struct{}{}
	}

	var vertices []vertexData
	var edges []edgeData

	// Compute PageRank on CALLS graph.
	prScores := computeSymbolPageRank(root, symbols, cg)

	// Package vertices.
	for dir := range pkgDirs {
		vertices = append(vertices, vertexData{
			Label: "Package",
			Props: map[string]string{
				"name": filepath.Base(dir),
				"path": dir,
				"repo": root,
			},
		})
	}

	// File vertices + CONTAINS (pkg→file) edges.
	for _, f := range files {
		lineCount := estimateLines(f)
		vertices = append(vertices, vertexData{
			Label: "File",
			Props: map[string]string{
				"path":     f.RelPath,
				"language": f.Language,
				"lines":    strconv.Itoa(lineCount),
			},
		})

		pkgDir := filepath.Dir(f.RelPath)
		edges = append(edges, edgeData{
			FromLabel: "Package",
			FromKey:   pkgDir,
			ToLabel:   "File",
			ToKey:     f.RelPath,
			EdgeLabel: "CONTAINS",
			Props:     map[string]string{},
		})
	}

	// Symbol vertices + CONTAINS (file→symbol) edges.
	symVerts, symEdges := buildSymbolGraph(root, symbols, prScores)
	vertices = append(vertices, symVerts...)
	edges = append(edges, symEdges...)

	// CALLS edges (Symbol→Symbol).
	for _, ce := range cg.Edges {
		if ce.Caller == nil || ce.Callee == nil {
			continue
		}
		callerRelFile := relPath(ce.Caller.File, root)
		calleeRelFile := relPath(ce.Callee.File, root)
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   ce.Caller.Name + ":" + callerRelFile,
			ToLabel:   "Symbol",
			ToKey:     ce.Callee.Name + ":" + calleeRelFile,
			EdgeLabel: "CALLS",
			Props: map[string]string{
				"line": strconv.Itoa(int(ce.Line)),
			},
		})
	}

	// INHERITS / IMPLEMENTS edges (Symbol→Symbol).
	relEdges := buildRelationshipEdges(root, rels, symbols)
	edges = append(edges, relEdges...)

	// TESTED_BY edges (test Symbol → tested Symbol).
	testedByEdges := ExtractTestedByEdges(root, symbols)
	edges = append(edges, testedByEdges...)

	// IMPORTS edges (File→Package) + external Package vertices.
	impVertices, impEdges := buildImportsGraph(pkgDirs, fileImports)
	vertices = append(vertices, impVertices...)
	edges = append(edges, impEdges...)

	return vertices, edges
}

// buildSymbolGraph creates Symbol vertices and CONTAINS edges from file to symbol.
func buildSymbolGraph(root string, symbols []*parser.Symbol, prScores map[string]float64) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData

	for _, sym := range symbols {
		relFile := relPath(sym.File, root)
		symKey := sym.Name + ":" + relFile
		props := map[string]string{
			"name":       sym.Name,
			"kind":       string(sym.Kind),
			"signature":  sym.Signature,
			"file":       relFile,
			"start_line": strconv.Itoa(int(sym.StartLine)),
			"end_line":   strconv.Itoa(int(sym.EndLine)),
		}
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			lines := 1
			if sym.EndLine >= sym.StartLine {
				lines = int(sym.EndLine-sym.StartLine) + 1
			}
			props["lines"] = strconv.Itoa(lines)
			cc := sym.Complexity
			if cc == 0 && sym.Body != "" {
				cc = parser.Complexity(sym.Body)
			}
			props["complexity"] = strconv.Itoa(cc)
		}
		if score, ok := prScores[symKey]; ok {
			props["pagerank"] = fmt.Sprintf("%.6f", score)
		}
		vertices = append(vertices, vertexData{
			Label: "Symbol",
			Props: props,
		})

		edges = append(edges, edgeData{
			FromLabel: "File",
			FromKey:   relFile,
			ToLabel:   "Symbol",
			ToKey:     symKey,
			EdgeLabel: "CONTAINS",
			Props:     map[string]string{},
		})
	}

	return vertices, edges
}

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
	// Build symbol lookup by name.
	byName := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		byName[s.Name] = append(byName[s.Name], s)
	}

	var edges []edgeData
	for _, r := range rels {
		targets, ok := byName[r.Target]
		if !ok || len(targets) == 0 {
			continue // target not in parsed symbols (external type)
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

// buildCrossLanguageGraph constructs Layer and Route vertices, plus HANDLES
// and FETCHES edges connecting symbols to routes.
func buildCrossLanguageGraph(layers []polyglot.Layer, routeList []routes.Route, fileToLayer map[string]string) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData

	// Layer vertices.
	for _, l := range layers {
		vertices = append(vertices, vertexData{
			Label: "Layer",
			Props: map[string]string{
				"name":     l.Name,
				"role":     l.Role,
				"language": l.Language,
				"root_dir": l.RootDir,
			},
		})
	}

	// Route vertices — deduplicated by Method+":"+Path.
	routeSeen := make(map[string]bool)
	for _, r := range routeList {
		key := r.Method + ":" + r.Path
		if routeSeen[key] {
			continue
		}
		routeSeen[key] = true
		vertices = append(vertices, vertexData{
			Label: "Route",
			Props: map[string]string{
				"method":    r.Method,
				"path":      r.Path,
				"framework": r.Framework,
			},
		})
	}

	// HANDLES / FETCHES edges (Symbol → Route).
	// Match Symbol by name only — the handler may be defined in a different
	// file from the route registration.
	for _, r := range routeList {
		if r.Handler == "" {
			continue
		}
		routeKey := r.Method + ":" + r.Path
		edgeLabel := "HANDLES"
		if r.Side == sideClient {
			edgeLabel = "FETCHES"
		}
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   r.Handler,
			ToLabel:   "Route",
			ToKey:     routeKey,
			EdgeLabel: edgeLabel,
			Props: map[string]string{
				"line": strconv.Itoa(int(r.Line)),
			},
		})
	}

	return vertices, edges
}
