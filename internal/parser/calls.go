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
}

// ExtractCalls parses a source file and returns all function/method call sites.
// Returns empty slice (not error) for unsupported languages.
func ExtractCalls(path string, source []byte, opts ParseOpts) ([]CallSite, error) {
	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	cqp, ok := handler.(CallQueryProvider)
	if !ok || cqp.CallsQuery() == nil {
		return nil, nil
	}

	p := sitter.NewParser()
	p.SetLanguage(handler.SitterLanguage())

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}

	return runCallQuery(cqp.CallsQuery(), tree.RootNode(), source, path), nil
}

func runCallQuery(q *sitter.Query, root *sitter.Node, source []byte, path string) []CallSite {
	qc := sitter.NewQueryCursor()
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
