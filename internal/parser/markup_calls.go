package parser

import (
	"context"
	_ "embed"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

//go:embed queries/markup_refs.scm
var markupRefsQueryBytes []byte

// markupRefsQuery is the bare-top-level-identifier query used ONLY by the markup
// {expr} reparse path. It is compiled lazily against the TSX grammar at call
// time (not in init) to mirror the lazy-singleton discipline the astro/svelte/
// vue handlers use: tsxLang.caps.SitterLanguage is wired in handler_tsx.go's
// init(), which may run after this file's init() (init-order MapCapture landmine,
// documented in the parity plan).
var (
	markupRefsQueryOnce sync.Once
	markupRefsQuery     *sitter.Query
)

func getMarkupRefsQuery() *sitter.Query {
	markupRefsQueryOnce.Do(func() {
		lang := tsxLang.Capabilities().SitterLanguage
		markupRefsQuery = mustCompileQuery(markupRefsQueryBytes, lang, "markup_refs.scm")
	})
	return markupRefsQuery
}

// markupExprReparse extracts the function/method/argref call sites embedded in
// the template-body {expr} ranges of a preprocessor-language file (Astro today;
// Svelte in a later phase). It batches every {expr} range into ONE virtual
// source (preproc.ExtractMarkupExprs) and reparses it with the TSX grammar
// (tsxLang) rather than plain TypeScript: Astro template expressions legally
// embed JSX (e.g. {list.map(i => <Card/>)}), which a plain-TS reparse would
// reject as ERROR nodes, dropping the calls. Under the TSX grammar tsx_calls.scm
// fires for free (calls / member-calls / argrefs); markup_refs.scm additionally
// captures bare top-level identifiers ({count}) for React parity.
//
// Call-site line numbers are remapped from virtual to original-file coordinates
// via the shared virtualToOriginal helper; padding lines are dropped. This
// mirrors the collectRuneSymbols / appendRuneSymbols post-parse-classifier
// precedent (operate on the original src via a VirtualSource, remap afterwards).
func markupExprReparse(path string, src []byte, opts ParseOpts) []CallSite {
	vs := preproc.ExtractMarkupExprs(src)
	if vs == nil || len(vs.Code) == 0 {
		return nil
	}
	lang := tsxLang.Capabilities().SitterLanguage
	if lang == nil {
		return nil
	}

	ps := sitter.NewParser()
	defer ps.Close()
	ps.SetLanguage(lang)

	tree, err := ps.ParseCtx(context.Background(), nil, vs.Code)
	if err != nil {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()

	// tsx_calls.scm: calls, member-calls, argrefs (incl. JSX-expression argrefs).
	// markup_refs.scm: bare top-level identifiers ({count}) for React parity.
	calls := runCallQuery(tsxLang.Capabilities().CallsQuery, root, vs.Code, path)
	calls = append(calls, runCallQuery(getMarkupRefsQuery(), root, vs.Code, path)...)

	// Remap virtual line numbers to original coordinates, dropping padding.
	remapped := calls[:0]
	for _, c := range calls {
		orig := virtualToOriginal(vs.LineMap, c.Line)
		if orig == 0 {
			continue
		}
		c.Line = orig
		remapped = append(remapped, c)
	}
	return remapped
}

// MarkupCalls satisfies markupCallSource (see calls.go): the Astro handler's
// template body carries {expr} call sites that parsing the raw .astro file with
// the delegated plain-TS grammar cannot reach. ExtractCalls appends these to the
// ordinary call sites.
func (h *astroHandler) MarkupCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return markupExprReparse(path, src, opts)
}
