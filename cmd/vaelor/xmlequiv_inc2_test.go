package main

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// Increment-2 XML formatter migration: structural-equivalence proofs (migrated
// output decodes to the same tree as the pre-migration output) plus hostile-input
// round-trip proofs (migrated attributes carrying <, &, " decode back verbatim).
//
// Unlike the %q attribute sites fixed in #262, these formatters already escaped
// via escapeXML, so their pre-migration output was well-formed — there is no
// malformed-baseline to assert against here. The migration's value is structural:
// well-formedness becomes correct by construction. The hostile tests therefore
// prove the migrated form still round-trips hostile values (not that a prior bug
// is fixed).

// benignSuggestions is a benign, well-formed <semantic_suggestions> fragment used
// as the ,innerxml payload for the error / no-match equivalence tests. Its exact
// content is opaque to the outer fragment: passed identically to both baseline
// and migrated, it must only decode cleanly.
const benignSuggestions = `<semantic_suggestions><hint>h</hint></semantic_suggestions>`

// ---- design_search ----

func TestDesignSearchXMLEquivalent(t *testing.T) {
	migrated := formatDesignResults(benignDesignArgs())
	assertXMLEquivalent(t, readGolden(t, "inc2_design_benign.xml"), migrated)
}

func TestDesignSearchXMLHostileAttrEscaped(t *testing.T) {
	brand := `Ben & Jerry's "Best" <ice>`
	migrated := formatDesignResults("q",
		[]brandHit{{brand: brand, section: "S", distance: 0.1, excerpt: "e"}},
		nil, nil)
	assertAttrRoundTrips(t, migrated, "response/results/result", "brand", brand)
}

// TestDesignStatusXMLEquivalent proves formatDesignStatus (increment 3; migrated
// from the inline hand-rolled not-indexed <response> string in tool_design_search.go)
// is structurally identical to that pre-migration constant, reproduced inline as
// a self-contained baseline.
func TestDesignStatusXMLEquivalent(t *testing.T) {
	const preMigration = "<response tool=\"design_search\"><status>not_indexed</status>" +
		"<message>No design embeddings. Run: go-code index-designs /path/to/dir/</message></response>"
	migrated := formatDesignStatus("not_indexed", "No design embeddings. Run: go-code index-designs /path/to/dir/")
	assertXMLEquivalent(t, preMigration, migrated)
}

// ---- semantic_suggestions ----

func TestSemanticSuggestionsXMLEquivalent(t *testing.T) {
	migrated := formatSemanticSuggestions(benignSemResults())
	assertXMLEquivalent(t, readGolden(t, "inc2_semantic_benign.xml"), migrated)
}

func TestSemanticSuggestionsXMLHostileAttrEscaped(t *testing.T) {
	kind := `weird"kind&<x>`
	migrated := formatSemanticSuggestions([]embeddings.SearchResult{
		{SymbolName: "A<B>&C", SymbolKind: kind, StartLine: 1, FilePath: "p&q.go", Distance: 0.1},
	})
	assertAttrRoundTrips(t, migrated, "semantic_suggestions/suggestion/symbol", "kind", kind)
}

// ---- understand / impact_analysis / prepare_change / call_trace (shared helper) ----

func TestToolErrorXMLEquivalent(t *testing.T) {
	msg := `symbol "renderCoturn" not found in repository`
	for _, tool := range []string{"understand", "impact_analysis", "prepare_change", "call_trace"} {
		// Verbatim pre-migration inline form (tool_understand.go:74 et al.):
		//   fmt.Sprintf("<response tool=\"X\"><error>%s</error>%s</response>", escapeXML(msg), suggestions)
		current := `<response tool="` + tool + `"><error>` + escapeXML(msg) + `</error>` + benignSuggestions + `</response>`
		migrated := formatToolErrorWithSuggestions(tool, msg, benignSuggestions)
		assertXMLEquivalent(t, current, migrated)

		root := parseXMLTree(t, "migrated", migrated)
		if got := root.attrs["tool"]; got != tool {
			t.Errorf("tool attr = %q, want %q", got, tool)
		}
	}
}

func TestToolErrorXMLHostileEscaped(t *testing.T) {
	msg := `symbol "Foo<T>" & <bar> not found`
	migrated := formatToolErrorWithSuggestions("understand", msg, benignSuggestions)
	root := parseXMLTree(t, "migrated", migrated) // must decode: proves well-formed

	// <error> chardata round-trips the hostile message verbatim.
	if got := childText(root, "error"); got != msg {
		t.Errorf("<error> text = %q, want %q", got, msg)
	}
	// the ,innerxml suggestions fragment survived as a sibling element.
	if childByName(root, "semantic_suggestions") == nil {
		t.Errorf("suggestions child missing; children = %s", childNames(root))
	}
}

// ---- code_search (no match) ----

func TestCodeSearchNoMatchXMLEquivalent(t *testing.T) {
	pattern := "renderCoturn"
	// Verbatim pre-migration inline form (tool_code_search.go:126), self-closing:
	current := `<response tool="code_search"><search pattern="` + escapeXML(pattern) + `" matches="0"/>` + benignSuggestions + `</response>`
	migrated := formatCodeSearchNoMatch(pattern, benignSuggestions)
	assertXMLEquivalent(t, current, migrated)
}

func TestCodeSearchNoMatchXMLHostileAttrEscaped(t *testing.T) {
	pattern := `a && b || c > "d" <e>`
	migrated := formatCodeSearchNoMatch(pattern, benignSuggestions)
	assertAttrRoundTrips(t, migrated, "response/search", "pattern", pattern)
}

// ---- symbol_search (no match; <response> portion only) ----

func TestSymbolSearchNoMatchXMLEquivalent(t *testing.T) {
	query := "renderCoturn"
	// Verbatim pre-migration inline form (tool_symbol_search.go:136), the caller
	// appends "\n\n"+hint OUTSIDE this <response>.
	current := `<response tool="symbol_search"><symbols query="` + escapeXML(query) + `" count="0"/>` + benignSuggestions + `</response>`
	migrated := formatSymbolSearchNoMatch(query, benignSuggestions)
	assertXMLEquivalent(t, current, migrated)
}

func TestSymbolSearchNoMatchXMLHostileAttrEscaped(t *testing.T) {
	query := `x"y&z<w>`
	migrated := formatSymbolSearchNoMatch(query, benignSuggestions)
	assertAttrRoundTrips(t, migrated, "response/symbols", "query", query)
}

// childText returns the concatenated text of the first direct child named name.
func childText(n *xmlTreeNode, name string) string {
	if c := childByName(n, name); c != nil {
		return c.text
	}
	return ""
}

// childByName returns the first direct child of n with the given local name.
func childByName(n *xmlTreeNode, name string) *xmlTreeNode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}
