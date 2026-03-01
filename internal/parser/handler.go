// Package parser provides multi-language AST parsing via tree-sitter.
//
// Each supported language has a corresponding grammar library and a set of
// tree-sitter query files (.scm) in the queries/ subdirectory that extract
// symbols (functions, types, imports, etc.) from the parsed syntax tree.
//
// CGO_ENABLED=1 is required because tree-sitter grammars are C libraries.
package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
)

// Tree-sitter capture name constants used across multiple language handlers.
// These correspond to the @symbol.* and @import.* captures in .scm query files.
const (
	captureFunction  = "symbol.function"
	captureMethod    = "symbol.method"
	captureClass     = "symbol.class"
	captureInterface = "symbol.interface"
	captureType      = "symbol.type"
	captureConst     = "symbol.const"
	captureVar       = "symbol.var"
	captureImport    = "import.path"

	captureCallFunction = "call.function"
	captureCallMethod   = "call.method"

	captureRelSubject    = "rel.subject"
	captureRelTarget     = "rel.target"
	captureRelImplTarget = "rel.impl_target"
)

// LanguageHandler abstracts a tree-sitter language grammar and its query logic.
// Each handler knows how to parse its language and map tree-sitter captures
// to the common Symbol type.
type LanguageHandler interface {
	// Language returns the canonical language name (e.g. "go", "python").
	Language() string

	// Extensions returns the file extensions handled (e.g. [".go"]).
	Extensions() []string

	// SitterLanguage returns the tree-sitter language for parser initialization.
	SitterLanguage() *sitter.Language

	// TagsQuery returns the compiled tree-sitter query for symbol extraction.
	TagsQuery() *sitter.Query

	// MapCapture converts a single tree-sitter capture into a Symbol.
	// Returns nil if the capture should be skipped.
	MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol
}

// CallQueryProvider is an optional interface that LanguageHandler implementations
// can satisfy to support call extraction.
type CallQueryProvider interface {
	CallsQuery() *sitter.Query
}

// RelationshipQueryProvider is an optional interface that LanguageHandler implementations
// can satisfy to support type relationship extraction (extends, implements, embeds).
type RelationshipQueryProvider interface {
	RelationshipsQuery() *sitter.Query
}

// registry maps file extension (e.g. ".go") to its LanguageHandler.
var registry = map[string]LanguageHandler{}

// registerHandler registers a LanguageHandler for all its extensions.
// Called from each handler's init() function.
func registerHandler(h LanguageHandler) {
	for _, ext := range h.Extensions() {
		registry[ext] = h
	}
}

// HandlerForExt returns the LanguageHandler for a given file extension.
// Returns nil if the extension is not supported.
func HandlerForExt(ext string) LanguageHandler {
	return registry[ext]
}
