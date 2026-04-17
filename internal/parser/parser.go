// Package parser provides multi-language AST parsing via tree-sitter.
//
// Each supported language has a corresponding grammar library and a set of
// tree-sitter query files (.scm) in the queries/ subdirectory that extract
// symbols (functions, types, imports, etc.) from the parsed syntax tree.
//
// CGO_ENABLED=1 is required because tree-sitter grammars are C libraries.
package parser

import (
	"fmt"
	"path/filepath"
)

// NodeKind represents the kind of a code symbol extracted from the AST.
type NodeKind string

const (
	KindFunction  NodeKind = "function"
	KindMethod    NodeKind = "method"
	KindType      NodeKind = "type"
	KindStruct    NodeKind = "struct"
	KindInterface NodeKind = "interface"
	KindConst     NodeKind = "const"
	KindVar       NodeKind = "var"
	KindImport    NodeKind = "import"
	KindClass     NodeKind = "class"
	KindModule    NodeKind = "module"
	// KindRune is a Svelte 5 rune call expression ($state, $derived, $effect, etc.).
	// Svelte-only: the rune detector in runes_svelte.go synthesizes these symbols
	// after tree-sitter parsing and sets RuneKind to the canonical rune category.
	KindRune NodeKind = "rune"
)

// Symbol is a named code entity extracted from a parsed file.
type Symbol struct {
	// Name is the symbol's identifier (e.g. "ServeHTTP", "Config", "maxRetries").
	Name string

	// Kind is the symbol type (function, struct, etc.).
	Kind NodeKind

	// Language is the source language (go, python, etc.).
	Language string

	// File is the absolute path to the source file.
	File string

	// StartLine is the 1-based line number where the symbol definition begins.
	StartLine uint32

	// EndLine is the 1-based line number where the symbol definition ends.
	EndLine uint32

	// Signature is the function/type signature extracted from the AST.
	// For functions: full signature with parameters and return types.
	// For types: the type definition header.
	Signature string

	// Body is the full source text of the symbol (only populated when requested).
	Body string

	// DocComment is the documentation comment immediately preceding the symbol.
	DocComment string

	// Complexity is the estimated cyclomatic complexity of the function/method body.
	// Only populated for functions and methods (0 for other symbol kinds).
	Complexity int

	// BodyHash is a content hash of the normalized symbol body.
	// Used for fast equality checks in code comparison (0 means not computed).
	BodyHash uint64

	// Receiver is the type name for methods (e.g. "Config" for impl Config,
	// "Display for Config" for impl Display for Config). Empty for free functions.
	Receiver string

	// IsPublic indicates the symbol has public visibility (pub in Rust, uppercase in Go).
	IsPublic bool

	// Attributes are annotations/decorators (e.g. "#[test]", "#[derive(Clone)]").
	Attributes []string

	// RuneKind is the canonical Svelte 5 rune category for KindRune symbols.
	// Values: "state", "derived", "effect", "props", "bindable", "inspect".
	// Empty for all non-rune symbols.
	RuneKind string
}

// ParseResult contains the symbols extracted from a single source file.
type ParseResult struct {
	// File is the absolute path to the parsed file.
	File string

	// Language is the detected programming language.
	Language string

	// Symbols is the ordered list of symbols found in the file.
	Symbols []*Symbol

	// Imports is the list of import paths/modules declared in the file.
	Imports []string

	// Error is set if parsing failed or produced an error node in the tree.
	Error error
}

// ParseOpts controls how a file is parsed.
type ParseOpts struct {
	// Language overrides auto-detection.
	Language string

	// IncludeBody includes the full source text of each symbol.
	IncludeBody bool

	// IncludeImports includes import declarations in the result.
	IncludeImports bool
}

// ParseFile parses a single source file and returns its symbol table.
// source contains the raw file bytes. path is used for language detection
// and to populate Symbol.File fields.
func ParseFile(path string, source []byte, opts ParseOpts) (*ParseResult, error) {
	lang := opts.Language
	if lang == "" {
		lang = DetectLanguageFromPath(path)
	}
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(path))
	}

	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		// No tree-sitter grammar — use regex-based fallback tokenizer.
		return fallbackParse(path, source, lang), nil
	}

	result, err := handler.Parse(path, source, opts)
	if err != nil {
		return nil, err
	}
	// Override with detected language: one handler may serve multiple languages
	// (typescriptHandler parses .js as "javascript", not its canonical "typescript").
	result.Language = lang
	return result, nil
}
