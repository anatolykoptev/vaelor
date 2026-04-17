package parser

import "path/filepath"

// SupportedLanguages returns the list of languages that can be parsed.
func SupportedLanguages() []string {
	return []string{
		"go",
		"python",
		"typescript",
		"javascript",
		"rust",
		"java",
		"c",
		"cpp",
		"ruby",
		"csharp",
		"php",
	}
}

// extToLanguage maps file extensions to their programming language names.
var extToLanguage = map[string]string{
	".go":    "go",
	".py":    "python",
	".ts":    "typescript",
	".tsx":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".cjs":   "javascript",
	".cts":   "typescript",
	".mts":   "typescript",
	".rs":    "rust",
	".java":  "java",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".cc":    "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".rb":    "ruby",
	".cs":    "csharp",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".php":   "php",
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

// DetectLanguageFromPath returns the language based on file extension.
// Exported so tests and other packages can use it without parsing a full file.
func DetectLanguageFromPath(path string) string {
	return extToLanguage[filepath.Ext(path)]
}
