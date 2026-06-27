package codegraph

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/importresolve"
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
	edgeLabelHandles    = "HANDLES"
)

// buildGraphInput holds all inputs to buildGraph.
// fileImports maps each file's relative path to the import paths declared in that file.
// rels contains type relationships (embeds/extends/implements) extracted by the parser.
// tplRefs contains Astro template component usages that produce USES edges.
type buildGraphInput struct {
	Root        string
	Files       []*ingest.File
	Symbols     []*parser.Symbol
	CallGraph   *callgraph.CallGraph
	FileImports map[string][]string
	Rels        []parser.TypeRelationship
	TplRefs     []templateFileRef
}

// buildGraph constructs vertices and edges from ingested files and parsed symbols.
// It returns the computed PageRank scores so callers can reuse them without a
// second computeSymbolPageRank call.
func buildGraph(in buildGraphInput) ([]vertexData, []edgeData, map[string]float64) {
	// Collect unique packages (directories) and the set of all indexed file paths
	// (fileSet is used to resolve relative TS/JS imports to their target file's dir).
	pkgDirs := make(map[string]struct{})
	fileSet := make(map[string]struct{}, len(in.Files))
	for _, f := range in.Files {
		dir := filepath.Dir(f.RelPath)
		pkgDirs[dir] = struct{}{}
		fileSet[f.RelPath] = struct{}{}
	}

	// Build alias config by walking the repo from disk. BuildConfig discovers
	// svelte.config.* and package.json files directly — ingest drops .json (unknown
	// language), so reading from in.Files would always produce an empty Workspace,
	// making @scope resolution dead in production.
	aliasCfg := importresolve.BuildConfig(in.Root)

	var vertices []vertexData
	var edges []edgeData

	// Compute PageRank on CALLS graph.
	prScores := computeSymbolPageRank(in.Root, in.Symbols, in.CallGraph)

	// Package vertices.
	for dir := range pkgDirs {
		vertices = append(vertices, vertexData{
			Label: "Package",
			Props: map[string]string{
				"name": filepath.Base(dir),
				"path": dir,
				"repo": in.Root,
			},
		})
	}

	// File vertices + CONTAINS (pkg→file) edges.
	for _, f := range in.Files {
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
	symVerts, symEdges := buildSymbolGraph(in.Root, in.Symbols, prScores)
	vertices = append(vertices, symVerts...)
	edges = append(edges, symEdges...)

	// CALLS edges (Symbol→Symbol).
	for _, ce := range in.CallGraph.Edges {
		if ce.Caller == nil || ce.Callee == nil {
			continue
		}
		callerRelFile := relPath(ce.Caller.File, in.Root)
		calleeRelFile := relPath(ce.Callee.File, in.Root)
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   ce.Caller.Name + compositeKeyDelim + callerRelFile,
			ToLabel:   "Symbol",
			ToKey:     ce.Callee.Name + compositeKeyDelim + calleeRelFile,
			EdgeLabel: "CALLS",
			Props: map[string]string{
				"line": strconv.Itoa(int(ce.Line)),
			},
		})
	}

	// INHERITS / IMPLEMENTS edges (Symbol→Symbol).
	relEdges := buildRelationshipEdges(in.Root, in.Rels, in.Symbols)
	edges = append(edges, relEdges...)

	// TESTED_BY edges (test Symbol → tested Symbol).
	testedByEdges := ExtractTestedByEdges(in.Root, in.Symbols)
	edges = append(edges, testedByEdges...)

	// IMPORTS edges (File→Package) + external Package vertices.
	impVertices, impEdges := buildImportsGraph(pkgDirs, fileSet, in.FileImports, aliasCfg)
	vertices = append(vertices, impVertices...)
	edges = append(edges, impEdges...)

	// USES edges (File→File) from resolved Astro template component references.
	// Unresolved refs are already dropped by indexParseFile; all refs here have
	// a valid resolvedTo path.
	for _, ref := range in.TplRefs {
		edges = append(edges, edgeData{
			FromLabel: "File",
			FromKey:   ref.relFile,
			ToLabel:   "File",
			ToKey:     ref.resolvedTo,
			EdgeLabel: "USES",
			Props:     map[string]string{"line": strconv.Itoa(int(ref.line))},
		})
	}

	return vertices, edges, prScores
}

// buildSymbolGraph creates Symbol vertices and CONTAINS edges from file to symbol.
func buildSymbolGraph(root string, symbols []*parser.Symbol, prScores map[string]float64) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData

	for _, sym := range symbols {
		relFile := relPath(sym.File, root)
		symKey := sym.Name + compositeKeyDelim + relFile
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
			props["complexity"] = strconv.Itoa(sym.Complexity)
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
