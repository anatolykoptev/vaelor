//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/kotlin"
)

//go:embed queries/kotlin.scm
var kotlinQueryBytes []byte

// kotlinHandler implements LanguageHandler for Kotlin source files.
type kotlinHandler struct {
	parserBase
}

// kotlinLang is the singleton Kotlin language handler, registered on package init.
var kotlinLang = &kotlinHandler{}

func init() {
	lang := kotlin.GetLanguage()
	kotlinLang.parserBase = parserBase{
		lang: "kotlin",
		caps: Capabilities{
			SitterLanguage: lang,
			TagsQuery:      mustCompileQuery(kotlinQueryBytes, lang, "kotlin.scm"),
			MapCapture:     kotlinLang.MapCapture,
		},
	}
	registerHandler(kotlinLang)
}

func (h *kotlinHandler) Extensions() []string { return []string{".kt", ".kts"} }

// MapCapture converts a tree-sitter capture to a Symbol for Kotlin.
func (h *kotlinHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureClass:
		return h.mapClass(node, source)
	case captureFunction:
		return h.mapFunction(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureVar:
		return h.mapVar(node, source)
	case captureType:
		return h.mapTypeAlias(node, source)
	}
	return nil
}

// kotlinNameNode finds the first child node with the given type string.
// The Kotlin grammar (smacker/go-tree-sitter @ dd81d9e) defines FIELD_COUNT=0,
// so named-field lookup via ChildByFieldName is unavailable; we scan children
// by type instead.
func kotlinNameNode(parent *sitter.Node, childType string) *sitter.Node {
	count := int(parent.ChildCount())
	for i := range count {
		child := parent.Child(i)
		if child != nil && child.Type() == childType {
			return child
		}
	}
	return nil
}

func (h *kotlinHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := kotlinNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "kotlin",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *kotlinHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := kotlinNameNode(node, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "kotlin",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *kotlinHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := kotlinNameNode(node, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "kotlin",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *kotlinHandler) mapVar(node *sitter.Node, source []byte) *Symbol {
	// property_declaration: (variable_declaration (simple_identifier) ...) — drill one level.
	varDecl := kotlinNameNode(node, "variable_declaration")
	if varDecl == nil {
		return nil
	}
	nameNode := kotlinNameNode(varDecl, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindVar,
		Language:  "kotlin",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *kotlinHandler) mapTypeAlias(node *sitter.Node, source []byte) *Symbol {
	nameNode := kotlinNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "kotlin",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
