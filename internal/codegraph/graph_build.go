package codegraph

import (
	"path/filepath"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildGraph constructs vertices and edges from ingested files and parsed symbols.
func buildGraph(root string, files []*ingest.File, symbols []*parser.Symbol, cg *callgraph.CallGraph) ([]vertexData, []edgeData) {
	// Collect unique packages (directories).
	pkgDirs := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		pkgDirs[dir] = struct{}{}
	}

	var vertices []vertexData
	var edges []edgeData

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
	for _, sym := range symbols {
		relFile := relPath(sym.File, root)
		symKey := sym.Name + ":" + relFile
		vertices = append(vertices, vertexData{
			Label: "Symbol",
			Props: map[string]string{
				"name":       sym.Name,
				"kind":       string(sym.Kind),
				"signature":  sym.Signature,
				"file":       relFile,
				"start_line": strconv.Itoa(int(sym.StartLine)),
				"end_line":   strconv.Itoa(int(sym.EndLine)),
			},
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

	return vertices, edges
}
