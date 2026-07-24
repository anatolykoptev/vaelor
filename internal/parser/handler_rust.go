package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
)

//go:embed queries/rust.scm
var rustQueryBytes []byte

//go:embed queries/rust_calls.scm
var rustCallsQueryBytes []byte

//go:embed queries/rust_rels.scm
var rustRelsQueryBytes []byte

// rustHandler implements LanguageHandler for Rust source files.
type rustHandler struct {
	parserBase
}

// rustLang is the singleton Rust language handler, registered on package init.
var rustLang = &rustHandler{}

func init() {
	lang := rust.GetLanguage()
	rustLang.parserBase = parserBase{
		lang: "rust",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(rustQueryBytes, lang, "rust.scm"),
			CallsQuery:         mustCompileQuery(rustCallsQueryBytes, lang, "rust_calls.scm"),
			RelationshipsQuery: mustCompileQuery(rustRelsQueryBytes, lang, "rust_rels.scm"),
			MapCapture:         rustLang.MapCapture,
		},
	}
	registerHandler(rustLang)
}

func (h *rustHandler) Extensions() []string { return []string{".rs"} }

// MapCapture converts a tree-sitter capture to a Symbol for Rust.
func (h *rustHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureType:
		return h.mapType(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
	case captureConst:
		return h.mapConst(node, source)
	case captureVar:
		return h.mapVar(node, source)
	case captureMacro:
		return h.mapMacro(node, source)
	case captureModule:
		return h.mapModule(node, source)
	}
	return nil
}

func (h *rustHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindFunction,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

func (h *rustHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindMethod,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
		Receiver:   implReceiver(node, source),
	}
}

// mapType maps struct_item, enum_item, and type_item captures to the correct kind.
func (h *rustHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	kind := KindType
	if node.Type() == "struct_item" {
		kind = KindStruct
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       kind,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

func (h *rustHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindInterface,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

func (h *rustHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindConst,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

func (h *rustHandler) mapVar(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindVar,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

// mapMacro extracts a macro_rules! definition as a KindMacro symbol (#664).
func (h *rustHandler) mapMacro(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindMacro,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}

// mapModule extracts a mod declaration as a KindModule symbol (#664).
func (h *rustHandler) mapModule(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindModule,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
