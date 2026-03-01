// Package parser provides multi-language AST parsing via tree-sitter.
//
// Each supported language has a corresponding grammar library and a set of
// tree-sitter query files (.scm) in the queries/ subdirectory that extract
// symbols (functions, types, imports, etc.) from the parsed syntax tree.
//
// CGO_ENABLED=1 is required because tree-sitter grammars are C libraries.
package parser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
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

	p := sitter.NewParser()
	p.SetLanguage(handler.SitterLanguage())

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	result := &ParseResult{
		File:     path,
		Language: lang,
		Symbols:  make([]*Symbol, 0),
		Imports:  make([]string, 0),
	}

	runQuery(result, handler, tree.RootNode(), source, path, opts)

	return result, nil
}

// runQuery executes the language handler's TagsQuery against the tree root
// and populates result with symbols and imports.
func runQuery(result *ParseResult, handler LanguageHandler, root *sitter.Node, source []byte, path string, opts ParseOpts) {
	qc := sitter.NewQueryCursor()
	qc.Exec(handler.TagsQuery(), root)

	// Deduplicate symbols by "kind:name:startLine" to avoid duplicates
	// that arise when multiple captures match the same declaration node.
	seen := make(map[string]struct{})
	q := handler.TagsQuery()

	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			captureName := q.CaptureNameForId(capture.Index)
			processCapture(result, handler, captureName, capture.Node, source, path, opts, seen)
		}
	}
}

// processCapture handles a single tree-sitter query capture and updates result accordingly.
func processCapture(
	result *ParseResult,
	handler LanguageHandler,
	captureName string,
	node *sitter.Node,
	source []byte,
	path string,
	opts ParseOpts,
	seen map[string]struct{},
) {
	if captureName == captureImport {
		if opts.IncludeImports {
			// Strip surrounding quotes — languages use `"..."` or `'...'`.
			importPath := strings.Trim(node.Content(source), `"'`)
			result.Imports = append(result.Imports, importPath)
		}
		return
	}

	sym := handler.MapCapture(captureName, node, source)
	if sym == nil {
		return
	}

	dedupeKey := fmt.Sprintf("%s:%s:%d", sym.Kind, sym.Name, sym.StartLine)
	if _, exists := seen[dedupeKey]; exists {
		return
	}
	seen[dedupeKey] = struct{}{}

	sym.File = path
	sym.DocComment = extractDocComment(node, source)
	if sym.Kind == KindFunction || sym.Kind == KindMethod {
		sym.Complexity = Complexity(node.Content(source))
	}
	if opts.IncludeBody {
		sym.Body = node.Content(source)
	}
	result.Symbols = append(result.Symbols, sym)
}

// SupportedLanguages returns the list of languages that have tree-sitter grammar support.
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
	}
}

// DetectLanguageFromPath returns the language based on file extension.
// Exported so tests and other packages can use it without parsing a full file.
func DetectLanguageFromPath(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".kt", ".kts":
		return "kotlin"
	case ".php":
		return "php"
	case ".scala", ".sc":
		return "scala"
	case ".lua":
		return "lua"
	case ".pl", ".pm":
		return "perl"
	case ".swift":
		return "swift"
	case ".dart":
		return "dart"
	case ".ex", ".exs":
		return "elixir"
	default:
		return ""
	}
}

// extractDocComment looks at previous sibling nodes for comment blocks that
// form a documentation comment. It handles both // and /* */ style comments.
// Returns the doc comment text with leading comment markers stripped.
func extractDocComment(node *sitter.Node, source []byte) string {
	// Walk up to the nearest declaration-level parent if needed.
	// In Go, comments precede the declaration (function_declaration, type_declaration, etc.)
	// The node from the query may be the declaration itself, or its child.
	declNode := node
	if declNode.Parent() != nil {
		parentType := declNode.Parent().Type()
		if parentType == "source_file" || parentType == "block" || parentType == "module" ||
			parentType == "program" || parentType == "translation_unit" {
			// Already at top-level
		} else {
			// Check if parent is a declaration-level node
			declNode = declNode.Parent()
		}
	}

	// Collect consecutive comment siblings immediately preceding the declaration.
	var commentLines []string
	prev := declNode.PrevNamedSibling()
	for prev != nil && isCommentNode(prev) {
		text := prev.Content(source)
		commentLines = append([]string{text}, commentLines...)
		// Only include consecutive comments (no gap lines between them).
		if prev.EndPoint().Row+1 < declNode.StartPoint().Row {
			// Check if the gap is just blank lines before the first collected comment.
			if len(commentLines) == 1 {
				commentLines = nil
			}
			break
		}
		prev = prev.PrevNamedSibling()
	}

	if len(commentLines) == 0 {
		return ""
	}

	cleaned := make([]string, 0, len(commentLines))
	for _, line := range commentLines {
		cleaned = append(cleaned, stripCommentMarker(line))
	}
	return strings.Join(cleaned, "\n")
}

// stripCommentMarker removes leading comment syntax (// /* # ) from a single line.
func stripCommentMarker(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "//"):
		line = strings.TrimPrefix(line, "//")
		return strings.TrimPrefix(line, " ")
	case strings.HasPrefix(line, "/*") && strings.HasSuffix(line, "*/"):
		return strings.TrimSpace(line[2 : len(line)-2])
	case strings.HasPrefix(line, "#"):
		line = strings.TrimPrefix(line, "#")
		return strings.TrimPrefix(line, " ")
	default:
		return line
	}
}

// isCommentNode returns true if the node is a comment in any supported language.
func isCommentNode(node *sitter.Node) bool {
	t := node.Type()
	return t == "comment" || t == "line_comment" || t == "block_comment"
}
