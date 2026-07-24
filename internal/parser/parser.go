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

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
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
	// KindMacro is a C/C++ preprocessor #define or a Rust macro_rules! definition.
	// Aligned with the tree-sitter @definition.macro / SCIP Macro kind.
	KindMacro NodeKind = "macro"
	// KindTypeAlias is a type alias (Rust type_item, TS type_alias_declaration,
	// C++ alias_declaration/type_definition). Aligned with the tree-sitter
	// @definition.type_alias / SCIP TypeAlias kind. When EXPAND_SYMBOL_KINDS is
	// OFF, type aliases remain indexed as KindType (byte-identical to pre-#664);
	// the refinement to KindTypeAlias only fires when the flag is ON.
	KindTypeAlias NodeKind = "type_alias"
	// KindRune is a Svelte 5 rune call expression ($state, $derived, $effect, etc.).
	// Svelte-only: the rune detector in runes_svelte.go synthesizes these symbols
	// after tree-sitter parsing and sets RuneKind to the canonical rune category.
	KindRune NodeKind = "rune"
)

// IsEmbeddableKind reports whether a symbol kind is eligible for the semantic
// embedding index. The embedding pipeline (bulk, incremental, and cache paths)
// uses this single predicate so all three agree on the indexed set — a divergent
// set would churn (embed on one path, orphan-delete on the next).
//
// Indexed kinds (high retrieval value, the symbols users search for by name or
// concept across every language):
//   - KindFunction, KindMethod: behaviour — the historical indexed set.
//   - KindType: Rust enum/type-alias, Java/TS type aliases, etc.
//   - KindStruct: Rust/Go struct definitions.
//   - KindInterface: Go interfaces, Rust traits (mapped by handler_rust), Java interfaces.
//   - KindClass: TS/JS/Python/Java/PHP/Ruby class definitions.
//
// Excluded kinds (high volume, low retrieval value, embedding cost — not added
// here; a future change may revisit with a separate budget):
//   - KindConst, KindVar: per-symbol embeddings of every constant/variable would
//     balloon the index with low-signal rows (e.g. `const Version = "1.0"`).
//   - KindModule: module/import declarations are structural, not searchable concepts.
//   - KindImport, KindRune: not user-facing definable symbols.
func IsEmbeddableKind(k NodeKind) bool {
	switch k {
	case KindFunction, KindMethod, KindType, KindStruct, KindInterface, KindClass:
		return true
	}
	return false
}

// IsEmbeddableKindExpanded is the flag-gated predicate that controls whether
// the new low-volume symbol kinds (#664: macro, module, type-alias) enter the
// embedding index. When expanded=false it delegates to IsEmbeddableKind —
// byte-identical to the pre-#664 indexed set (prod unchanged). When
// expanded=true it additionally admits KindMacro, KindModule, and
// KindTypeAlias.
//
// The embedding pipeline (bulk, incremental, and cache paths) uses this
// predicate so all three agree on the indexed set — a divergent set would
// churn (embed on one path, orphan-delete on the next). The flag is read from
// the EXPAND_SYMBOL_KINDS env var (default false) and wired through the
// Pipeline; see cmd/vaelor/config.go.
func IsEmbeddableKindExpanded(k NodeKind, expanded bool) bool {
	if IsEmbeddableKind(k) {
		return true
	}
	if !expanded {
		return false
	}
	switch k {
	case KindMacro, KindModule, KindTypeAlias:
		return true
	}
	return false
}

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

	// CognitiveComplexity is the cognitive complexity of the function body (nesting-aware).
	// Only populated for functions and methods (0 for other kinds).
	CognitiveComplexity int

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
	// Values: "state", "derived", "effect", "props", "bindable", "inspect", "host".
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

	// TemplateRefs holds capitalised JSX-style component tag usages found in
	// the template body of Astro (and future Svelte/Vue) files. Empty for all
	// other languages. Each entry is one occurrence; callers may deduplicate.
	// Resolution of names to file paths requires joining against Imports.
	TemplateRefs []preproc.TemplateRef `json:"template_refs,omitempty"`

	// TypeRels is the list of type relationships (embeds/extends/implements)
	// extracted during parse. Populated only when ParseOpts.IncludeTypeRels is true.
	TypeRels []TypeRelationship `json:"type_rels,omitempty"`

	// Lines is the number of lines in the source file. Optional; populated by
	// callers that already count lines while reading source.
	Lines int `json:"lines,omitempty"`

	// Error is set if parsing failed or produced an error node in the tree.
	Error error
}

// ParseOpts controls how a file is parsed.
type ParseOpts struct {
	// Language overrides auto-detection.
	Language string

	// Parser is an optional tree-sitter parser to reuse. When non-nil, Parse
	// will use it instead of creating a new parser. The caller retains
	// ownership and must not use it concurrently.
	Parser *sitter.Parser

	// IncludeBody includes the full source text of each symbol.
	IncludeBody bool

	// IncludeImports includes import declarations in the result.
	IncludeImports bool

	// IncludeTypeRels extracts type relationships (embeds/extends/implements)
	// during parse. When false, TypeRels is left empty.
	IncludeTypeRels bool

	// ExpandSymbolKinds gates the #664 low-volume symbol-kind expansion. When
	// true, macro and module symbols (C/C++ #define, Rust macro_rules!/mod) are
	// emitted into pr.Symbols and type-alias nodes (Rust type_item, TS
	// type_alias_declaration, C/C++ type_definition/alias_declaration) are
	// refined from KindType to KindTypeAlias. When false (the default), the
	// parse result is byte-identical to the pre-#664 behavior: macro and module
	// symbols are skipped at the parse-time emission chokepoint
	// (processCaptureWithCaps) so they never enter pr.Symbols, and type aliases
	// remain KindType. The embedding pipeline additionally filters via
	// IsEmbeddableKindExpanded as defense-in-depth.
	ExpandSymbolKinds bool
}

// ParseFile parses a single source file and returns its symbol table.
// source contains the raw file bytes. path is used for language detection
// and to populate Symbol.File fields.
func ParseFile(path string, source []byte, opts ParseOpts) (*ParseResult, error) {
	lang := resolveLanguage(path, opts)
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

// resolveLanguage returns the file's override-first language: opts.Language when
// set, else DetectLanguageFromPath(path). Single source of the precedence shared
// by ParseFile (result-level) and applyDetectedSymbolLanguage (symbol-level).
func resolveLanguage(path string, opts ParseOpts) string {
	if opts.Language != "" {
		return opts.Language
	}
	return DetectLanguageFromPath(path)
}

// applyDetectedSymbolLanguage overwrites each symbol's Language with the file's
// override-first language: opts.Language when set (matching ParseFile), else
// DetectLanguageFromPath(path). The JS/TS-family handlers (tsxHandler,
// typescriptHandler) share a MapCapture that hardcodes "typescript" on every
// symbol, mislabeling .jsx/.js/.mjs/.cjs — this corrects them to agree with the
// file's own detector. Override-first is load-bearing: the sparse-embedding
// backfill re-parses stored rows with ParseOpts{Language: storedRow.Language}
// so buildEmbedText reproduces the stored hash; honoring opts.Language keeps
// pre-parity rows (indexed as "typescript") reproducible instead of drifting
// them into NULL sparse vectors. Handlers opt in from Parse; the shared
// ParseFile seam is deliberately left untouched so svelte/astro/vue/html symbol
// labels are unaffected.
func applyDetectedSymbolLanguage(result *ParseResult, path string, opts ParseOpts) {
	lang := resolveLanguage(path, opts)
	for _, sym := range result.Symbols {
		sym.Language = lang
	}
}
