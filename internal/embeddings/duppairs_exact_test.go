package embeddings

import "testing"

func TestReceiverPairKey_Canonical(t *testing.T) {
	if got, want := receiverPairKey("B", "A"), "A|B"; got != want {
		t.Errorf("receiverPairKey(B,A) = %q, want %q", got, want)
	}
	if got, want := receiverPairKey("A", "B"), "A|B"; got != want {
		t.Errorf("receiverPairKey(A,B) = %q, want %q", got, want)
	}
	if receiverPairKey("A", "B") != receiverPairKey("B", "A") {
		t.Error("reversed args must produce the same key")
	}
}

func TestDistinctReceivers(t *testing.T) {
	eps := []methodEndpoint{
		{name: "M", file: "a.go", receiver: "A"},
		{name: "M", file: "b.go", receiver: "B"},
		{name: "N", file: "a2.go", receiver: "A"}, // duplicate receiver A
	}
	got := distinctReceivers(eps)
	if len(got) != 2 {
		t.Fatalf("got %d distinct receivers, want 2: %v", len(got), got)
	}
	set := map[string]bool{got[0]: true, got[1]: true}
	if !set["A"] || !set["B"] {
		t.Errorf("expected {A,B}, got %v", got)
	}
}

// TestMatchSiblingPairs_FetchREADMESuppressed: GitHubForge and GitLabForge both
// IMPLEMENTS Forge (a real shared interface), so the same-name FetchREADME method
// pair is correctly suppressed via the edge — the exact-path equivalent of the
// #218 FetchREADME case, now driven by a real IMPLEMENTS 2-hop, not the heuristic.
func TestMatchSiblingPairs_FetchREADMESuppressed(t *testing.T) {
	endpoints := []methodEndpoint{
		{name: "FetchREADME", file: "internal/forge/github.go", receiver: "GitHubForge"},
		{name: "FetchREADME", file: "internal/forge/gitlab.go", receiver: "GitLabForge"},
	}
	connected := map[string]bool{
		receiverPairKey("GitHubForge", "GitLabForge"): true, // both IMPLEMENTS Forge
	}
	pk := NewPairKey("internal/forge/github.go", "FetchREADME", "internal/forge/gitlab.go", "FetchREADME")
	inputSet := map[PairKey]bool{pk: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if !siblings[pk] {
		t.Error("FetchREADME pair should be suppressed: receivers share interface Forge via real IMPLEMENTS edges")
	}
}

// TestMatchSiblingPairs_RemoveFromOrderReported: callGraphCache and couplingCache
// share NO interface, so the connected set is empty and the same-name
// removeFromOrder pair is REPORTED (a real cross-package duplicate).
func TestMatchSiblingPairs_RemoveFromOrderReported(t *testing.T) {
	endpoints := []methodEndpoint{
		{name: "removeFromOrder", file: "internal/callgraph/repo_cache.go", receiver: "callGraphCache"},
		{name: "removeFromOrder", file: "internal/compare/coupling_cache.go", receiver: "couplingCache"},
	}
	connected := map[string]bool{} // no shared interface → empty 2-hop result
	pk := NewPairKey("internal/callgraph/repo_cache.go", "removeFromOrder", "internal/compare/coupling_cache.go", "removeFromOrder")
	inputSet := map[PairKey]bool{pk: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if siblings[pk] {
		t.Error("removeFromOrder pair must be reported (not suppressed): receivers share no interface")
	}
}

// TestMatchSiblingPairs_ResidualExportedNoInterfaceReported closes the residual
// false-negative the #218 heuristic could not: two EXPORTED same-name methods on
// distinct receivers with identical signatures that share NO interface. The
// heuristic suppressed these (exported + distinct receiver + same sig); the exact
// path correctly REPORTS them because no common interface edge connects the
// receivers.
func TestMatchSiblingPairs_ResidualExportedNoInterfaceReported(t *testing.T) {
	endpoints := []methodEndpoint{
		{name: "Validate", file: "internal/orders/cart.go", receiver: "Cart"},
		{name: "Validate", file: "internal/billing/invoice.go", receiver: "Invoice"},
	}
	connected := map[string]bool{} // Cart and Invoice share no interface
	pk := NewPairKey("internal/orders/cart.go", "Validate", "internal/billing/invoice.go", "Validate")
	inputSet := map[PairKey]bool{pk: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if siblings[pk] {
		t.Error("RESIDUAL: exported same-name methods on receivers sharing no interface must be REPORTED, not suppressed")
	}
}

// TestMatchSiblingPairs_SameReceiverNotSibling: two methods on the same receiver
// type are not a cross-type sibling pair.
func TestMatchSiblingPairs_SameReceiverNotSibling(t *testing.T) {
	endpoints := []methodEndpoint{
		{name: "M", file: "a.go", receiver: "T"},
		{name: "M", file: "a.go", receiver: "T"},
	}
	connected := map[string]bool{receiverPairKey("T", "T"): true}
	pk := NewPairKey("a.go", "M", "a.go", "M")
	inputSet := map[PairKey]bool{pk: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if siblings[pk] {
		t.Error("same receiver type must not be a cross-type sibling")
	}
}

// TestMatchSiblingPairs_DifferentNameNotSibling: methods with different names are
// never paired even when their receivers share an interface.
func TestMatchSiblingPairs_DifferentNameNotSibling(t *testing.T) {
	endpoints := []methodEndpoint{
		{name: "Foo", file: "a.go", receiver: "A"},
		{name: "Bar", file: "b.go", receiver: "B"},
	}
	connected := map[string]bool{receiverPairKey("A", "B"): true}
	pkFoo := NewPairKey("a.go", "Foo", "b.go", "Bar")
	inputSet := map[PairKey]bool{pkFoo: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if len(siblings) != 0 {
		t.Errorf("different method names must not be paired; got %v", siblings)
	}
}
