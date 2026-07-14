package parser

import (
	"fmt"
	"path/filepath"
)

// callParser is implemented by every LanguageHandler through the
// parserBase.ParseWithCalls method (parserBase is embedded by all handlers).
// ParseFileWithCalls dispatches through it to run a single shared tree-sitter
// parse for both the symbol/tags query and the calls query.
type callParser interface {
	// ParseWithCalls parses src ONCE and returns the ParseResult (symbols + rels)
	// plus the call sites, both derived from the SAME parsed tree. shared reports
	// whether that single-tree path applied — see (*parserBase).ParseWithCalls.
	ParseWithCalls(path string, src []byte, opts ParseOpts) (result *ParseResult, calls []CallSite, shared bool, err error)
}

// ParseWithCalls parses src ONCE and returns the ParseResult (symbols + rels via
// buildResult) plus the call sites (via CallsQuery on the SAME root), eliminating
// the second full tree-sitter parse a separate ExtractCalls would perform over the
// identical bytes with the identical grammar (issue #400). The tags query, the
// relationships query and the calls query all run against one root through
// independent read-only query cursors, so the calls returned here are identical to
// ExtractCalls(src) and the ParseResult is identical to parserBase.Parse.
//
// shared reports whether the single-tree path applied. It is false — and
// result/calls are nil — when this handler's calls CANNOT ride its symbol-parse
// tree, detected by a nil embedded caps.SitterLanguage:
//   - the borrowed-caps preprocessor handlers (vue, svelte, astro) whose Parse
//     parses an EXTRACTED <script>/frontmatter virtual source, NOT the raw src, and
//     whose calls come from either a raw-file CallsQuery (vue) or the two-region
//     ScriptCalls/MarkupCalls split (svelte, astro) — a different tree either way;
//   - the no-grammar html handler (byte-walker, SitterLanguage nil).
//
// For those, ParseFileWithCalls transparently falls back to the independent
// Parse + ExtractCalls pairing, reproducing their exact historical output.
//
// FAIL-SAFE INVARIANT: a handler that (a) sets its OWN caps.SitterLanguage AND
// (b) overrides Parse to post-process symbols (append/relabel) MUST also override
// ParseWithCalls to apply that same post-processing after calling
// p.parserBase.ParseWithCalls — the base impl runs buildResult only. Today only
// typescriptHandler and tsxHandler do this. A handler that BORROWS caps (empty
// embedded caps) needs no override: it auto-falls-back via shared=false, so
// forgetting an override yields a slower-but-correct parse, never a wrong one.
func (p *parserBase) ParseWithCalls(path string, src []byte, opts ParseOpts) (*ParseResult, []CallSite, bool, error) {
	if p.caps.SitterLanguage == nil {
		return nil, nil, false, nil
	}

	root, closeTree, err := parseTree(p.caps.SitterLanguage, src)
	if err != nil {
		return nil, nil, true, fmt.Errorf("parse %s: %w", path, err)
	}
	defer closeTree()

	result := p.buildResult(root, src, path, opts)

	var calls []CallSite
	if p.caps.CallsQuery != nil {
		calls = runCallQuery(p.caps.CallsQuery, root, src, path)
	}
	return result, calls, true, nil
}

// ParseFileWithCalls parses a file ONCE and returns BOTH its symbol table
// (identical to ParseFile) and its call sites (identical to ExtractCalls), sharing
// a single tree-sitter parse for the mainstream path (issue #400). Every
// symbols+calls consumer (codegraph, callgraph, explore, analyze, ingest focus)
// should call this instead of ParseFile followed by ExtractCalls to avoid parsing
// the same bytes with the same grammar twice.
//
// Handlers whose calls cannot ride the symbol-parse tree (vue/svelte/astro extract
// a virtual source; html has no grammar) fall back transparently to the
// independent ParseFile + ExtractCalls pairing, so their output is byte-identical
// to before. The behavior is proven equivalent across every registered language by
// TestParseFileWithCalls_EquivalentToSeparate.
func ParseFileWithCalls(path string, source []byte, opts ParseOpts) (*ParseResult, []CallSite, error) {
	lang := resolveLanguage(path, opts)
	if lang == "" {
		return nil, nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(path))
	}

	handler := HandlerForExt(filepath.Ext(path))
	if handler == nil {
		// No tree-sitter grammar — regex fallback tokenizer. ExtractCalls returns no
		// calls for a nil handler, so calls is nil here too (matches ParseFile+ExtractCalls).
		return fallbackParse(path, source, lang), nil, nil
	}

	if cp, ok := handler.(callParser); ok {
		result, calls, shared, err := cp.ParseWithCalls(path, source, opts)
		if err != nil {
			return nil, nil, err
		}
		if shared {
			result.Language = lang // mirror ParseFile's post-parse language override
			return result, calls, nil
		}
	}

	return parseFileThenExtractCalls(handler, path, source, opts, lang)
}

// parseFileThenExtractCalls is the separate-parse fallback for handlers whose calls
// do not share the symbol-parse tree. It reproduces the exact ParseFile +
// ExtractCalls behavior, including ParseFile's result.Language override.
//
// ExtractCalls never returns a non-nil error in practice (a deterministic
// tree-sitter parse over already-readable bytes with a non-nil grammar), and the
// historical callers only debug-logged that error and never dropped the file; so on
// the theoretical error the file is kept with empty calls, preserving call-graph
// output.
func parseFileThenExtractCalls(handler LanguageHandler, path string, source []byte, opts ParseOpts, lang string) (*ParseResult, []CallSite, error) {
	result, err := handler.Parse(path, source, opts)
	if err != nil {
		return nil, nil, err
	}
	result.Language = lang

	calls, _ := ExtractCalls(path, source, opts)
	return result, calls, nil
}
