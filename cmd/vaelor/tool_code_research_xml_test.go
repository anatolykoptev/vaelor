package main

import "testing"

// TestCodeResearch_StructurallyEquivalentToBaseline proves the migrated
// formatResearchResult output (grouped seeds, graph with a skipped no-symbol
// file, and the code map) is structurally identical to the recorded
// pre-migration output for a benign fixture.
func TestCodeResearch_StructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "inc3_research_benign.xml")
	migrated := formatResearchResult(benignResearchInput(), benignResearchRoot, benignResearchResult())
	assertXMLEquivalent(t, current, migrated)
}

// TestCodeResearch_BaselineHostileIsMalformed documents the LIVE bug: the
// pre-migration formatter emitted the <map> body with a raw %s (a code map full
// of <-chan / & / generics) and the <seeds> path/kind attributes with %q, so its
// output is malformed XML for essentially every real repository.
func TestCodeResearch_BaselineHostileIsMalformed(t *testing.T) {
	assertNotWellFormed(t, readGolden(t, "inc3_research_hostile_current.xml"))
}

// TestCodeResearch_HostileEscaped proves the fix: the migrated output is
// well-formed and the <map> text plus the seed path/kind/name round-trip to
// their exact hostile values.
func TestCodeResearch_HostileEscaped(t *testing.T) {
	migrated := formatResearchResult(benignResearchInput(), benignResearchRoot, hostileResearchResult())
	assertTextRoundTrips(t, migrated, "response/map", "func Send() <-chan T { return ch & mask }")
	assertAttrRoundTrips(t, migrated, "response/seeds/file", "path", "internal/a&b.go")
	assertAttrRoundTrips(t, migrated, "response/seeds/file/symbol", "kind", "func<>")
	assertTextRoundTrips(t, migrated, "response/seeds/file/symbol", "Send<T>")
}
