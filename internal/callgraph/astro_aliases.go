package callgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// aliasMap is a simple alias-prefix → directory mapping derived from tsconfig
// compilerOptions.paths / baseUrl and astro.config vite.resolve.alias.
// Keys are alias prefixes including the trailing "/" (e.g. "~/", "@/").
// Values are repo-root-relative directories without a trailing "/" (e.g. "src").
type aliasMap map[string]string

// tsconfigParseErrorsTotal counts tsconfig.json files that fail JSON
// unmarshalling after comment-stripping. A non-zero rate means aliases were
// silently dropped for those repos — operators can use this to discover
// malformed tsconfig files.
var tsconfigParseErrorsTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_tsconfig_parse_errors_total",
		Help: "tsconfig.json files that failed JSON unmarshalling after comment-stripping (aliases silently dropped).",
	},
)

// aliasCacheKey combines the repo root and the maximum mtime across all
// tsconfig files touched during alias resolution. Including mtime in the key
// invalidates the cached entry whenever tsconfig.json is modified — critical
// because go-code is a long-lived Docker process.
type aliasCacheKey struct {
	root    string
	maxMtime int64 // UnixNano of the most-recently-modified tsconfig file
}

// aliasCache caches the per-repo alias map.
// Key: aliasCacheKey; value: aliasMap.
var aliasCache sync.Map

// loadTSConfigAliases returns the alias map for a repository root.
// It reads tsconfig.json (and tsconfig.base.json when referenced via "extends"),
// then parses compilerOptions.paths and baseUrl. Results are cached keyed by
// the maximum tsconfig mtime so edits invalidate the entry without a restart.
//
// tsconfig paths take priority over astro.config vite.resolve.alias.
// When no alias config is found, an empty (non-nil) map is returned so the
// cache still records "no aliases" and avoids repeated file-stat on cold repos.
func loadTSConfigAliases(repoRoot string) aliasMap {
	maxMtime := latestTSConfigMtime(repoRoot)
	key := aliasCacheKey{root: repoRoot, maxMtime: maxMtime}

	if v, ok := aliasCache.Load(key); ok {
		return v.(aliasMap)
	}
	m := buildAliasMap(repoRoot)
	aliasCache.Store(key, m)
	return m
}

// latestTSConfigMtime returns the maximum UnixNano mtime across the top-level
// tsconfig files we parse. 0 means no tsconfig was found.
func latestTSConfigMtime(repoRoot string) int64 {
	var max int64
	for _, name := range []string{"tsconfig.json", "tsconfig.base.json"} {
		info, err := os.Stat(filepath.Join(repoRoot, name))
		if err != nil {
			continue
		}
		if ns := info.ModTime().UnixNano(); ns > max {
			max = ns
		}
	}
	return max
}

// buildAliasMap does the actual filesystem reads and JSON parsing.
func buildAliasMap(repoRoot string) aliasMap {
	m := make(aliasMap)

	// Try tsconfig.json then tsconfig.base.json (many monorepos split config).
	visited := make(map[string]bool)
	for _, name := range []string{"tsconfig.json", "tsconfig.base.json"} {
		parseTSConfigFile(filepath.Join(repoRoot, name), repoRoot, m, visited)
	}

	// Try astro.config.mjs / astro.config.ts as a supplemental source (lower
	// priority than tsconfig — only fills gaps).
	if len(m) == 0 {
		parseAstroConfigAliases(repoRoot, m)
	}

	return m
}

