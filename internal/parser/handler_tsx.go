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

// Parse overrides parserBase.Parse to correct each emitted Symbol.Language.
// The shared MapCapture (tsLang.MapCapture, handler_typescript.go) hardcodes
// Language:"typescript" on every symbol — correct for .tsx, wrong for .jsx
// (DetectLanguageFromPath maps .jsx -> "javascript", matching GitHub Linguist).
// applyDetectedSymbolLanguage fixes it override-first WITHOUT mutating the
// shared parserBase.lang field or tsLang.MapCapture's literals — the
// boundaries-HIGH trap (plan ADR 5): a global flip there would mislabel every
// .tsx and every plain .ts too. This override only touches symbols flowing
// through THIS handler's Parse, keeping .tsx byte-identical ("typescript").
func (h *tsxHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	result, err := h.parserBase.Parse(path, src, opts)
	if err != nil {
		return nil, err
	}
	applyDetectedSymbolLanguage(result, path, opts)
	return result, nil
}
