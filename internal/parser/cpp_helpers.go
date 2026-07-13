package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// cppKeywords are C++ specifier keywords tracked as symbol attributes.
var cppKeywords = []string{
	"virtual", "override", "static", "constexpr",
	"inline", "explicit", "noexcept", "friend",
}

// isCppPublic determines visibility for a C++ AST node.
// Top-level and namespace-scoped symbols are always public.
// Class/struct members check the nearest preceding access_specifier.
func isCppPublic(node *sitter.Node, source []byte) bool {
	parent := node.Parent()
	if parent == nil || parent.Type() != "field_declaration_list" {
		return true // file/namespace scope — no access control
	}
	idx := nodeIndex(node, parent)
	for i := idx - 1; i >= 0; i-- {
		sib := parent.Child(i)
		if sib != nil && sib.Type() == "access_specifier" {
			text := strings.TrimSpace(strings.TrimSuffix(sib.Content(source), ":"))
			return text == "public"
		}
	}
	// Default: struct=public, class=private.
	if gp := parent.Parent(); gp != nil && gp.Type() == nodeTypeStructSpecifier {
		return true
	}
	return false
}

// extractCppAttributes scans a node's children for C++ specifier keywords.
func extractCppAttributes(node *sitter.Node, source []byte) []string {
	var attrs []string
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type()
		switch ct {
		case "virtual", "storage_class_specifier", "type_qualifier", "virtual_specifier":
			if kw := matchCppKeyword(child.Content(source)); kw != "" {
				attrs = append(attrs, kw)
			}
		case nodeFunctionDeclarator:
			attrs = append(attrs, scanDeclChildren(child, source)...)
		}
	}
	// Check preceding sibling for "friend" keyword.
	if parent := node.Parent(); parent != nil {
		idx := nodeIndex(node, parent)
		for i := idx - 1; i >= 0; i-- {
			sib := parent.Child(i)
			if sib == nil || sib.Type() == "comment" {
				continue
			}
			if sib.Content(source) == "friend" {
				attrs = append(attrs, "friend")
			}
			break
		}
	}
	return attrs
}

// scanDeclChildren extracts trailing qualifiers from a function_declarator.
func scanDeclChildren(decl *sitter.Node, source []byte) []string {
	var attrs []string
	count := int(decl.ChildCount())
	for i := range count {
		child := decl.Child(i)
		if child == nil {
			continue
		}
		if kw := matchCppKeyword(child.Content(source)); kw != "" {
			attrs = append(attrs, kw)
		}
	}
	return attrs
}

// matchCppKeyword returns the keyword if text matches a tracked C++ keyword.
func matchCppKeyword(text string) string {
	for _, kw := range cppKeywords {
		if text == kw {
			return kw
		}
	}
	return ""
}

// cppVarName extracts the variable name from a declaration node.
func cppVarName(node *sitter.Node, source []byte) string {
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return ""
	}
	return cppVarNameFromDeclarator(decl, source)
}

// cppVarNameFromDeclarator recursively walks a declarator to find the identifier.
func cppVarNameFromDeclarator(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case nodeIdentifier, nodeFieldIdentifier:
		return node.Content(source)
	case "init_declarator":
		inner := node.ChildByFieldName("declarator")
		return cppVarNameFromDeclarator(inner, source)
	case nodePointerDeclarator, nodeReferenceDeclarator:
		inner := node.ChildByFieldName("declarator")
		return cppVarNameFromDeclarator(inner, source)
	}
	return ""
}

// isCppConst checks if a declaration node has const or constexpr specifiers.
func isCppConst(node *sitter.Node, source []byte) bool {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child == nil {
			continue
		}
		text := child.Content(source)
		if text == "const" || text == "constexpr" {
			return true
		}
	}
	return false
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

// cppMethodDeclName extracts the method name from a declaration or field_declaration
// node inside a class body.
func cppMethodDeclName(node *sitter.Node, source []byte) string {
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
