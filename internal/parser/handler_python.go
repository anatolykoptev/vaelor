package parser

import (
	_ "embed"
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

//go:embed queries/python.scm
var pythonQueryBytes []byte

//go:embed queries/python_calls.scm
var pythonCallsQueryBytes []byte

//go:embed queries/python_rels.scm
var pythonRelsQueryBytes []byte

// pythonHandler implements LanguageHandler for Python source files.
type pythonHandler struct {
	parserBase
}

// pyLang is the singleton Python language handler, registered on package init.
var pyLang = &pythonHandler{}

func init() {
	lang := python.GetLanguage()
	pyLang.parserBase = parserBase{
		lang: "python",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(pythonQueryBytes, lang, "python.scm"),
			CallsQuery:         mustCompileQuery(pythonCallsQueryBytes, lang, "python_calls.scm"),
			RelationshipsQuery: mustCompileQuery(pythonRelsQueryBytes, lang, "python_rels.scm"),
			MapCapture:         pyLang.MapCapture,
		},
	}
	registerHandler(pyLang)
}

func (h *pythonHandler) Extensions() []string { return []string{".py"} }

// MapCapture converts a tree-sitter capture to a Symbol for Python.
func (h *pythonHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	case captureVar:
		return h.mapVariable(node, source)
	}
	return nil
}

func (h *pythonHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(source)
	return &Symbol{
		Name:       name,
		Kind:       KindFunction,
		Language:   "python",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isPythonPublic(name),
		Attributes: extractPythonDecorators(node, source),
	}
}

func (h *pythonHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(source)
	return &Symbol{
		Name:       name,
		Kind:       KindClass,
		Language:   "python",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isPythonPublic(name),
		Attributes: extractPythonDecorators(node, source),
	}
}

func (h *pythonHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(source)
	return &Symbol{
		Name:       name,
		Kind:       KindMethod,
		Language:   "python",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   isPythonPublic(name),
		Attributes: extractPythonDecorators(node, source),
	}
}

func (h *pythonHandler) mapVariable(node *sitter.Node, source []byte) *Symbol {
	// node is the assignment; left child is the identifier captured as symbol.name.
	nameNode := node.ChildByFieldName("left")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(source)
	kind := KindVar
	if isAllCaps(name) {
		kind = KindConst
	}
	return &Symbol{
		Name:      name,
		Kind:      kind,
		Language:  "python",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
		IsPublic:  isPythonPublic(name),
	}
}

// extractPythonDecorators extracts decorator names from a decorated_definition parent node.
func extractPythonDecorators(node *sitter.Node, source []byte) []string {
	parent := node.Parent()
	if parent == nil || parent.Type() != "decorated_definition" {
		return nil
	}
	var attrs []string
	count := int(parent.ChildCount())
	for i := range count {
		child := parent.Child(i)
		if child != nil && child.Type() == "decorator" {
			text := strings.TrimSpace(child.Content(source))
			attrs = append(attrs, text)
		}
	}
	return attrs
}

// isPythonPublic returns true if a Python name is public by convention.
// Names starting with underscore are private; all others are public.
func isPythonPublic(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return r != '_' || unicode.IsUpper(r)
}

// isAllCaps returns true if name consists entirely of uppercase letters and underscores.
func isAllCaps(name string) bool {
	if name == "" {
		return false
	}
	hasLetter := false
	for _, r := range name {
		if unicode.IsUpper(r) {
			hasLetter = true
		} else if r != '_' && !unicode.IsDigit(r) {
			return false
		}
	}
	return hasLetter
}
