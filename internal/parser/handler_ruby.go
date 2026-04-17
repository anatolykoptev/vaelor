//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/ruby"
)

//go:embed queries/ruby.scm
var rubyQueryBytes []byte

//go:embed queries/ruby_calls.scm
var rubyCallsQueryBytes []byte

// rubyHandler implements LanguageHandler for Ruby source files.
type rubyHandler struct {
	parserBase
}

// rubyLang is the singleton Ruby language handler, registered on package init.
var rubyLang = &rubyHandler{}

func init() {
	lang := ruby.GetLanguage()
	rubyLang.parserBase = parserBase{
		lang: "ruby",
		caps: Capabilities{
			SitterLanguage: lang,
			TagsQuery:      mustCompileQuery(rubyQueryBytes, lang, "ruby.scm"),
			CallsQuery:     mustCompileQuery(rubyCallsQueryBytes, lang, "ruby_calls.scm"),
			MapCapture:     rubyLang.MapCapture,
		},
	}
	registerHandler(rubyLang)
}

func (h *rubyHandler) Extensions() []string { return []string{".rb"} }

// MapCapture converts a tree-sitter capture to a Symbol for Ruby.
func (h *rubyHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureType:
		return h.mapModule(node, source)
	case captureConst:
		return h.mapConst(node, source)
	}
	return nil
}

func (h *rubyHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "ruby",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *rubyHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "ruby",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *rubyHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "ruby",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *rubyHandler) mapModule(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "ruby",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *rubyHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("left")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindConst,
		Language:  "ruby",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
