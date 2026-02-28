package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"
)

//go:embed queries/csharp.scm
var csharpQueryBytes []byte

//go:embed queries/csharp_calls.scm
var csharpCallsQueryBytes []byte

// csharpHandler implements LanguageHandler for C# source files.
type csharpHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}

// csharpLang is the singleton C# language handler, registered on package init.
var csharpLang = &csharpHandler{}

func init() {
	lang := csharp.GetLanguage()
	q, err := sitter.NewQuery(csharpQueryBytes, lang)
	if err != nil {
		panic("csharp.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(csharpCallsQueryBytes, lang)
	if err != nil {
		panic("csharp_calls.scm query compile error: " + err.Error())
	}
	csharpLang.lang = lang
	csharpLang.query = q
	csharpLang.callQuery = cq
	registerHandler(csharpLang)
}

func (h *csharpHandler) Language() string { return "csharp" }

func (h *csharpHandler) Extensions() []string { return []string{".cs"} }

func (h *csharpHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *csharpHandler) TagsQuery() *sitter.Query { return h.query }

func (h *csharpHandler) CallsQuery() *sitter.Query { return h.callQuery }

// MapCapture converts a tree-sitter capture to a Symbol for C#.
func (h *csharpHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
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

func (h *csharpHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "csharp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *csharpHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "csharp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// mapType maps namespace_declaration, struct_declaration, and enum_declaration
// to the correct kind. Structs use KindStruct; all others use KindType.
func (h *csharpHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	kind := KindType
	if node.Type() == "struct_declaration" {
		kind = KindStruct
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      kind,
		Language:  "csharp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *csharpHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "csharp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
