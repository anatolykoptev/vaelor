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

// Parse overrides parserBase.Parse to correct each emitted Symbol.Language
// per the file's actual extension. MapCapture is shared with tsLang
// (handler_typescript.go) and hardcodes Language: "typescript" on every
// symbol it builds — correct for .tsx, wrong for .jsx (DetectLanguageFromPath
// already maps .jsx -> "javascript", matching GitHub Linguist; parser_lang.go:45).
//
// Deriving the label from DetectLanguageFromPath here — instead of mutating
// the shared parserBase.lang field or tsLang.MapCapture's literals — is the
// boundaries-HIGH trap this fix must avoid (plan ADR 5,
// plans/go-code/2026-06-30-frontend-parse-parity-react-svelte-astro.md):
// tsxHandler serves BOTH .tsx and .jsx through ONE shared handler, and
// tsLang.MapCapture is also called directly by typescriptHandler for
// .ts/.js/.mjs/.cjs, so a global label flip there would mislabel every
// .tsx (and every plain .ts) file too. This override only touches symbols
// that flow through THIS handler's Parse, keeping .tsx's "typescript"
// label byte-identical while fixing .jsx to agree with its own detector.
func (h *tsxHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	result, err := h.parserBase.Parse(path, src, opts)
	if err != nil {
		return nil, err
	}
	lang := DetectLanguageFromPath(path)
	for _, sym := range result.Symbols {
		sym.Language = lang
	}
	return result, nil
}
