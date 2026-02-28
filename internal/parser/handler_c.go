package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
)

// C/C++ tree-sitter node type name constants shared across C and C++ handlers.
const (
	nodeIdentifier        = "identifier"
	nodeFieldIdentifier   = "field_identifier"
	nodeFunctionDeclarator = "function_declarator"
	nodePointerDeclarator  = "pointer_declarator"
	nodeReferenceDeclarator = "reference_declarator"
	nodeQualifiedIdentifier = "qualified_identifier"
)

//go:embed queries/c.scm
var cQueryBytes []byte

// cHandler implements LanguageHandler for C source files.
type cHandler struct {
	lang  *sitter.Language
	query *sitter.Query
}

// cLang is the singleton C language handler, registered on package init.
var cLang = &cHandler{}

func init() {
	lang := c.GetLanguage()
	q, err := sitter.NewQuery(cQueryBytes, lang)
	if err != nil {
		panic("c.scm query compile error: " + err.Error())
	}
	cLang.lang = lang
	cLang.query = q
	registerHandler(cLang)
}

func (h *cHandler) Language() string { return "c" }

func (h *cHandler) Extensions() []string { return []string{".c", ".h"} }

func (h *cHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *cHandler) TagsQuery() *sitter.Query { return h.query }

// MapCapture converts a tree-sitter capture to a Symbol for C.
func (h *cHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureType:
		return h.mapType(node, source)
	}
	return nil
}

// mapFunction extracts a function symbol from a function_definition or declaration node.
func (h *cHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	name := cFunctionName(node, source)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Kind:      KindFunction,
		Language:  "c",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// mapType maps struct_specifier, type_definition, and enum_specifier to the correct kind.
func (h *cHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	var name string
	kind := KindType

	switch node.Type() {
	case "struct_specifier":
		// Named struct definition: "struct Server { ... }"
		// Skip type references like "struct Node* next;" (no body).
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return nil
		}
		if node.ChildByFieldName("body") == nil {
			return nil
		}
		name = nameNode.Content(source)
		kind = KindStruct

	case "type_definition":
		// Typedef: "typedef struct { ... } Config;"
		// The declarator field is the type_identifier (the alias name).
		declNode := node.ChildByFieldName("declarator")
		if declNode == nil {
			return nil
		}
		name = declNode.Content(source)

	case "enum_specifier":
		// Named enum definition: "enum Status { ... }"
		// Skip type references like "enum LogLevel level" (no body).
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return nil
		}
		if node.ChildByFieldName("body") == nil {
			return nil
		}
		name = nameNode.Content(source)
	}

	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Kind:      kind,
		Language:  "c",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// cFunctionName navigates the C AST to extract the function name from
// a function_definition or declaration node.
func cFunctionName(node *sitter.Node, source []byte) string {
	// For function_definition: node → declarator (function_declarator) → declarator (identifier)
	// For declaration with pointer: node → declarator (pointer_declarator) → declarator (function_declarator) → declarator (identifier)
	// For declaration without pointer: node → declarator (function_declarator) → declarator (identifier)
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return ""
	}
	return findIdentifierInDeclarator(decl, source)
}

// findIdentifierInDeclarator recursively extracts the identifier from a declarator chain.
func findIdentifierInDeclarator(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case nodeIdentifier:
		return node.Content(source)
	case nodeFunctionDeclarator:
		inner := node.ChildByFieldName("declarator")
		return findIdentifierInDeclarator(inner, source)
	case nodePointerDeclarator:
		inner := node.ChildByFieldName("declarator")
		return findIdentifierInDeclarator(inner, source)
	}
	return ""
}
