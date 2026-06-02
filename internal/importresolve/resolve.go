// Package importresolve provides a unified import resolver shared by codegraph
// and analyze. It is a leaf package (path/filepath, strings, os, encoding/json)
// so both callers can import it without introducing dependency cycles.
//
// Resolution strategy (in dispatch order):
//  1. "$lib/…" / "$lib" — SvelteKit alias: resolved via each LibDir in Config.
//  2. "@scope/pkg…"     — Workspace scoped package: resolved via Config.Workspace.
//  3. "./x" / "../x"   — TS/JS/Svelte relative imports resolved against the
//     importing file or package directory. See resolveRelative.
//  4. everything else   — Go-style absolute imports, longest-suffix-matched against
//     the set of local package dirs. See localPkgDir.
//
// A zero Config{} means "no aliases" — steps 1 and 2 are skipped, preserving
// the exact pre-alias behaviour for analyze callers that do not have config.
package importresolve

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// importExts are source extensions a relative TS/JS/Svelte import may resolve to
// when written without one (e.g. `./foo` → `./foo.ts`).
var importExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte", ".astro", ".vue"}

// Config carries alias configuration for the Resolver.
// A zero Config{} disables all alias resolution — relative and Go-style
// resolution still applies. This preserves exact pre-alias behaviour for analyze
// callers that do not have project config at hand.
type Config struct {
	// LibDirs is the list of SvelteKit project roots (repo-relative dirs that
	// contain a svelte.config.js or svelte.config.ts). For each LibDir, "$lib"
	// maps to "<libDir>/src/lib".
	LibDirs []string

	// Workspace maps a scoped package name (e.g. "@oxpulse/mesh-core") to its
	// repo-relative directory (e.g. "packages/mesh-core"). Subpath imports like
	// "@oxpulse/mesh-core/foo" are resolved by joining the mapped dir with "foo".
	Workspace map[string]string
}

// Resolver resolves import paths to the repo-relative container directory of the
// package they refer to.
type Resolver struct {
	pkgDirs      map[string]struct{} // repo-relative package dirs
	fileSet      map[string]struct{} // repo-relative indexed file paths
	pkgDirByBase map[string][]string // base name → dirs (for O(1) suffix lookup)
	cfg          Config
}

// New builds a Resolver from pkgDirs (set of repo-relative package directories),
// fileSet (set of all indexed repo-relative file paths), and an alias Config.
// A zero Config{} means no aliases — relative and Go-style resolution only.
// Both maps must not be mutated after New returns.
func New(pkgDirs, fileSet map[string]struct{}, cfg Config) *Resolver {
	byBase := make(map[string][]string, len(pkgDirs))
	for d := range pkgDirs {
		base := filepath.Base(d)
		byBase[base] = append(byBase[base], d)
	}
	return &Resolver{
		pkgDirs:      pkgDirs,
		fileSet:      fileSet,
		pkgDirByBase: byBase,
		cfg:          cfg,
	}
}

// BuildConfig walks the repository at root and produces a Config by:
//   - recording the repo-relative dir of every svelte.config.js or svelte.config.ts
//     as a LibDir (so "$lib" → "<libDir>/src/lib");
//   - reading every package.json and mapping its "name" field to the repo-relative
//     dir, building the Workspace alias map.
//
// "node_modules" and any dot-directory (".git", ".svelte-kit", ".claude/worktrees",
// …) are skipped entirely via filepath.SkipDir (path-segment match, not substring).
// Skipping dot-dirs matters because some repos keep nested git worktrees (full repo
// copies) under e.g. .claude/worktrees/; descending into them would register a
// duplicate svelte.config / package.json and let a worktree copy win the $lib root
// or a Workspace name by last-write. Config files live in non-dot dirs, so this is
// safe. Files that cannot be read or parsed are silently skipped — one bad manifest
// must never fail the build.
//
// BuildConfig is decoupled from ingest: it walks the repo from disk so that
// package.json files (which have no registered language and are excluded by the
// ingest language filter) are still discovered. This fixes the production bug where
// Config.Workspace was always empty because ingest.IngestRepo dropped .json files
// before BuildConfig could see them.
func BuildConfig(root string) Config {
	var libDirs []string
	workspace := make(map[string]string)

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			name := d.Name()
			// Skip node_modules and any dot-dir (.git, .svelte-kit, .claude, …) —
			// the latter can hold nested repo worktrees whose duplicate manifests
			// would otherwise shadow the real ones. Never skip root itself (".").
			if name == "node_modules" || (name != "." && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}

		base := d.Name()
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}

		switch base {
		case "svelte.config.js", "svelte.config.ts":
			libDirs = append(libDirs, filepath.Dir(rel))
		case "package.json":
			name, ok := readPackageName(path)
			if !ok || name == "" {
				return nil
			}
			workspace[name] = filepath.Dir(rel)
		}
		return nil
	})

	return Config{LibDirs: libDirs, Workspace: workspace}
}