// tsconfigShape is a minimal subset of tsconfig.json for alias extraction.
type tsconfigShape struct {
	Extends         string `json:"extends"`
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// trailingCommaRe matches a trailing comma immediately before a closing
// bracket or brace (possibly with intervening whitespace).
// Example: {"a": 1,} or ["x",] → removes the comma.
var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

// parseTSConfigFile reads a single tsconfig file and merges alias entries into m.
// It follows "extends" recursively (with a visited-set to prevent cycles).
// baseUrl in an extended config is resolved relative to THAT file's directory.
//
// visited tracks absolute paths already parsed in the current chain to prevent
// infinite recursion when tsconfig files mutually extend each other.
func parseTSConfigFile(path, repoRoot string, m aliasMap, visited map[string]bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	if visited[absPath] {
		return // cycle detected — stop recursing
	}
	visited[absPath] = true

	ts, ok := readTSConfigShape(absPath)
	if !ok {
		return
	}

	// Follow "extends" recursively with cycle guard.
	followTSConfigExtends(ts.Extends, absPath, repoRoot, m, visited)

	// baseUrl is resolved relative to THIS file's directory, not repoRoot.
	// A base tsconfig with baseUrl:"." means the directory of the base file.
	baseURL := resolveTSBaseURL(ts.CompilerOptions.BaseURL, absPath, repoRoot)

	mergeTSPaths(ts.CompilerOptions.Paths, baseURL, m)
}

// readTSConfigShape reads and parses a tsconfig file, returning the shape and
// whether parsing succeeded. It strips // comments and trailing commas before
// unmarshalling.
//
// Note: block comments /* */ are not handled — tsconfig files rarely use them,
// but if needed add block-comment stripping before the json.Unmarshal call.
func readTSConfigShape(absPath string) (tsconfigShape, bool) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return tsconfigShape{}, false // file absent — not an error
	}
	data = stripJSONComments(data)
	data = trailingCommaRe.ReplaceAll(data, []byte("$1"))

	var ts tsconfigShape
	if err := json.Unmarshal(data, &ts); err != nil {
		tsconfigParseErrorsTotal.Inc()
		fmt.Printf("go-code: tsconfig parse error at %s: %v\n", absPath, err)
		return tsconfigShape{}, false
	}
	return ts, true
}

// followTSConfigExtends resolves and recursively processes a tsconfig "extends"
// field, using the visited set to prevent cycles.
func followTSConfigExtends(extends, absPath, repoRoot string, m aliasMap, visited map[string]bool) {
	if extends == "" {
		return
	}
	ext := extends
	if !strings.HasPrefix(ext, "/") {
		ext = filepath.Join(filepath.Dir(absPath), ext)
	}
	if filepath.Ext(ext) == "" {
		ext += ".json" // tsc allows omitting the .json suffix
	}
	parseTSConfigFile(ext, repoRoot, m, visited)
}

// resolveTSBaseURL converts a raw tsconfig baseUrl string to a repo-root-relative
// directory. The baseUrl is resolved relative to the directory of the tsconfig
// file that declared it (absPath), so "." in a base file means that file's dir.
func resolveTSBaseURL(rawBaseURL, absPath, repoRoot string) string {
	base := strings.TrimRight(rawBaseURL, "/")
	if base == "" || filepath.IsAbs(base) {
		return base
	}
	abs := filepath.Join(filepath.Dir(absPath), base)
	if rel, err := filepath.Rel(repoRoot, abs); err == nil {
		return rel
	}
	return base
}

// mergeTSPaths merges compilerOptions.paths entries from a tsconfig into m.
// Existing keys in m are not overwritten (first-wins / tsconfig-wins policy).
func mergeTSPaths(paths map[string][]string, baseURL string, m aliasMap) {
	for alias, targets := range paths {
		if len(targets) == 0 {
			continue
		}
		key := normaliseTSAliasKey(alias)
		if _, exists := m[key]; exists {
			continue // first-wins; don't overwrite
		}
		m[key] = normaliseTSTarget(targets[0], baseURL)
	}
}

// normaliseTSAliasKey converts a tsconfig paths key to the canonical alias
// prefix used in aliasMap (trailing "/" instead of trailing "/*").
func normaliseTSAliasKey(alias string) string {
	if strings.HasSuffix(alias, "/*") {
		return alias[:len(alias)-1] // "~/*" → "~/"
	}
	if !strings.HasSuffix(alias, "/") {
		return alias + "/"
	}
	return alias
}

// normaliseTSTarget strips the wildcard suffix from a tsconfig paths target and
// resolves it relative to baseURL when set. TypeScript resolves relative targets
// against the tsconfig's baseUrl, so "src/*" with baseUrl "config/" yields
// "config/src" relative to the repo root.
func normaliseTSTarget(target, baseURL string) string {
	if strings.HasSuffix(target, "/*") {
		target = target[:len(target)-2]
	}
	if baseURL != "" && !filepath.IsAbs(target) {
		return filepath.Join(baseURL, strings.TrimPrefix(target, "./"))
	}
	return strings.TrimRight(strings.TrimPrefix(target, "./"), "/")
}

