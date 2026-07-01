package main

import "testing"

// TestSemanticSearch_StructurallyEquivalentToBaseline proves the migrated
// (xml.Marshal) formatSemanticResults output is structurally identical to the
// recorded pre-migration output, including the two <result> shapes
// (with/without pagerank).
func TestSemanticSearch_StructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "inc3_semantic_benign.xml")
	migrated := formatSemanticResults(benignSemanticInput(), benignSemanticResults(), nil)
	assertXMLEquivalent(t, current, migrated)
}

// TestSemanticStatus_StructurallyEquivalentToBaseline proves buildStatusResponse
// is unchanged for a real status/message (the migration only adds escaping to
// the previously-raw <status>/<message> text, a no-op for server constants).
func TestSemanticStatus_StructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "inc3_semantic_status_benign.xml")
	migrated := buildStatusResponse(benignSemanticInput(), "indexing", benignSemanticStatusMessage)
	assertXMLEquivalent(t, current, migrated)
}

// TestSemanticSearch_HostileRoundTrips: the pre-migration formatter already
// escaped query/repo/symbol via escapeXML (its output was well-formed), so this
// is a STRUCTURAL migration, not a malformed-XML fix -- there is no malformed
// baseline to assert. This proves the migrated output round-trips the hostile
// query, repo and symbol verbatim (escaping now correct by construction).
func TestSemanticSearch_HostileRoundTrips(t *testing.T) {
	migrated := formatSemanticResults(hostileSemanticInput(), hostileSemanticResults(), nil)
	assertTextRoundTrips(t, migrated, "response/query", `find <T> where a & b == "x"`)
	assertTextRoundTrips(t, migrated, "response/repo", `a&b`)
	assertTextRoundTrips(t, migrated, "response/results/result/symbol", `New<T>`)
	assertAttrRoundTrips(t, migrated, "response/results/result/symbol", "kind", `func<>`)
}
