package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
)

const nodeTypeStructSpecifier = "struct_specifier"

//go:embed queries/cpp.scm
var cppQueryBytes []byte

//go:embed queries/cpp_calls.scm
var cppCallsQueryBytes []byte

//go:embed queries/cpp_rels.scm
var cppRelsQueryBytes []byte

// cppHandler implements LanguageHandler for C++ source files.
type cppHandler struct {
	parserBase
}

// cppLang is the singleton C++ language handler, registered on package init.
var cppLang = &cppHandler{}

func init() {
	lang := cpp.GetLanguage()
	cppLang.parserBase = parserBase{
		lang: "cpp",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(cppQueryBytes, lang, "cpp.scm"),
			CallsQuery:         mustCompileQuery(cppCallsQueryBytes, lang, "cpp_calls.scm"),
			RelationshipsQuery: mustCompileQuery(cppRelsQueryBytes, lang, "cpp_rels.scm"),
			MapCapture:         cppLang.MapCapture,
		},
	}
	registerHandler(cppLang)
}

func (h *cppHandler) Extensions() []string { return []string{".cpp", ".cc", ".cxx", ".hpp"} }

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
	case captureVar:
		return h.mapVariable(node, source)
	case captureConst:
		return h.mapConst(node, source)
	}
	return nil
}

// mapFunctionOrMethod handles function_definition nodes.
// Qualified names like "Config::Config(...)" are emitted as KindMethod.
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
		Name:       name,
		Kind:       kind,
		Language:   "cpp",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isCppPublic(node, source),
		Attributes: extractCppAttributes(node, source),
	}
}

// mapMethod handles method declarations inside class bodies.
func (h *cppHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	name := cppMethodDeclName(node, source)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:       name,
		Kind:       KindMethod,
		Language:   "cpp",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isCppPublic(node, source),
		Attributes: extractCppAttributes(node, source),
	}
}

// mapClass maps class_specifier to KindClass.
func (h *cppHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindClass,
		Language:   "cpp",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isCppPublic(node, source),
		Attributes: extractCppAttributes(node, source),
	}
}

// mapType maps struct_specifier and enum_specifier captures.
// Only definitions with a body — skips type references like "struct Foo* p".
func (h *cppHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	// Skip type references without a body (e.g. "struct Foo* ptr;").
	if (node.Type() == nodeTypeStructSpecifier || node.Type() == "enum_specifier") && node.ChildByFieldName("body") == nil {
		return nil
	}
	kind := KindType
	if node.Type() == nodeTypeStructSpecifier {
		kind = KindStruct
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       kind,
		Language:   "cpp",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isCppPublic(node, source),
		Attributes: extractCppAttributes(node, source),
	}
}

// mapVariable extracts a variable symbol; const/constexpr promotes to KindConst.
func (h *cppHandler) mapVariable(node *sitter.Node, source []byte) *Symbol {
	return h.mapVarOrConst(node, source, false)
}

// mapConst extracts a constant symbol from a declaration node.
func (h *cppHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	return h.mapVarOrConst(node, source, true)
}

func (h *cppHandler) mapVarOrConst(node *sitter.Node, source []byte, forceConst bool) *Symbol {
	name := cppVarName(node, source)
	if name == "" {
		return nil
	}
	kind := KindVar
	if forceConst || isCppConst(node, source) {
		kind = KindConst
	}
	return &Symbol{
		Name:       name,
		Kind:       kind,
		Language:   "cpp",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isCppPublic(node, source),
		Attributes: extractCppAttributes(node, source),
	}
}
