package parser

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Capabilities holds the compiled tree-sitter queries and language reference
// for a LanguageHandler. Fields are nil for handlers that do not support a
// given capability (e.g. no call extraction, no relationship extraction).
type Capabilities struct {
	// SitterLanguage is the tree-sitter grammar. Nil for fallback/regex-only handlers.
	SitterLanguage *sitter.Language

	// TagsQuery is the compiled query for symbol extraction (functions, types, etc.).
	TagsQuery *sitter.Query

	// CallsQuery is the compiled query for call-site extraction. Nil if unsupported.
	CallsQuery *sitter.Query

	// RelationshipsQuery is the compiled query for type relationship extraction. Nil if unsupported.
	RelationshipsQuery *sitter.Query

	// MapCapture converts a single tree-sitter capture into a Symbol.
	// Bound to the handler method so it can access handler-specific logic.
	// Returns nil if the capture should be skipped.
	MapCapture func(captureName string, node *sitter.Node, src []byte) *Symbol
}

// parserBase is a composable base for LanguageHandler implementations.
// It stores the language name and its Capabilities, and provides a default
// Parse implementation using tree-sitter. Handlers that need custom parsing
// (e.g. preprocessor languages) override Parse.
type parserBase struct {
	lang string
	caps Capabilities
}

// Language returns the canonical language name (e.g. "go", "python").
func (p *parserBase) Language() string { return p.lang }

// Capabilities returns the handler's tree-sitter capabilities.
func (p *parserBase) Capabilities() Capabilities { return p.caps }

// Parse is the default tree-sitter–based parse implementation.
// If SitterLanguage is nil, it falls back to regex-based tokenization.
// Handlers that need custom preprocessing should embed parserBase and override Parse.
func (p *parserBase) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	if p.caps.SitterLanguage == nil {
		return fallbackParse(path, src, p.lang), nil
	}

	ps := sitter.NewParser()
	defer ps.Close()
	ps.SetLanguage(p.caps.SitterLanguage)

	tree, err := ps.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	defer tree.Close()

	result := &ParseResult{
		File:     path,
		Language: p.lang,
		Symbols:  make([]*Symbol, 0),
		Imports:  make([]string, 0),
	}
	runQueryWithCaps(result, p.caps, tree.RootNode(), src, path, opts)
	return result, nil
}

// mustCompileQuery compiles a tree-sitter query or panics with a descriptive message.
// Used in handler init() functions where a query compile failure is a programming error.
func mustCompileQuery(src []byte, lang *sitter.Language, name string) *sitter.Query {
	q, err := sitter.NewQuery(src, lang)
	if err != nil {
		panic(name + " query compile error: " + err.Error())
	}
	return q
}

// runQueryWithCaps executes the TagsQuery from caps against the tree root
// and populates result with symbols and imports.
func runQueryWithCaps(result *ParseResult, caps Capabilities, root *sitter.Node, source []byte, path string, opts ParseOpts) {
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(caps.TagsQuery, root)

	seen := make(map[string]struct{})
	q := caps.TagsQuery

	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			captureName := q.CaptureNameForId(capture.Index)
			processCaptureWithCaps(result, caps, captureName, capture.Node, source, path, opts, seen)
		}
	}
}

// processCaptureWithCaps handles a single tree-sitter query capture and updates result.
func processCaptureWithCaps(
	result *ParseResult,
	caps Capabilities,
	captureName string,
	node *sitter.Node,
	source []byte,
	path string,
	opts ParseOpts,
	seen map[string]struct{},
) {
	if captureName == captureImport {
		if opts.IncludeImports {
			importPath := trimQuotes(node.Content(source))
			result.Imports = append(result.Imports, importPath)
		}
		return
	}

	sym := caps.MapCapture(captureName, node, source)
	if sym == nil {
		return
	}

	key := fmt.Sprintf("%s:%s:%d", sym.Kind, sym.Name, sym.StartLine)
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}

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

// trimQuotes strips surrounding single or double quotes from an import path.
// Languages use both `"..."` and `'...'` for import/require strings.
func trimQuotes(s string) string {
	return strings.Trim(s, `"'`)
}
