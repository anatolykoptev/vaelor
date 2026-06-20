package callgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// aliasMap is a simple alias-prefix → directory mapping derived from tsconfig
// compilerOptions.paths / baseUrl and astro.config vite.resolve.alias.
// Keys are alias prefixes including the trailing "/" (e.g. "~/", "@/").
// Values are repo-root-relative directories without a trailing "/" (e.g. "src").
type aliasMap map[string]string

// aliasCache caches the per-repo alias map so tsconfig is parsed at most once
// per repo root per process lifetime.
var aliasCache sync.Map // key: repoRoot string, value: aliasMap

// loadTSConfigAliases returns the alias map for a repository root.
// It reads tsconfig.json (and tsconfig.base.json when referenced via "extends"),
// then parses compilerOptions.paths and baseUrl. Results are cached.
//
// tsconfig paths take priority over astro.config vite.resolve.alias.
// When no alias config is found, an empty (non-nil) map is returned so the
// cache still records "no aliases" and avoids repeated file-stat on cold repos.
func loadTSConfigAliases(repoRoot string) aliasMap {
	if v, ok := aliasCache.Load(repoRoot); ok {
		return v.(aliasMap)
	}
	m := buildAliasMap(repoRoot)
	aliasCache.Store(repoRoot, m)
	return m
}

// buildAliasMap does the actual filesystem reads and JSON parsing.
func buildAliasMap(repoRoot string) aliasMap {
	m := make(aliasMap)

	// Try tsconfig.json then tsconfig.base.json (many monorepos split config).
	for _, name := range []string{"tsconfig.json", "tsconfig.base.json"} {
		parseTSConfigFile(filepath.Join(repoRoot, name), repoRoot, m)
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

// parseTSConfigFile reads a single tsconfig file and merges alias entries into m.
// It follows one level of "extends" (base tsconfigs rarely chain further).
func parseTSConfigFile(path, repoRoot string, m aliasMap) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // file absent — not an error
	}

	// tsconfig files often contain // comments and trailing commas. Use a
	// lenient JSON decoder by stripping single-line comments first.
	data = stripJSONComments(data)

	var ts tsconfigShape
	if err := json.Unmarshal(data, &ts); err != nil {
		return // malformed — skip
	}

	// Follow "extends" one level deep (e.g. "tsconfig.base.json" or relative path).
	if ts.Extends != "" {
		ext := ts.Extends
		if !strings.HasPrefix(ext, "/") {
			ext = filepath.Join(filepath.Dir(path), ext)
		}
		// Add .json suffix if missing (tsc allows omitting it).
		if filepath.Ext(ext) == "" {
			ext += ".json"
		}
		parseTSConfigFile(ext, repoRoot, m)
	}

	baseURL := strings.TrimRight(ts.CompilerOptions.BaseURL, "/")

	for alias, targets := range ts.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		// Normalise alias key: strip trailing /* (e.g. "~/*" → "~/").
		key := alias
		if strings.HasSuffix(key, "/*") {
			key = key[:len(key)-1] // keep trailing "/"
		} else if !strings.HasSuffix(key, "/") {
			key += "/"
		}

		// Resolve target: strip leading baseUrl if present, strip trailing /*.
		target := targets[0]
		if strings.HasSuffix(target, "/*") {
			target = target[:len(target)-2]
		}
		// If target is relative (./src) and baseUrl is set, join them.
		if baseURL != "" && strings.HasPrefix(target, "./") {
			target = filepath.Join(baseURL, target[2:])
		}
		// Make target relative to repoRoot.
		target = strings.TrimPrefix(target, "./")
		target = strings.TrimRight(target, "/")

		if _, exists := m[key]; !exists { // tsconfig wins; don't overwrite
			m[key] = target
		}
	}
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

// resolveAlias attempts to resolve importPath using the provided alias map.
// importPath must be a non-relative path (i.e. not starting with ".").
// Returns the repo-root-relative resolved path and true on success, or
// ("", false) when no alias prefix matches.
func resolveAlias(importPath string, aliases aliasMap) (string, bool) {
	for prefix, dir := range aliases {
		if strings.HasPrefix(importPath, prefix) {
			rest := importPath[len(prefix):]
			// dir is already repo-root-relative (e.g. "src").
			resolved := filepath.Join(dir, rest)
			return filepath.Clean(resolved), true
		}
	}
	return "", false
}
