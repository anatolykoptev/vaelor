package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

//go:embed queries/python.scm
var pythonQueryBytes []byte

// pythonHandler implements LanguageHandler for Python source files.
type pythonHandler struct {
	lang  *sitter.Language
	query *sitter.Query
}

// pyLang is the singleton Python language handler, registered on package init.
var pyLang = &pythonHandler{}

func init() {
	lang := python.GetLanguage()
	q, err := sitter.NewQuery(pythonQueryBytes, lang)
	if err != nil {
		panic("python.scm query compile error: " + err.Error())
	}
	pyLang.lang = lang
	pyLang.query = q
	registerHandler(pyLang)
}

func (h *pythonHandler) Language() string { return "python" }

func (h *pythonHandler) Extensions() []string { return []string{".py"} }

func (h *pythonHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *pythonHandler) TagsQuery() *sitter.Query { return h.query }

// MapCapture converts a tree-sitter capture to a Symbol for Python.
func (h *pythonHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	}
	return nil
}

func (h *pythonHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "python",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *pythonHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "python",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *pythonHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "python",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
