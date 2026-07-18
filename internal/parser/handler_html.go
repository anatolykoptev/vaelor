package parser

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

// htmlHandler parses .html / .gohtml / .tmpl files that use Go html/template
// syntax ({{define}}, {{range}}, {{if}}, etc.).
//
// No tree-sitter grammar is used (see research dossier
// reports/go-code/2026-05-28-htmx-go-template-port-research.md §3c):
//   - tree-sitter-html is available via the smacker pin but adds ~150 KB CGO
//     binary weight with zero practical benefit for Go-template-bearing files
//     where {{...}} actions interrupt the HTML the grammar would parse.
//   - The byte-walker pattern from preproc/astro_refs.go is the proven
//     architecture for this class of mixed HTML+template files.
//
// Wave 1 extracts: {{define "X"}} template names as KindFunction symbols.
// Wave 2 will add: hx-* attribute extraction via preproc/htmx_attrs.go.
type htmlHandler struct {
	parserBase
}

// htmlLang is the singleton HTML language handler, registered on package init.
var htmlLang = &htmlHandler{}

func init() {
	htmlLang.parserBase = parserBase{
		lang: "html",
		// SitterLanguage: nil — symbol extraction is done by the byte-walker
		// in Parse(), not by tree-sitter. parserBase.Parse() with nil
		// SitterLanguage calls fallbackParse, but we override Parse() so that
		// path is never reached.
		caps: Capabilities{},
	}
	registerHandler(htmlLang)
}

// Extensions returns the file extensions this handler claims.
func (h *htmlHandler) Extensions() []string {
	return []string{".html", ".gohtml", ".tmpl"}
}

// Capabilities returns an empty Capabilities struct — no tree-sitter grammar.
func (h *htmlHandler) Capabilities() Capabilities { return h.caps }

// MapCapture is a no-op: htmlHandler does not use tree-sitter captures.
func (h *htmlHandler) MapCapture(_ string, _ *sitter.Node, _ []byte) *Symbol {
	return nil
}

// Parse strips Go template actions from src and extracts {{define "X"}} blocks
// as KindFunction symbols. The cleaned source is preserved for future htmx
// attribute extraction (Wave 2).
func (h *htmlHandler) Parse(path string, src []byte, _ ParseOpts) (*ParseResult, error) {
	_, defines := preproc.StripGoTemplate(src)

	res := &ParseResult{
		File:     path,
		Language: "html",
		Symbols:  make([]*Symbol, 0, len(defines)),
		Imports:  make([]string, 0),
	}

	for _, d := range defines {
		res.Symbols = append(res.Symbols, &Symbol{
			Name: d.Name,
			Kind: KindFunction, // KindTemplate does not exist; template names
			// behave as callable units — KindFunction is the established
			// fallback (matches Astro/Svelte symbol convention).
			Language:  "html",
			File:      path,
			StartLine: safeIntToUint32(d.StartLine),
			EndLine:   safeIntToUint32(d.EndLine),
			Signature: `{{define "` + d.Name + `"}}`,
		})
	}

	return res, nil
}
