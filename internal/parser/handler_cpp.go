package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
)

//go:embed queries/cpp.scm
var cppQueryBytes []byte

//go:embed queries/cpp_calls.scm
var cppCallsQueryBytes []byte

// cppHandler implements LanguageHandler for C++ source files.
type cppHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}

// cppLang is the singleton C++ language handler, registered on package init.
var cppLang = &cppHandler{}

func init() {
	lang := cpp.GetLanguage()
	q, err := sitter.NewQuery(cppQueryBytes, lang)
	if err != nil {
		panic("cpp.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(cppCallsQueryBytes, lang)
	if err != nil {
		panic("cpp_calls.scm query compile error: " + err.Error())
	}
	cppLang.lang = lang
	cppLang.query = q
	cppLang.callQuery = cq
	registerHandler(cppLang)
}

func (h *cppHandler) Language() string { return "cpp" }

func (h *cppHandler) Extensions() []string { return []string{".cpp", ".cc", ".cxx", ".hpp"} }

func (h *cppHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *cppHandler) TagsQuery() *sitter.Query { return h.query }

func (h *cppHandler) CallsQuery() *sitter.Query { return h.callQuery }

// MapCapture converts a tree-sitter capture to a Symbol for C++.
func (h *cppHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunctionOrMethod(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureType:
		return h.mapType(node, source)
	}
	return nil
}

// mapFunctionOrMethod handles function_definition nodes.
// Qualified definitions like "Config::Config(...)" are emitted as KindMethod.
// Simple definitions like "run(...)" are emitted as KindFunction.
func (h *cppHandler) mapFunctionOrMethod(node *sitter.Node, source []byte) *Symbol {
	name, isQualified := cppFunctionName(node, source)
	if name == "" {
		return nil
	}
	kind := KindFunction
	if isQualified {
		kind = KindMethod
	}
	return &Symbol{
		Name:      name,
		Kind:      kind,
		Language:  "cpp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// mapMethod handles method declarations inside class bodies.
func (h *cppHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	name := cppMethodDeclName(node, source)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Kind:      KindMethod,
		Language:  "cpp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// mapClass maps class_specifier to KindClass.
func (h *cppHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "cpp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// mapType maps struct_specifier and enum_specifier captures.
// struct_specifier → KindStruct, everything else → KindType.
// Only captures definitions with a body — skips type references like "struct Foo* p".
func (h *cppHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	// Skip type references without a body (e.g. "struct Foo* ptr;").
	if (node.Type() == "struct_specifier" || node.Type() == "enum_specifier") && node.ChildByFieldName("body") == nil {
		return nil
	}
	kind := KindType
	if node.Type() == "struct_specifier" {
		kind = KindStruct
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      kind,
		Language:  "cpp",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// cppFunctionName extracts the function name from a function_definition node.
// Returns the name and whether it is a qualified name (Class::method).
func cppFunctionName(node *sitter.Node, source []byte) (string, bool) {
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return "", false
	}
	return cppNameFromDeclarator(decl, source)
}

// cppNameFromDeclarator recursively walks a declarator chain to extract the name.
func cppNameFromDeclarator(node *sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Type() {
	case nodeIdentifier:
		return node.Content(source), false
	case nodeQualifiedIdentifier:
		// e.g. "Config::Config" — treat as method.
		return node.Content(source), true
	case nodeFunctionDeclarator:
		inner := node.ChildByFieldName("declarator")
		return cppNameFromDeclarator(inner, source)
	case nodePointerDeclarator, nodeReferenceDeclarator:
		inner := node.ChildByFieldName("declarator")
		return cppNameFromDeclarator(inner, source)
	}
	return "", false
}

// cppMethodDeclName extracts the method name from a declaration or field_declaration node
// inside a class body.
func cppMethodDeclName(node *sitter.Node, source []byte) string {
	// declaration: declarator = function_declarator → declarator = identifier
	// field_declaration: declarator = function_declarator → declarator = field_identifier
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return ""
	}
	return cppMethodInnerName(decl, source)
}

// cppMethodInnerName navigates into function_declarator to find the method name.
func cppMethodInnerName(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case nodeIdentifier, nodeFieldIdentifier:
		return node.Content(source)
	case nodeFunctionDeclarator:
		inner := node.ChildByFieldName("declarator")
		return cppMethodInnerName(inner, source)
	case nodePointerDeclarator, nodeReferenceDeclarator:
		inner := node.ChildByFieldName("declarator")
		return cppMethodInnerName(inner, source)
	}
	return ""
}
