package parser

import (
	"path/filepath"
	"sort"
)

// SupportedLanguages returns the deduplicated list of tree-sitter-supported
// language names, sorted alphabetically. Derives from the handler registry
// plus extLanguageOverride values — so "javascript" is included even though
// no handler's Language() returns it (it's served by typescriptHandler).
// Fallback-only languages (scala, lua, perl, dart, elixir)
// are NOT included — they have no tree-sitter grammar.
func SupportedLanguages() []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
	}
	for _, h := range registry {
		add(h.Language())
	}
	for _, name := range extLanguageOverride {
		add(name)
	}
	langs := make([]string, 0, len(seen))
	for name := range seen {
		langs = append(langs, name)
	}
	sort.Strings(langs)
	return langs
}

// extLanguageOverride maps extensions where the handler's Language() doesn't
// match the canonical language name for that extension. For example, the
// TypeScript handler also handles .js/.mjs/.cjs (same tree-sitter grammar) but
// those files are "javascript", not "typescript".
//
// Keep entries in sync with typescriptHandler.Extensions() — if a new JS
// variant is added there, it must appear here too.
var extLanguageOverride = map[string]string{
	".js":  "javascript",
	".jsx": "javascript",
	".mjs": "javascript",
	".cjs": "javascript",
}

// fallbackExtToLanguage maps file extensions to a nominal language name for
// languages that have no tree-sitter handler but are still recognized by the
// regex-based fallbackParse. Without this, ParseFile would reject them.
var fallbackExtToLanguage = map[string]string{
	".scala": "scala",
	".sc":    "scala",
	".lua":   "lua",
	".pl":    "perl",
	".pm":    "perl",
	".dart":  "dart",
	".ex":    "elixir",
	".exs":   "elixir",
}

// DetectLanguageFromPath returns the language for a file based on its extension.
// Looks up the handler registry — single source of truth.
// extLanguageOverride wins when the handler's Language() doesn't match the
// canonical name for that extension (e.g. TS handler handles .js files).
// Falls back to fallbackExtToLanguage for languages with no tree-sitter handler.
// Returns "" if no handler is registered for the extension.
func DetectLanguageFromPath(path string) string {
	ext := filepath.Ext(path)
	if lang, ok := extLanguageOverride[ext]; ok {
		return lang
	}
	if h := HandlerForExt(ext); h != nil {
		return h.Language()
	}
	if lang, ok := fallbackExtToLanguage[ext]; ok {
		return lang
	}
	return ""
}
