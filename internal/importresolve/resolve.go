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
	"regexp"
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

	// WorkspaceExports maps a scoped package name to its package.json "exports"
	// map, normalized so that both keys and values have any leading "./" stripped
	// and use forward slashes (e.g. "./*" → "src/*"). Wildcard "*" is preserved.
	// Used by resolveWorkspaceAlias when the bare wsDir+subpath probe misses, to
	// honor subpath redirects like {"./*":"./src/*"} that move source under src/.
	// nil/empty for packages with no "exports" field — preserves pre-fix behavior.
	WorkspaceExports map[string]map[string]string

	// VirtualModules maps a project-local Vite virtual module id (e.g.
	// "virtual:guide/content") to the repo-relative directory of the package
	// that defines it (the package containing the Vite plugin's resolveId/load
	// code). This is the approach-2 stopgap from #423: it does NOT resolve to
	// the specific re-exported target file (that requires parsing the Vite
	// plugin's load() body and each app's astro.config — approach 1, deferred),
	// but it preserves the package-to-package IMPORTS edge so dep_graph /
	// dead_code / impact_analysis do not show the importing package as
	// orphaned. nil/empty when no virtual modules are found in the repo.
	VirtualModules map[string]string
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
	workspaceExports := make(map[string]map[string]string)
	virtualModules := make(map[string]string)

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
			name, exp, ok := readPackageManifest(path)
			if !ok || name == "" {
				return nil
			}
			workspace[name] = filepath.Dir(rel)
			if len(exp) > 0 {
				workspaceExports[name] = exp
			}
		}

		// Scan TS/JS source files (not .astro/.svelte/.vue — those are consumers)
		// for project-local virtual module definitions. See scanVirtualModules.
		if isVirtualScanTarget(base) {
			scanVirtualModules(path, rel, virtualModules)
		}
		return nil
	})

	return Config{LibDirs: libDirs, Workspace: workspace, WorkspaceExports: workspaceExports, VirtualModules: virtualModules}
}

