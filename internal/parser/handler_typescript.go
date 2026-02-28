package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

//go:embed queries/typescript.scm
var typescriptQueryBytes []byte

//go:embed queries/typescript_calls.scm
var tsCallsQueryBytes []byte

// typescriptHandler implements LanguageHandler for TypeScript and JavaScript source files.
type typescriptHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}

// tsLang is the singleton TypeScript language handler, registered on package init.
var tsLang = &typescriptHandler{}

func init() {
	lang := typescript.GetLanguage()
	q, err := sitter.NewQuery(typescriptQueryBytes, lang)
	if err != nil {
		panic("typescript.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(tsCallsQueryBytes, lang)
	if err != nil {
		panic("typescript_calls.scm query compile error: " + err.Error())
	}
	tsLang.lang = lang
	tsLang.query = q
	tsLang.callQuery = cq
	registerHandler(tsLang)
}

func (h *typescriptHandler) Language() string { return "typescript" }

func (h *typescriptHandler) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
}

func (h *typescriptHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *typescriptHandler) TagsQuery() *sitter.Query { return h.query }

func (h *typescriptHandler) CallsQuery() *sitter.Query { return h.callQuery }

// MapCapture converts a tree-sitter capture to a Symbol for TypeScript/JavaScript.
func (h *typescriptHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
	case captureType:
		return h.mapTypeAlias(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	}
	return nil
}

func (h *typescriptHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	name := h.resolveFunctionName(node, source)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Kind:      KindFunction,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// resolveFunctionName extracts the function name from a function_declaration,
// lexical_declaration (arrow function), or export_statement wrapping one of those.
func (h *typescriptHandler) resolveFunctionName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return ""
		}
		return nameNode.Content(source)

	case "lexical_declaration", "export_statement":
		// Walk descendants to find the variable_declarator with an arrow_function value.
		return findArrowFunctionName(node, source)
	}
	return ""
}

// findArrowFunctionName walks an AST subtree looking for a variable_declarator
// whose value is an arrow_function, returning the identifier name.
func findArrowFunctionName(node *sitter.Node, source []byte) string {
	if node.Type() == "variable_declarator" {
		valueNode := node.ChildByFieldName("value")
		if valueNode != nil && valueNode.Type() == "arrow_function" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				return nameNode.Content(source)
			}
		}
	}
	for i := range int(node.ChildCount()) {
		if name := findArrowFunctionName(node.Child(i), source); name != "" {
			return name
		}
	}
	return ""
}

func (h *typescriptHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapTypeAlias(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
