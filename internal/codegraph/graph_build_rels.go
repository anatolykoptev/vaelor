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
// resolveImport (via localPkgDir for Go-style imports, resolveRelativeImport for
// TS/JS-style "./x"/"../x" imports) maps each import back to its container dir so
// both edge kinds land on one node. fileSet is the set of all indexed file paths,
// needed to resolve a relative import to its target file's directory.
func buildImportsGraph(pkgDirs, fileSet map[string]struct{}, fileImports map[string][]string) ([]vertexData, []edgeData) {
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
			localDir, isLocal := resolveImport(imp, relFile, pkgDirs, fileSet, pkgDirByBase)
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

// importExts are the source extensions a relative TS/JS/Svelte import may resolve
// to when written without one (e.g. `./foo` → `./foo.ts`).
var importExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte", ".astro", ".vue"}

// resolveImport maps an import string to the repo-relative container dir of the
// package it refers to, or ("", false) for an external import.
//
//   - "./x" / "../x"      → TS/JS/Svelte relative import, resolved against the
//     importing file's directory (needs fileSet). See resolveRelativeImport.
//   - everything else     → Go-style absolute import, suffix-matched against the
//     local package dirs. See localPkgDir.
//
// Aliased ("$lib/x") and workspace ("@scope/pkg") TS imports are not yet resolved
// and fall through to external — a follow-up can add config-driven resolution.
func resolveImport(imp, relFile string, pkgDirs, fileSet map[string]struct{}, pkgDirByBase map[string][]string) (string, bool) {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return resolveRelativeImport(imp, relFile, pkgDirs, fileSet)
	}
	return localPkgDir(imp, pkgDirs, pkgDirByBase)
}

// resolveRelativeImport resolves a "./x"/"../x" import (relative to relFile) to the
// container dir of its target file. It tries, in order: the joined path as-is (the
// import already carried an extension), the path with each known source extension,
// and the path as a directory holding an index.<ext> file. Returns the target
// file's directory (its package), or ("", false) if nothing indexed matches.
func resolveRelativeImport(imp, relFile string, pkgDirs, fileSet map[string]struct{}) (string, bool) {
	cand := filepath.Clean(filepath.Join(filepath.Dir(relFile), imp))

	// Explicit-extension file (e.g. "./foo.ts").
	if _, ok := fileSet[cand]; ok {
		return filepath.Dir(cand), true
	}
	// Extensionless file (e.g. "./foo" → "./foo.ts").
	for _, ext := range importExts {
		if _, ok := fileSet[cand+ext]; ok {
			return filepath.Dir(cand + ext), true
		}
	}
	// Directory with an index file (e.g. "./video" → "./video/index.ts"). The
	// container is the directory itself.
	for _, ext := range importExts {
		if _, ok := fileSet[filepath.Join(cand, "index"+ext)]; ok {
			return cand, true
		}
	}
	// Best-effort: the candidate is itself an indexed package dir. This is more
	// permissive than strict Node/TS resolution (a bare directory import needs an
	// index.* or package.json#main), but it can only resolve to the real dir at
	// Join(importerDir, imp) — never an unrelated package — so an occasional extra
	// structural edge is benign for the heuristic graph.
	if _, ok := pkgDirs[cand]; ok {
		return cand, true
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
