//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

//go:embed queries/php.scm
var phpQueryBytes []byte

//go:embed queries/php_calls.scm
var phpCallsQueryBytes []byte

// phpHandler implements LanguageHandler for PHP source files.
type phpHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}

// phpLang is the singleton PHP language handler, registered on package init.
var phpLang = &phpHandler{}

func init() {
	lang := php.GetLanguage()
	q, err := sitter.NewQuery(phpQueryBytes, lang)
	if err != nil {
		panic("php.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(phpCallsQueryBytes, lang)
	if err != nil {
		panic("php_calls.scm query compile error: " + err.Error())
	}
	phpLang.lang = lang
	phpLang.query = q
	phpLang.callQuery = cq
	registerHandler(phpLang)
}

func (h *phpHandler) Language() string { return "php" }

func (h *phpHandler) Extensions() []string { return []string{".php"} }

func (h *phpHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *phpHandler) TagsQuery() *sitter.Query { return h.query }

func (h *phpHandler) CallsQuery() *sitter.Query { return h.callQuery }

// MapCapture converts a tree-sitter capture to a Symbol for PHP.
func (h *phpHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
	case captureType:
		return h.mapTrait(node, source)
	case captureConst:
		return h.mapConst(node, source)
	}
	return nil
}

func (h *phpHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *phpHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *phpHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *phpHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *phpHandler) mapTrait(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *phpHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	// const_element has (name) as a positional child, not a named field.
	var nameNode *sitter.Node
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if child := node.NamedChild(i); child.Type() == "name" {
			nameNode = child
			break
		}
	}
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindConst,
		Language:  "php",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
