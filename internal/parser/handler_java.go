//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

//go:embed queries/java.scm
var javaQueryBytes []byte

// javaHandler implements LanguageHandler for Java source files.
type javaHandler struct {
	lang  *sitter.Language
	query *sitter.Query
}

// javaLang is the singleton Java language handler, registered on package init.
var javaLang = &javaHandler{}

func init() {
	lang := java.GetLanguage()
	q, err := sitter.NewQuery(javaQueryBytes, lang)
	if err != nil {
		panic("java.scm query compile error: " + err.Error())
	}
	javaLang.lang = lang
	javaLang.query = q
	registerHandler(javaLang)
}

func (h *javaHandler) Language() string { return "java" }

func (h *javaHandler) Extensions() []string { return []string{".java"} }

func (h *javaHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *javaHandler) TagsQuery() *sitter.Query { return h.query }

// MapCapture converts a tree-sitter capture to a Symbol for Java.
func (h *javaHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureClass:
		return h.mapClass(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
	case captureType:
		return h.mapType(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	}
	return nil
}

func (h *javaHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "java",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *javaHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "java",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *javaHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "java",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *javaHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "java",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
