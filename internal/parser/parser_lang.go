package parser

import (
	"path/filepath"
	"sort"
)

// SupportedLanguages returns the deduplicated list of language names from the
// handler registry, sorted alphabetically.
func SupportedLanguages() []string {
	seen := make(map[string]struct{})
	var langs []string
	for _, h := range registry {
		name := h.Language()
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		langs = append(langs, name)
	}
	sort.Strings(langs)
	return langs
}

// extLanguageOverride maps extensions where the handler's Language() doesn't
// match the canonical language name for that extension. For example, the
// TypeScript handler also handles .js/.mjs/.cjs (same tree-sitter grammar) but
// those files are "javascript", not "typescript".
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
	".kt":    "kotlin",
	".kts":   "kotlin",
	".scala": "scala",
	".sc":    "scala",
	".lua":   "lua",
	".pl":    "perl",
	".pm":    "perl",
	".swift": "swift",
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