// readPackageManifest reads the "name" and "exports" fields from a package.json
// at absPath. Returns (name, normalizedExports, ok). ok is false on any read or
// parse error. exports is nil when the field is absent or unparseable; non-nil
// (possibly empty) only when "exports" is present.
//
// The returned exports map is normalized: both keys and values have any leading
// "./" stripped and use forward slashes (e.g. "./*" → "src/*", "./foo" → "foo").
// The wildcard "*" is preserved. Multi-form values are flattened to their string
// targets: a string value is taken as-is; an array value yields its first string
// entry (or first string found inside a condition object); a condition object
// ({"import": ..., "require": ...}) is flattened by preferring "import", then
// "default", then the first string-valued condition. This is a static-analysis
// approximation of Node's resolution algorithm — sufficient for the fleet's
// packages, none of which use conditional-only or array-form-only exports.
func readPackageManifest(absPath string) (string, map[string]string, bool) {
	data, err := os.ReadFile(absPath) //nolint:gosec // path comes from the indexed file set
	if err != nil {
		return "", nil, false
	}
	// Decode into raw json.RawMessage so we can inspect "exports" shape without
	// committing to one struct. "name" is always a string.
	var raw struct {
		Name    string          `json:"name"`
		Exports json.RawMessage `json:"exports"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, false
	}
	exp := ParseExports(raw.Exports)
	return raw.Name, exp, true
}

// ParseExports normalizes a package.json "exports" json.RawMessage into a
// map[string]string (normalized key → normalized target). Returns nil for empty
// or unparseable input. See readPackageManifest for the normalization rules.
//
// Supported shapes:
//   - string:        "exports": "./index.ts"           → {"": "index.ts"}
//   - map:           "exports": {"./foo": "./bar.ts"}  → {"foo": "bar.ts"}
//   - map wildcard:  "exports": {"./*": "./src/*"}     → {"*": "src/*"}
//   - array value:   "exports": {"./foo": ["./a.ts"]}  → {"foo": "a.ts"}
//   - cond object:   "exports": {"./foo": {"import": "./a.mjs"}} → {"foo": "a.mjs"}
func ParseExports(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	// Bare-string form: "exports": "./index.ts" — the package's main entry.
	// Normalize to key "" (empty subpath) → target.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t, ok := normalizeExportsTarget(s); ok {
			return map[string]string{"": t}
		}
		return nil
	}

	// Map form: keys are subpaths (with optional "./" prefix and "*" wildcard),
	// values are string / array / condition-object.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		key := normalizeExportsKey(k)
		target, ok := firstExportsTarget(v)
		if !ok {
			continue
		}
		t, ok := normalizeExportsTarget(target)
		if !ok {
			continue
		}
		out[key] = t
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstExportsTarget extracts the first usable string target from an exports
// value of any shape (string, array, condition object). For condition objects
// it prefers "import" > "default" > first-string. Returns ("", false) when no
// string target can be extracted.
func firstExportsTarget(v json.RawMessage) (string, bool) {
	// Direct string.
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s, true
	}
	// Array — first string element (or first string inside a nested condition).
	var arr []json.RawMessage
	if err := json.Unmarshal(v, &arr); err == nil {
		for _, el := range arr {
			if t, ok := firstExportsTarget(el); ok {
				return t, true
			}
		}
		return "", false
	}
	// Condition object — prefer "import", then "default", then first string.
	var cond map[string]json.RawMessage
	if err := json.Unmarshal(v, &cond); err == nil {
		for _, pref := range []string{"import", "default"} {
			if el, ok := cond[pref]; ok {
				if t, ok := firstExportsTarget(el); ok {
					return t, true
				}
			}
		}
		for _, el := range cond {
			if t, ok := firstExportsTarget(el); ok {
				return t, true
			}
		}
		return "", false
	}
	return "", false
}

// normalizeExportsKey strips a leading "./" and converts to forward slashes.
// The wildcard "*" is preserved. The bare "." key (package root) normalizes to "".
// e.g. "./foo" → "foo", "./*" → "*", "." → "".
func normalizeExportsKey(k string) string {
	k = strings.TrimPrefix(k, "./")
	if k == "." {
		return ""
	}
	return filepath.ToSlash(k)
}

// normalizeExportsTarget strips a leading "./" and converts to forward slashes.
// The wildcard "*" is preserved. Returns ("", false) for empty targets.
func normalizeExportsTarget(t string) (string, bool) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", false
	}
	t = strings.TrimPrefix(t, "./")
	return filepath.ToSlash(t), true
}

// virtualModuleRe matches project-local Vite virtual module identifiers inside
// string literals: "virtual:guide/content", 'virtual:guide/layout', etc.
// It captures the full id (without quotes). The pattern requires at least one
// slash after the prefix to distinguish virtual module ids from the bare
// "virtual:" namespace (which is rare and has no resolvable target).
var virtualModuleRe = regexp.MustCompile(`["'` + "`" + `](virtual:[a-zA-Z][\w-]*(?:/[\w./-]+)+)["'` + "`" + `]`)

// virtualScanExts are the file extensions scanned for virtual module definitions.
// .astro/.svelte/.vue are excluded — they are consumers (importers), not definers.
// .d.ts is included — type declaration files like virtual.d.ts also carry the ids
// and live in the defining package, so they contribute the correct package dir.
var virtualScanExts = map[string]bool{
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".jsx":  true,
	".mjs":  true,
	".cjs":  true,
	".d.ts": false, // handled by compound-ext check below
}

// isVirtualScanTarget reports whether a file should be scanned for virtual module
// definitions, based on its extension. Only TS/JS-family source files are scanned
// (not .astro/.svelte/.vue — those are consumers). Compound extensions like .d.ts
// are handled explicitly.
func isVirtualScanTarget(name string) bool {
	ext := filepath.Ext(name)
	if ext == ".ts" && strings.HasSuffix(name, ".d.ts") {
		return true // .d.ts — type declarations, in the defining package
	}
	return virtualScanExts[ext]
}

// virtualScanMaxBytes bounds the amount of each file read during the virtual
// module scan. Virtual module ids are declared near the top of the file (in
// const blocks), so 16KB is ample for any real-world Vite plugin.
const virtualScanMaxBytes = 16 * 1024

// scanVirtualModules reads the first virtualScanMaxBytes of absPath, finds all
// virtual module id string literals, and records each in out mapped to the
// repo-relative directory of the file (filepath.Dir(rel)). If the same virtual
// id is already in out (from a prior file), it is NOT overwritten — the first
// file wins. This is a stopgap: it does not distinguish the definer (resolveId/
// load code) from a type declaration file, but both live in the same package,
// so the recorded dir is correct for the package-to-package edge.
func scanVirtualModules(absPath, rel string, out map[string]string) {
	data, err := os.ReadFile(absPath) //nolint:gosec // path comes from the repo walk
	if err != nil {
		return
	}
	if len(data) > virtualScanMaxBytes {
		data = data[:virtualScanMaxBytes]
	}
	dir := filepath.Dir(rel)
	for _, m := range virtualModuleRe.FindAllSubmatch(data, -1) {
		id := string(m[1])
		if _, exists := out[id]; !exists {
			out[id] = dir
		}
	}
}

// Resolve maps an import string to the repo-relative container directory of the
// package it refers to. Returns ("", false) for external (unresolvable) imports.
//
// Dispatch order:
//  1. "$lib/…" or "$lib" — SvelteKit alias (requires non-empty cfg.LibDirs).
//  2. "@scope/pkg…"      — workspace scoped package (requires cfg.Workspace entry).
//  3. "virtual:…"        — project-local Vite virtual module (requires
//     cfg.VirtualModules entry). Stopgap (#423): resolves to the defining
//     package dir, not the specific re-exported target file.
//  4. "./x" / "../x"     — relative import, resolved against importingDir.
//     importingDir should be filepath.Dir(relFile) for file-level callers, or the
//     package directory for package-level callers.
//  5. everything else    — Go-style absolute import, longest-suffix-matched.
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

	// Project-local Vite virtual module (stopgap — #423).
	if strings.HasPrefix(imp, "virtual:") {
		if dir, ok := r.resolveVirtualModule(imp); ok {
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
// When the bare wsDir+subpath probe misses, the package's "exports" map (from
// cfg.WorkspaceExports) is consulted: the subpath is matched against the exports
// keys (exact, then "./*" wildcard), rewritten to the mapped target, and
// resolveAbsSubpath is retried with the rewritten subpath. This honors subpath
// redirects like {"./*":"./src/*"} that move source under src/ — the common
// Astro/Vite monorepo layout where package.json lives at the package root but
// source files live under src/.
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
		// index files. If none found, consult exports for the "" (root) key,
		// then return ("", false) → external vertex.
		if dir, ok := r.resolveAbsSubpath(wsDir, ""); ok {
			return dir, true
		}
		return r.resolveViaExports(wsDir, pkgName, "")
	}

	// Subpath import: try the bare wsDir+subpath probe first (preserves the
	// pre-fix fast path for packages whose files live directly under wsDir).
	if dir, ok := r.resolveAbsSubpath(wsDir, rest); ok {
		return dir, true
	}
	// Miss — consult the package's exports map for a subpath rewrite.
	return r.resolveViaExports(wsDir, pkgName, rest)
}

// resolveViaExports consults cfg.WorkspaceExports[pkgName] to rewrite the
// subpath via the package's "exports" map, then re-probes with resolveAbsSubpath.
// Returns ("", false) when the package has no exports, no key matches, or the
// rewritten probe still misses. Callers fall through to external in that case.
//
// Matching: exact key match first (e.g. "foo" → "bar.ts"), then wildcard keys.
// A wildcard key contains a single "*" (the only form Node allows); the rest of
// the key is a literal prefix/suffix that the subpath must match. The captured
// substring (everything between the prefix and suffix) is substituted for "*" in
// the target. This covers both the {"./*":"./src/*"} idiom (key "*" → "src/*")
// and the {".//components/*":"./src/components/*"} idiom (key "components/*" →
// "src/components/*") used by @guide/ui. Multi-wildcard keys are not supported
// (Node forbids them).
func (r *Resolver) resolveViaExports(wsDir, pkgName, subpath string) (string, bool) {
	exp := r.cfg.WorkspaceExports[pkgName]
	if len(exp) == 0 {
		return "", false
	}
	// Exact key match.
	if target, ok := exp[subpath]; ok {
		if dir, ok := r.resolveAbsSubpath(wsDir, target); ok {
			return dir, true
		}
	}
	// Wildcard keys. Iterate looking for a key containing "*" that the subpath
	// matches. Map iteration order is non-deterministic, but wildcard keys in a
	// single package.json are mutually non-overlapping in practice (Node
	// resolution requires this); if two could match, the more specific one
	// (longer literal prefix) wins.
	var bestKey, bestTarget string
	for key, target := range exp {
		star := strings.IndexByte(key, '*')
		if star < 0 {
			continue // exact key, already tried above
		}
		prefix, suffix := key[:star], key[star+1:]
		if !strings.HasPrefix(subpath, prefix) || !strings.HasSuffix(subpath, suffix) {
			continue
		}
		if len(subpath) < len(prefix)+len(suffix) {
			continue // subpath is shorter than prefix+suffix — nothing captured
		}
		// Prefer the longest literal prefix (most specific match). Guard the
		// bestKey=="" case first to avoid slicing an empty key on the wildcard.
		bestStar := strings.IndexByte(bestKey, '*')
		bestPrefixLen := 0
		if bestStar >= 0 {
			bestPrefixLen = len(bestKey[:bestStar])
		}
		if bestKey == "" || len(prefix) > bestPrefixLen {
			bestKey = key
			bestTarget = target
		}
	}
	if bestKey != "" {
		star := strings.IndexByte(bestKey, '*')
		prefix, suffix := bestKey[:star], bestKey[star+1:]
		captured := subpath[len(prefix) : len(subpath)-len(suffix)]
		rewritten := strings.ReplaceAll(bestTarget, "*", captured)
		if dir, ok := r.resolveAbsSubpath(wsDir, rewritten); ok {
			return dir, true
		}
	}
	return "", false
}

// resolveVirtualModule resolves a "virtual:foo/bar" import to the repo-relative
// directory of the package that defines it, using cfg.VirtualModules. This is
// the approach-2 stopgap from #423: it returns the defining package dir (found
// by scanning TS/JS source for the virtual id string literal), NOT the specific
// re-exported target file. The edge is package-to-package, sufficient for
// dep_graph / dead_code to not show the importer as orphaned.
//
// The defining dir is only returned when it is a known pkgDir — the same guard
// as resolveWorkspaceAlias's package-root branch. Without this, a stale
// VirtualModules entry for a removed package would cause a silently-dropped
// IMPORTS edge (callers treat isLocal=true as "vertex exists").
func (r *Resolver) resolveVirtualModule(imp string) (string, bool) {
	if len(r.cfg.VirtualModules) == 0 {
		return "", false
	}
	dir, ok := r.cfg.VirtualModules[imp]
	if !ok {
		return "", false
	}
	if _, known := r.pkgDirs[dir]; known {
		return dir, true
	}
	return "", false
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
