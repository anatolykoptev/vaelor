//nolint:dupl // Language handlers are intentionally similar — each links a separate C grammar.
package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/swift"
)

//go:embed queries/swift.scm
var swiftQueryBytes []byte

//go:embed queries/swift_calls.scm
var swiftCallsQueryBytes []byte

//go:embed queries/swift_rels.scm
var swiftRelsQueryBytes []byte

// swiftHandler implements LanguageHandler for Swift source files.
type swiftHandler struct {
	parserBase
}

// swiftLang is the singleton Swift language handler, registered on package init.
var swiftLang = &swiftHandler{}

func init() {
	lang := swift.GetLanguage()
	swiftLang.parserBase = parserBase{
		lang: "swift",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(swiftQueryBytes, lang, "swift.scm"),
			CallsQuery:         mustCompileQuery(swiftCallsQueryBytes, lang, "swift_calls.scm"),
			RelationshipsQuery: mustCompileQuery(swiftRelsQueryBytes, lang, "swift_rels.scm"),
			MapCapture:         swiftLang.MapCapture,
		},
	}
	registerHandler(swiftLang)
}

func (h *swiftHandler) Extensions() []string { return []string{".swift"} }

// MapCapture converts a tree-sitter capture to a Symbol for Swift.
func (h *swiftHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureClass:
		// The Swift grammar uses class_declaration for class, struct, enum, actor,
		// and extension declarations. Disambiguate at MapCapture time.
		// protocol_declaration is a separate node and maps to KindInterface.
		if swiftIsExtension(node) {
			// Extensions don't create a new type — skip the container itself.
			// Methods inside the extension body are captured via captureMethod.
			return nil
		}
		return h.mapClass(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
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

// swiftNameNode finds the first child node with the given type string.
// The Swift grammar (smacker/go-tree-sitter @ dd81d9e) has FIELD_COUNT=0 for
// most declaration nodes — no named-field lookup via ChildByFieldName. We scan
// children by type instead, matching graphify's extract_swift.go approach.
func swiftNameNode(parent *sitter.Node, childType string) *sitter.Node {
	count := int(parent.ChildCount())
	for i := range count {
		child := parent.Child(i)
		if child != nil && child.Type() == childType {
			return child
		}
	}
	return nil
}

// swiftIsExtension reports whether a class_declaration node uses the "extension"
// keyword. In the Swift grammar, class, struct, enum, actor, and extension all
// parse as class_declaration; the distinguishing first keyword child differs.
// This mirrors kotlinIsInterface from handler_kotlin.go.
func swiftIsExtension(node *sitter.Node) bool {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child != nil && child.Type() == "extension" {
			return true
		}
	}
	return false
}

func (h *swiftHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := swiftNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *swiftHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := swiftNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *swiftHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := swiftNameNode(node, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *swiftHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := swiftNameNode(node, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *swiftHandler) mapVar(node *sitter.Node, source []byte) *Symbol {
	// property_declaration: (pattern (simple_identifier) ...) — drill via pattern child.
	patternNode := swiftNameNode(node, "pattern")
	if patternNode == nil {
		return nil
	}
	nameNode := swiftNameNode(patternNode, "simple_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindVar,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *swiftHandler) mapTypeAlias(node *sitter.Node, source []byte) *Symbol {
	nameNode := swiftNameNode(node, "type_identifier")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "swift",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
