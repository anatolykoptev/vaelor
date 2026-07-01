package parser

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// CallSite represents a single function/method call extracted from source code.
type CallSite struct {
	Name     string // called function or method name
	Receiver string // qualifier for method calls (e.g. "fmt" in "fmt.Println"), empty for plain calls
	Line     uint32 // 1-based line number
	File     string // absolute file path
	// IsArgRef is true when this site comes from a heuristic argument-position
	// or struct-literal-value capture (e.g. `Register("x", handler)` —
	// `handler`). Most of these are plain values, not function references, so
	// the call graph drops unresolved IsArgRef sites unless the caller opts in
	// via the MCP field_access=true flag (filtered in the callgraph layer).
	IsArgRef bool
}

// scriptCallSource is implemented by preprocessor-language handlers (Astro,
// Svelte) whose CALLS must be extracted from the language's <script> /
// frontmatter VirtualSource, NOT from a raw CallsQuery over the whole file.
//
// A .svelte/.astro file is not valid TypeScript, so running the delegated
// grammar's CallsQuery over the RAW bytes makes tree-sitter error-recovery
// surface the TEMPLATE body's calls too — but GARBLED (e.g. `<p>{user.greet()}</p>`
// yields greet with Receiver "{user"). Those then DUPLICATE the clean template
// calls that MarkupCalls produces (see markupCallSource), and the call graph
// (callgraph.BuildCallGraphWithOpts) has no dedup, so the same edge lands twice.
//
// Handlers implementing this interface own their script-region call extraction;
// ExtractCalls runs ScriptCalls (clean, line-remapped from the extracted script
// VirtualSource) INSTEAD OF the raw CallsQuery for them, so the template region
// is served solely by MarkupCalls. Result: exactly ONE producer per region —
// script calls from ScriptCalls, template calls from MarkupCalls, no overlap, no
// garbled error-recovery edge. Handlers that do not implement it are unaffected
// (they keep the raw CallsQuery path).
type scriptCallSource interface {
	ScriptCalls(path string, src []byte, opts ParseOpts) []CallSite
}

// markupCallSource is implemented by preprocessor-language handlers (Astro,
// Svelte) whose TEMPLATE body carries {expr} / block-header call sites. It is the
// SOLE producer of the template region's calls: for handlers that also implement
// scriptCallSource, ExtractCalls does not run a raw CallsQuery over the template,
// so there is no second (garbled) producer to duplicate these. Optional: handlers
// that do not implement it are unaffected. Invoked independently of CallsQuery, so
// a future markup-only handler with a nil CallsQuery still contributes markup calls.
type markupCallSource interface {
	MarkupCalls(path string, src []byte, opts ParseOpts) []CallSite
}

// ExtractCalls parses a source file and returns all function/method call sites.
// Returns empty slice (not error) for unsupported languages.
//
// Preprocessor-language handlers (Astro, Svelte) split call extraction into two
// single-producer regions — script/frontmatter via ScriptCalls, template body via
// MarkupCalls — instead of a raw CallsQuery over the whole (non-TS) file, which
// would double-emit garbled template calls. Every other language uses the raw
// CallsQuery path.
func ExtractCalls(path string, source []byte, opts ParseOpts) ([]CallSite, error) {
	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	var calls []CallSite

	if sc, ok := handler.(scriptCallSource); ok {
		// Preprocessor handler: script-region calls only (clean, remapped). The
		// raw CallsQuery is deliberately NOT run — it would surface garbled
		// template calls that duplicate MarkupCalls below.
		calls = append(calls, sc.ScriptCalls(path, source, opts)...)
	} else if caps := handler.Capabilities(); caps.CallsQuery != nil {
		p := sitter.NewParser()
		defer p.Close()
		p.SetLanguage(caps.SitterLanguage)

		tree, err := p.ParseCtx(context.Background(), nil, source)
		if err != nil {
			return nil, err
		}
		defer tree.Close()

		calls = runCallQuery(caps.CallsQuery, tree.RootNode(), source, path)
	}

	// Template-body calls (Astro, Svelte). For scriptCallSource handlers this is
	// the SOLE producer of the template region — no duplicate, no garbled edge.
	if mc, ok := handler.(markupCallSource); ok {
		calls = append(calls, mc.MarkupCalls(path, source, opts)...)
	}

	return calls, nil
}

func runCallQuery(q *sitter.Query, root *sitter.Node, source []byte, path string) []CallSite {
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var calls []CallSite
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			capName := q.CaptureNameForId(capture.Index)
			node := capture.Node

			switch capName {
			case captureCallFunction:
				calls = append(calls, CallSite{
					Name: node.Content(source),
					Line: node.StartPoint().Row + 1,
					File: path,
				})
			case captureCallMethod:
				calls = append(calls, CallSite{
					Name:     node.Content(source),
					Receiver: extractCallReceiver(node, source),
					Line:     node.StartPoint().Row + 1,
					File:     path,
				})
			case captureCallArgRef:
				calls = append(calls, CallSite{
					Name:     node.Content(source),
					Line:     node.StartPoint().Row + 1,
					File:     path,
					IsArgRef: true,
				})
			case captureCallArgRefMethod:
				calls = append(calls, CallSite{
					Name:     node.Content(source),
					Receiver: extractCallReceiver(node, source),
					Line:     node.StartPoint().Row + 1,
					File:     path,
					IsArgRef: true,
				})
			}
		}
	}

	return calls
}

func extractCallReceiver(methodNode *sitter.Node, source []byte) string {
	parent := methodNode.Parent()
	if parent == nil {
		return ""
	}

	for _, fieldName := range []string{"operand", "object"} {
		obj := parent.ChildByFieldName(fieldName)
		if obj != nil {
			text := obj.Content(source)
			if idx := strings.LastIndexByte(text, '.'); idx >= 0 {
				return text[idx+1:]
			}
			return text
		}
	}

	if parent.NamedChildCount() > 0 {
		first := parent.NamedChild(0)
		if first != nil && first != methodNode {
			text := first.Content(source)
			if idx := strings.LastIndexByte(text, '.'); idx >= 0 {
				return text[idx+1:]
			}
			return text
		}
	}

	return ""
}