// readPackageName reads just the "name" field from a package.json at absPath.
// Returns ("", false) on any read or parse error.
func readPackageName(absPath string) (string, bool) {
	data, err := os.ReadFile(absPath) //nolint:gosec // path comes from the indexed file set
	if err != nil {
		return "", false
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	return pkg.Name, true
}

// Resolve maps an import string to the repo-relative container directory of the
// package it refers to. Returns ("", false) for external (unresolvable) imports.
//
// Dispatch order:
//  1. "$lib/…" or "$lib" — SvelteKit alias (requires non-empty cfg.LibDirs).
//  2. "@scope/pkg…"      — workspace scoped package (requires cfg.Workspace entry).
//  3. "./x" / "../x"     — relative import, resolved against importingDir.
//     importingDir should be filepath.Dir(relFile) for file-level callers, or the
//     package directory for package-level callers.
//  4. everything else    — Go-style absolute import, longest-suffix-matched.
//
// Aliased imports that have no matching config entry fall through to external.
func (r *Resolver) Resolve(imp, importingDir string) (string, bool) {
	// SvelteKit $lib alias.
	if imp == "$lib" || strings.HasPrefix(imp, "$lib/") {
		if dir, ok := r.resolveLibAlias(imp); ok {
			return dir, true
		}
		return "", false
	}

	// Workspace @scope/pkg alias.
	if strings.HasPrefix(imp, "@") {
		if dir, ok := r.resolveWorkspaceAlias(imp); ok {
			return dir, true
		}
		return "", false
	}

	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return r.resolveRelative(imp, importingDir)
	}
	return r.localPkgDir(imp)
}

// resolveLibAlias resolves a "$lib" or "$lib/subpath" import using cfg.LibDirs.
// For each LibDir, the canonical lib root is "<libDir>/src/lib". The subpath (if
// any) is resolved against that root via resolveAbsSubpath.
//
// Multi-root assumption: when LibDirs has 2+ entries (multi-SvelteKit-app monorepo),
// this returns the first LibDir whose lib root contains the target. It does NOT
// disambiguate by which SvelteKit app is doing the importing — that would require
// per-importer LibDir scoping which is not implemented. For single-app repos (the
// common case) this is correct. For multi-app monorepos, $lib resolution may
// occasionally resolve to the wrong app's lib; callers should be aware of this
// single-app assumption.
func (r *Resolver) resolveLibAlias(imp string) (string, bool) {
	if len(r.cfg.LibDirs) == 0 {
		return "", false
	}

	// Strip the "$lib" prefix; rest is "" or "subpath".
	rest := strings.TrimPrefix(imp, "$lib")
	rest = strings.TrimPrefix(rest, "/")

	for _, libDir := range r.cfg.LibDirs {
		root := filepath.Join(libDir, "src", "lib")

		if rest == "" {
			// Bare $lib: resolve to the lib root dir if it is a known pkgDir.
			if _, ok := r.pkgDirs[root]; ok {
				return root, true
			}
			continue
		}

		if dir, ok := r.resolveAbsSubpath(root, rest); ok {
			return dir, true
		}
	}

	return "", false
}

