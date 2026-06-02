// Package importresolve provides a unified import resolver shared by codegraph
// and analyze. It is a stdlib-only leaf package (path/filepath and strings only)
// so both callers can import it without introducing dependency cycles.
//
// Resolution strategy:
//   - "./x" / "../x"  — TS/JS/Svelte relative imports resolved against the
//     importing file or package directory. See resolveRelative.
//   - everything else — Go-style absolute imports, longest-suffix-matched against
//     the set of local package dirs. See localPkgDir.
package importresolve

import (
	"path/filepath"
	"strings"
)

// importExts are source extensions a relative TS/JS/Svelte import may resolve to
// when written without one (e.g. `./foo` → `./foo.ts`).
var importExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte", ".astro", ".vue"}

// Resolver resolves import paths to the repo-relative container directory of the
// package they refer to.
type Resolver struct {
	pkgDirs      map[string]struct{} // repo-relative package dirs
	fileSet      map[string]struct{} // repo-relative indexed file paths
	pkgDirByBase map[string][]string // base name → dirs (for O(1) suffix lookup)
}

// New builds a Resolver from pkgDirs (set of repo-relative package directories)
// and fileSet (set of all indexed repo-relative file paths). Both maps must not be
// mutated after New returns.
func New(pkgDirs, fileSet map[string]struct{}) *Resolver {
	byBase := make(map[string][]string, len(pkgDirs))
	for d := range pkgDirs {
		base := filepath.Base(d)
		byBase[base] = append(byBase[base], d)
	}
	return &Resolver{
		pkgDirs:      pkgDirs,
		fileSet:      fileSet,
		pkgDirByBase: byBase,
	}
}

// Resolve maps an import string to the repo-relative container directory of the
// package it refers to. Returns ("", false) for external (unresolvable) imports.
//
//   - "./x" / "../x"  → relative import, resolved against importingDir.
//     importingDir should be filepath.Dir(relFile) for file-level callers, or the
//     package directory for package-level callers.
//   - everything else → Go-style absolute import, longest-suffix-matched against
//     pkgDirs.
func (r *Resolver) Resolve(imp, importingDir string) (string, bool) {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return r.resolveRelative(imp, importingDir)
	}
	return r.localPkgDir(imp)
}

// localPkgDir resolves an absolute import path to the longest-matching local
// package dir. The longest match is strictly more correct than first-match when
// two dirs share a suffix (e.g. "internal/util" vs "internal/sub/util").
func (r *Resolver) localPkgDir(imp string) (string, bool) {
	if _, ok := r.pkgDirs[imp]; ok {
		return imp, true
	}
	best := ""
	for _, d := range r.pkgDirByBase[filepath.Base(imp)] {
		if strings.HasSuffix(imp, "/"+d) && len(d) > len(best) {
			best = d
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// resolveRelative resolves a "./x"/"../x" import relative to importingDir to the
// container dir of its target. Resolution order:
//  1. Joined path as-is (import already carries an extension).
//  2. Path + each known source extension (extensionless import).
//  3. Path as a directory containing index.<ext> (directory index import).
//  4. Best-effort: the candidate is itself an indexed pkgDir (bare dir import).
func (r *Resolver) resolveRelative(imp, importingDir string) (string, bool) {
	cand := filepath.Clean(filepath.Join(importingDir, imp))

	// Explicit-extension file (e.g. "./foo.ts" or "../util/fmt.ts").
	if _, ok := r.fileSet[cand]; ok {
		return filepath.Dir(cand), true
	}

	// Extensionless file (e.g. "./chat" → "./chat.ts").
	for _, ext := range importExts {
		if _, ok := r.fileSet[cand+ext]; ok {
			return filepath.Dir(cand + ext), true
		}
	}

	// Directory with an index file (e.g. "./video" → "./video/index.ts").
	// Container is the directory itself.
	for _, ext := range importExts {
		if _, ok := r.fileSet[filepath.Join(cand, "index"+ext)]; ok {
			return cand, true
		}
	}

	// Best-effort: candidate is an indexed package dir. This is more permissive
	// than strict Node/TS resolution, but it can only match the real joined dir —
	// never an unrelated package — so an occasional extra structural edge is benign
	// for the heuristic graph.
	if _, ok := r.pkgDirs[cand]; ok {
		return cand, true
	}

	return "", false
}
