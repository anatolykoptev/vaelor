package main

import "encoding/xml"

// Error / no-match XML response fragments migrated from hand-rolled fmt.Sprintf
// onto encoding/xml.Marshal (failure class: manual XML string-concatenation).
//
// The prior fragments interpolated attribute/text values with escapeXML (so
// they were, unlike the %q attribute sites fixed in #262, already well-formed);
// the migration's payload here is STRUCTURAL — well-formedness becomes correct
// by construction, so a future edit that drops an escapeXML or reaches for %q
// cannot silently emit malformed XML. Empty elements serialize as long-form
// <x></x> (xml.Marshal never self-closes), decoder-equivalent to the prior
// self-closing <x/>. No xml.Header prolog is emitted — these are fragments the
// MCP caller consumes, matching the prior formatters.
//
// Each fragment embeds the pre-built <semantic_suggestions> child via ,innerxml:
// suggestions is already-marshaled XML (from formatSemanticSuggestions) and must
// be written verbatim, not re-escaped. It renders immediately after the leading
// element (field order) and is always non-empty here — every caller guards the
// XML branch on `suggestions != ""` and falls back to a plain-text result
// otherwise.

// ---- understand / impact_analysis / prepare_change / call_trace ----

// toolErrorXML is the shared "<tool> symbol-not-found + trigram suggestions"
// shape. All four tools emitted the identical
// <response tool="X"><error>msg</error>SUGGESTIONS</response>, so one helper
// replaces four inline fmt.Sprintf sites.
type toolErrorXML struct {
	XMLName     xml.Name `xml:"response"`
	Tool        string   `xml:"tool,attr"`
	Error       string   `xml:"error"`
	Suggestions string   `xml:",innerxml"`
}

// formatToolErrorWithSuggestions renders the shared symbol-not-found fragment.
// msg is RAW (unescaped) — xml.Marshal escapes the <error> chardata; passing an
// already-escaped msg would double-escape. suggestions is a pre-built XML
// fragment written verbatim after <error>.
func formatToolErrorWithSuggestions(tool, msg, suggestions string) string {
	b, err := xml.Marshal(toolErrorXML{Tool: tool, Error: msg, Suggestions: suggestions})
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return string(b)
}

// ---- code_search (no grep/semantic match) ----

// searchZeroXML is the zero-match <search pattern matches> element; matches is
// always 0 on this path.
type searchZeroXML struct {
	Pattern string `xml:"pattern,attr"`
	Matches int    `xml:"matches,attr"`
}

type codeSearchNoMatchXML struct {
	XMLName     xml.Name      `xml:"response"`
	Tool        string        `xml:"tool,attr"`
	Search      searchZeroXML `xml:"search"`
	Suggestions string        `xml:",innerxml"`
}

// formatCodeSearchNoMatch renders the code_search no-match fragment (pattern is
// RAW; xml.Marshal escapes the attribute).
func formatCodeSearchNoMatch(pattern, suggestions string) string {
	b, err := xml.Marshal(codeSearchNoMatchXML{
		Tool:        "code_search",
		Search:      searchZeroXML{Pattern: pattern, Matches: 0},
		Suggestions: suggestions,
	})
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return string(b)
}

// ---- symbol_search (no match) ----

// symbolsZeroXML is the zero-match <symbols query count> element; count is
// always 0 on this path.
type symbolsZeroXML struct {
	Query string `xml:"query,attr"`
	Count int    `xml:"count,attr"`
}

type symbolSearchNoMatchXML struct {
	XMLName     xml.Name       `xml:"response"`
	Tool        string         `xml:"tool,attr"`
	Symbols     symbolsZeroXML `xml:"symbols"`
	Suggestions string         `xml:",innerxml"`
}

// formatSymbolSearchNoMatch renders ONLY the <response>...</response> portion of
// the symbol_search no-match output. The caller appends "\n\n"+hint — a
// plain-text trailer OUTSIDE the XML document, preserved as-is.
func formatSymbolSearchNoMatch(query, suggestions string) string {
	b, err := xml.Marshal(symbolSearchNoMatchXML{
		Tool:        "symbol_search",
		Symbols:     symbolsZeroXML{Query: query, Count: 0},
		Suggestions: suggestions,
	})
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return string(b)
}