// resolveWorkspaceAlias resolves a "@scope/pkg" or "@scope/pkg/subpath" import
// using cfg.Workspace. The package name is the first two path segments (the
// scoped name including "@scope/"). A subpath is appended to the workspace dir
// and resolved via resolveAbsSubpath.
//
// For a package-root import (no subpath), the mapped wsDir is only returned as
// local when it is a known pkgDir or when resolveAbsSubpath finds an actual file
// under it. If wsDir is not in pkgDirs and has no indexed files, this returns
// ("", false) so the caller falls through to creating a proper external vertex.
// Returning local for an unknown path would cause the IMPORTS edge to be silently
// dropped (the edge persistence is MATCH/MATCH/MERGE — no vertex means no edge).
func (r *Resolver) resolveWorkspaceAlias(imp string) (string, bool) {
	if len(r.cfg.Workspace) == 0 {
		return "", false
	}

	// Split "@scope/pkg[/rest]" → pkgName="@scope/pkg", rest="rest" (may be "").
	pkgName, rest := splitScopedPkg(imp)
	wsDir, ok := r.cfg.Workspace[pkgName]
	if !ok {
		return "", false
	}

	if rest == "" {
		// Package root import: only resolve as local when the dir is a known
		// pkgDir or an indexed file exists directly inside it. Without this
		// guard an unknown wsDir causes a silently-dropped IMPORTS edge because
		// callers treat isLocal=true as "vertex exists" and skip external-vertex
		// creation, but the MATCH/MATCH/MERGE edge write has no vertex to attach to.
		if _, known := r.pkgDirs[wsDir]; known {
			return wsDir, true
		}
		// Fall through: try resolveAbsSubpath with empty subpath to probe for
		// index files. If none found, return ("", false) → external vertex.
		return r.resolveAbsSubpath(wsDir, "")
	}

	return r.resolveAbsSubpath(wsDir, rest)
}

// resolveAbsSubpath probes for "baseDir/subpath" using the same four-step
// strategy as resolveRelative: explicit extension, extensionless file, index-dir,
// bare-dir in pkgDirs. Returns the container dir and true on the first match.
func (r *Resolver) resolveAbsSubpath(baseDir, subpath string) (string, bool) {
	cand := filepath.Join(baseDir, subpath)

	// Explicit extension.
	if _, ok := r.fileSet[cand]; ok {
		return filepath.Dir(cand), true
	}
	// Extensionless file.
	for _, ext := range importExts {
		if _, ok := r.fileSet[cand+ext]; ok {
			return filepath.Dir(cand + ext), true
		}
	}
	// Directory with index file.
	for _, ext := range importExts {
		if _, ok := r.fileSet[filepath.Join(cand, "index"+ext)]; ok {
			return cand, true
		}
	}
	// Bare dir in pkgDirs.
	if _, ok := r.pkgDirs[cand]; ok {
		return cand, true
	}

	return "", false
}

// scopedPkgParts is the maximum number of parts when splitting a scoped package
// import: ["scope", "pkg", "rest…"]. Used in splitScopedPkg.
const scopedPkgParts = 3

// splitScopedPkg splits a scoped package import "@scope/pkg[/rest]" into the
// package name ("@scope/pkg") and the remaining subpath ("rest" or "").
// For non-scoped "@"-prefixed imports this returns the first path segment and
// the rest — callers must handle both.
func splitScopedPkg(imp string) (pkgName, rest string) {
	// An @-scoped package name is always "@scope/pkg" — two path segments.
	// Strip the leading "@", then split on "/".
	trimmed := strings.TrimPrefix(imp, "@")
	parts := strings.SplitN(trimmed, "/", scopedPkgParts) // ["scope", "pkg", "rest..."]
	switch len(parts) {
	case 1:
		// "@scope" only (unusual, but safe).
		return imp, ""
	case 2:
		// "@scope/pkg".
		return "@" + parts[0] + "/" + parts[1], ""
	default:
		// "@scope/pkg/rest".
		return "@" + parts[0] + "/" + parts[1], parts[2]
	}
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
