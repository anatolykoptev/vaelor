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
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
	relsQuery *sitter.Query
}

var tsxLang = &tsxHandler{}

func init() {
	lang := tsx.GetLanguage()
	q, err := sitter.NewQuery(typescriptQueryBytes, lang)
	if err != nil {
		panic("typescript.scm (tsx) query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(tsxCallsQueryBytes, lang)
	if err != nil {
		panic("tsx_calls.scm query compile error: " + err.Error())
	}
	rq, err := sitter.NewQuery(tsRelsQueryBytes, lang)
	if err != nil {
		panic("typescript_rels.scm (tsx) query compile error: " + err.Error())
	}
	tsxLang.lang = lang
	tsxLang.query = q
	tsxLang.callQuery = cq
	tsxLang.relsQuery = rq
	registerHandler(tsxLang)
}

func (h *tsxHandler) Language() string                 { return "typescript" }
func (h *tsxHandler) Extensions() []string             { return []string{".tsx", ".jsx"} }
func (h *tsxHandler) SitterLanguage() *sitter.Language { return h.lang }
func (h *tsxHandler) TagsQuery() *sitter.Query         { return h.query }
func (h *tsxHandler) CallsQuery() *sitter.Query        { return h.callQuery }
func (h *tsxHandler) RelationshipsQuery() *sitter.Query { return h.relsQuery }

// MapCapture delegates to the shared TypeScript capture mapper.
// TSX shares all symbol types with TypeScript (function, class, method, etc.)
func (h *tsxHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	return tsLang.MapCapture(captureName, node, source)
}
