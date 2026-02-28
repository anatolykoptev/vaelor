package parser

import (
	_ "embed"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

//go:embed queries/go.scm
var goQueryBytes []byte

//go:embed queries/go_calls.scm
var goCallsQueryBytes []byte

// goHandler implements LanguageHandler for Go source files.
type goHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
}

// goLang is the singleton Go language handler, registered on package init.
var goLang = &goHandler{}

func init() {
	lang := golang.GetLanguage()
	q, err := sitter.NewQuery(goQueryBytes, lang)
	if err != nil {
		panic("go.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(goCallsQueryBytes, lang)
	if err != nil {
		panic("go_calls.scm query compile error: " + err.Error())
	}
	goLang.lang = lang
	goLang.query = q
	goLang.callQuery = cq
	registerHandler(goLang)
}

func (h *goHandler) Language() string { return "go" }

func (h *goHandler) Extensions() []string { return []string{".go"} }

func (h *goHandler) SitterLanguage() *sitter.Language { return h.lang }

func (h *goHandler) TagsQuery() *sitter.Query { return h.query }

func (h *goHandler) CallsQuery() *sitter.Query { return h.callQuery }

// MapCapture converts a tree-sitter capture to a Symbol.
// Returns nil for captures that are not top-level declarations.
func (h *goHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureType:
		return h.mapType(node, source)
	case captureConst:
		return h.mapConst(node, source)
	case captureVar:
		return h.mapVar(node, source)
	}
	return nil
}

func (h *goHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindFunction,
		Language:  "go",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *goHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "go",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *goHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	// type_declaration → type_spec → name + type body
	typeSpec := firstChildOfType(node, "type_spec")
	if typeSpec == nil {
		return nil
	}
	nameNode := typeSpec.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	kind := detectTypeKind(typeSpec)
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      kind,
		Language:  "go",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *goHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	// Skip function-local const declarations.
	if parent := node.Parent(); parent != nil && parent.Type() != "source_file" {
		return nil
	}
	spec := firstChildOfType(node, "const_spec")
	if spec == nil {
		return nil
	}
	nameNode := spec.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindConst,
		Language:  "go",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(spec, source), // spec, not node — avoids "const (" for blocks
	}
}

func (h *goHandler) mapVar(node *sitter.Node, source []byte) *Symbol {
	// Skip function-local var declarations.
	if parent := node.Parent(); parent != nil && parent.Type() != "source_file" {
		return nil
	}
	spec := firstChildOfType(node, "var_spec")
	if spec == nil {
		return nil
	}
	nameNode := spec.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindVar,
		Language:  "go",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(spec, source), // spec, not node
	}
}

// extractSignature returns the first line of the declaration, up to (not including) the opening brace.
func extractSignature(node *sitter.Node, source []byte) string {
	text := node.Content(source)
	// Take only the first line for brevity (signature header).
	firstLine := text
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		firstLine = text[:idx]
	}
	// Strip trailing brace + whitespace if present on the same line.
	if idx := strings.IndexByte(firstLine, '{'); idx >= 0 {
		firstLine = strings.TrimRight(firstLine[:idx], " \t")
	}
	return strings.TrimSpace(firstLine)
}

// firstChildOfType returns the first direct child node with the given type.
func firstChildOfType(node *sitter.Node, nodeType string) *sitter.Node {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

// detectTypeKind inspects a type_spec node to return KindStruct or KindInterface
// (falling back to KindType for aliases, etc.).
func detectTypeKind(typeSpec *sitter.Node) NodeKind {
	typeBody := typeSpec.ChildByFieldName("type")
	if typeBody == nil {
		return KindType
	}
	switch typeBody.Type() {
	case "struct_type":
		return KindStruct
	case "interface_type":
		return KindInterface
	default:
		return KindType
	}
}

