package codegraph

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
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
// tplRefs contains Astro template component usages that produce USES edges.
func buildGraph(root string, files []*ingest.File, symbols []*parser.Symbol, cg *callgraph.CallGraph, fileImports map[string][]string, rels []parser.TypeRelationship, tplRefs []templateFileRef) ([]vertexData, []edgeData) {
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

	// USES edges (File→File) from Astro template component references.
	for _, ref := range tplRefs {
		edges = append(edges, edgeData{
			FromLabel: "File",
			FromKey:   ref.relFile,
			ToLabel:   "File",
			ToKey:     ref.name, // unresolved: tag name only (resolution is a v2 TODO)
			EdgeLabel: "USES",
			Props:     map[string]string{"line": strconv.Itoa(int(ref.line)), "unresolved": "true"},
		})
	}

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
