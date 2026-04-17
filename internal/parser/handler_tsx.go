package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
)

//go:embed queries/tsx_calls.scm
var tsxCallsQueryBytes []byte

// tsxHandler handles .tsx and .jsx files using the TSX grammar (TypeScript + JSX).
// Reuses the TypeScript tags/rels queries (all TS node types exist in TSX grammar)
// but uses a separate calls query with JSX-specific patterns.
type tsxHandler struct {
	parserBase
}

var tsxLang = &tsxHandler{}

func init() {
	lang := tsx.GetLanguage()
	tsxLang.parserBase = parserBase{
		lang: "typescript",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(typescriptQueryBytes, lang, "typescript.scm (tsx)"),
			CallsQuery:         mustCompileQuery(tsxCallsQueryBytes, lang, "tsx_calls.scm"),
			RelationshipsQuery: mustCompileQuery(tsRelsQueryBytes, lang, "typescript_rels.scm (tsx)"),
			MapCapture:         tsxLang.MapCapture,
		},
	}
	registerHandler(tsxLang)
}

func (h *tsxHandler) Extensions() []string { return []string{".tsx", ".jsx"} }

// MapCapture delegates to the shared TypeScript capture mapper.
// TSX shares all symbol types with TypeScript (function, class, method, etc.)
func (h *tsxHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}
