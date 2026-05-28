//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/kotlin"
)

//go:embed queries/kotlin.scm
var kotlinQueryBytes []byte

//go:embed queries/kotlin_calls.scm
var kotlinCallsQueryBytes []byte

//go:embed queries/kotlin_rels.scm
var kotlinRelsQueryBytes []byte

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
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(kotlinQueryBytes, lang, "kotlin.scm"),
			CallsQuery:         mustCompileQuery(kotlinCallsQueryBytes, lang, "kotlin_calls.scm"),
			RelationshipsQuery: mustCompileQuery(kotlinRelsQueryBytes, lang, "kotlin_rels.scm"),
			MapCapture:         kotlinLang.MapCapture,
		},
	}
	registerHandler(kotlinLang)
}

func (h *kotlinHandler) Extensions() []string { return []string{".kt", ".kts"} }

// MapCapture converts a tree-sitter capture to a Symbol for Kotlin.
func (h *kotlinHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureClass:
		// The Kotlin grammar uses class_declaration for both class and interface
		// declarations (with an "interface" keyword child instead of "class").
		// Disambiguate at MapCapture time so the tags query needs no separate pattern.
		if kotlinIsInterface(node) {
			return h.mapInterface(node, source)
		}
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

// kotlinIsInterface reports whether a class_declaration node was declared with
// the "interface" keyword. The Kotlin grammar represents both class and interface
// declarations as class_declaration; the distinguishing child is an unnamed token
// of type "interface" vs "class".
func kotlinIsInterface(node *sitter.Node) bool {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child != nil && child.Type() == "interface" {
			return true
		}
	}
	return false
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

func (h *kotlinHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := kotlinNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
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