// parseAstroConfigAliases is a best-effort heuristic that scans astro.config.mjs
// or astro.config.ts for vite resolve.alias lines of the form:
//
//	'~/': './src/'
//	'@/': new URL('./src', import.meta.url).pathname + '/',
//
// It only handles the simple string-to-string form; URL-constructor forms are
// skipped. This is intentionally shallow — tsconfig.paths covers 99% of real
// repos; this is a fallback for repos that configure aliases only in Astro.
func parseAstroConfigAliases(repoRoot string, m aliasMap) {
	for _, name := range []string{"astro.config.mjs", "astro.config.ts", "astro.config.js"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			continue
		}
		src := string(data)
		lines := strings.Split(src, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Match: 'key': 'value' or "key": "value"
			// Heuristic: both key and value are quoted strings with : between.
			alias, dir, ok := parseSimpleAliasLine(line)
			if !ok {
				continue
			}
			if _, exists := m[alias]; !exists {
				dir = strings.TrimRight(strings.TrimPrefix(dir, "./"), "/")
				m[alias] = dir
			}
		}
		break // first found wins
	}
}

// parseSimpleAliasLine tries to extract a quoted alias key and quoted directory
// value from a line of the form:
//
//	'~/': './src/',
//	"@/": "./src/",
//
// Returns ("", "", false) when the line doesn't match.
func parseSimpleAliasLine(line string) (alias, dir string, ok bool) {
	// Strip trailing comma.
	line = strings.TrimRight(line, ",")
	// Require at least one ':' separator.
	colonIdx := strings.Index(line, ":")
	if colonIdx < 0 {
		return "", "", false
	}
	keyPart := strings.TrimSpace(line[:colonIdx])
	valPart := strings.TrimSpace(line[colonIdx+1:])

	alias = unquote(keyPart)
	dir = unquote(valPart)
	if alias == "" || dir == "" {
		return "", "", false
	}
	// Alias must end with "/" (e.g. "~/" or "@/") to be a path alias prefix.
	if !strings.HasSuffix(alias, "/") {
		return "", "", false
	}
	// Value must start with "." — reject URL-constructor forms.
	if !strings.HasPrefix(dir, ".") {
		return "", "", false
	}
	return alias, dir, true
}

// unquote returns the contents of a single- or double-quoted string literal.
// Returns "" if s is not a quoted string.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	q := s[0]
	if q != '\'' && q != '"' && q != '`' {
		return ""
	}
	end := strings.IndexByte(s[1:], q)
	if end < 0 {
		return ""
	}
	return s[1 : end+1]
}

// stripJSONComments removes single-line // comments from JSON-ish content
// so that tsconfig files with comments can be unmarshalled by encoding/json.
// It does NOT handle /* */ block comments or comments inside string literals.
// This is good enough for the tsconfig subset we parse.
func stripJSONComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if idx := strings.Index(l, "//"); idx >= 0 {
			// Only strip if the // is not inside a string (basic heuristic:
			// count unescaped quotes before the //; if even, it's outside a string).
			before := l[:idx]
			q := strings.Count(before, "\"") - strings.Count(before, "\\\"")
			if q%2 == 0 {
				l = l[:idx]
			}
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n"))
}

// resolveAlias attempts to resolve importPath using the provided alias map
// using longest-prefix-wins semantics. When multiple prefixes match (e.g.
// "@/" and "@ui/" both match "@ui/Button"), the longest prefix wins, making
// resolution deterministic regardless of Go's map-iteration order.
//
// importPath must be a non-relative path (i.e. not starting with ".").
// Returns the repo-root-relative resolved path and true on success, or
// ("", false) when no alias prefix matches.
//
// OWASP path-traversal guard: if the resolved path escapes the repo root
// (starts with ".."), the resolution is rejected.
func resolveAlias(importPath string, aliases aliasMap) (string, bool) {
	// Collect all matching prefixes sorted by descending length so the longest wins.
	type candidate struct {
		prefix string
		dir    string
	}
	var matches []candidate
	for prefix, dir := range aliases {
		if strings.HasPrefix(importPath, prefix) {
			matches = append(matches, candidate{prefix: prefix, dir: dir})
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	// Longest prefix wins — deterministic even with overlapping prefixes.
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i].prefix) > len(matches[j].prefix)
	})
	best := matches[0]
	rest := importPath[len(best.prefix):]
	resolved := filepath.Clean(filepath.Join(best.dir, rest))
	// OWASP path-traversal guard — mirrors the relative-import guard above.
	if strings.HasPrefix(resolved, "..") {
		return "", false
	}
	return resolved, true
}
