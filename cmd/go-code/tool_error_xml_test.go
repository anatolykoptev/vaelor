package main

import (
	"errors"
	"testing"
)

// TestXMLMarshalErrorFragment_Escapes proves the shared marshal-failure fallback
// -- now the single authoritative <error> producer after #263 collapsed the
// error fragments and this increment routed the last four call sites
// (site_crawl, debug_investigate, site_analyze, repo_analyze_xml) through it --
// XML-escapes the error text, so a hostile message yields well-formed,
// round-tripping XML rather than the raw-%s malformed output the
// repo_analyze_xml fallback previously emitted.
func TestXMLMarshalErrorFragment_Escapes(t *testing.T) {
	frag := xmlMarshalErrorFragment(errors.New(`marshal: bad <tag> & "q"`))
	assertTextRoundTrips(t, frag, "error", `marshal: bad <tag> & "q"`)
}
