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

// markupCallSource is implemented by preprocessor-language handlers whose
// template body carries {expr} call sites that parsing the raw file with the
// delegated grammar cannot reach (Astro today; Svelte in a later phase).
// ExtractCalls appends these to the ordinary call sites. Optional: handlers that
// do not implement it are unaffected.
type markupCallSource interface {
	MarkupCalls(path string, src []byte, opts ParseOpts) []CallSite
}

// ExtractCalls parses a source file and returns all function/method call sites.
// Returns empty slice (not error) for unsupported languages.
func ExtractCalls(path string, source []byte, opts ParseOpts) ([]CallSite, error) {
	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	caps := handler.Capabilities()
	if caps.CallsQuery == nil {
		return nil, nil
	}

	p := sitter.NewParser()
	defer p.Close()
	p.SetLanguage(caps.SitterLanguage)

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	calls := runCallQuery(caps.CallsQuery, tree.RootNode(), source, path)

	// Preprocessor-language handlers (Astro) additionally surface calls embedded
	// in template-body {expr} ranges, unreachable by parsing the raw file with
	// the delegated grammar above.
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
