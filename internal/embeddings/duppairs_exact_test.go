package embeddings

import (
	"context"
	"strings"
	"testing"
)

// mkEndpoint builds a methodEndpoint with its package-qualified receiverID derived
// from the file, mirroring resolveMethodEndpoints so tests exercise the same
// identity the production path produces.
func mkEndpoint(name, file, receiver string) methodEndpoint {
	return methodEndpoint{
		name:       name,
		file:       file,
		receiver:   receiver,
		receiverID: receiverID(file, receiver),
	}
}

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

func TestReceiverID_PackageQualified(t *testing.T) {
	a := receiverID("internal/parser/cache.go", "Cache")
	b := receiverID("internal/cache/cache.go", "Cache")
	if a == b {
		t.Fatalf("same-named types in different packages must get distinct ids: %q == %q", a, b)
	}
	if a != receiverID("internal/parser/other.go", "Cache") {
		t.Error("same package + same type name must produce the same id regardless of file within the dir")
	}
}

func TestDistinctReceiverNames(t *testing.T) {
	eps := []methodEndpoint{
		mkEndpoint("M", "a.go", "A"),
		mkEndpoint("M", "b.go", "B"),
		mkEndpoint("N", "a2.go", "A"), // duplicate receiver name A
	}
	got := distinctReceiverNames(eps)
	if len(got) != 2 {
		t.Fatalf("got %d distinct receiver names, want 2: %v", len(got), got)
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
	a := mkEndpoint("FetchREADME", "internal/forge/github.go", "GitHubForge")
	b := mkEndpoint("FetchREADME", "internal/forge/gitlab.go", "GitLabForge")
	endpoints := []methodEndpoint{a, b}
	connected := map[string]bool{
		receiverPairKey(a.receiverID, b.receiverID): true, // both IMPLEMENTS Forge
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
		mkEndpoint("removeFromOrder", "internal/callgraph/repo_cache.go", "callGraphCache"),
		mkEndpoint("removeFromOrder", "internal/compare/coupling_cache.go", "couplingCache"),
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
		mkEndpoint("Validate", "internal/orders/cart.go", "Cart"),
		mkEndpoint("Validate", "internal/billing/invoice.go", "Invoice"),
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
// type (same package + same name → same receiverID) are not a cross-type sibling.
func TestMatchSiblingPairs_SameReceiverNotSibling(t *testing.T) {
	a := mkEndpoint("M", "a.go", "T")
	endpoints := []methodEndpoint{a, a}
	connected := map[string]bool{receiverPairKey(a.receiverID, a.receiverID): true}
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
	a := mkEndpoint("Foo", "a.go", "A")
	b := mkEndpoint("Bar", "b.go", "B")
	endpoints := []methodEndpoint{a, b}
	connected := map[string]bool{receiverPairKey(a.receiverID, b.receiverID): true}
	pkFoo := NewPairKey("a.go", "Foo", "b.go", "Bar")
	inputSet := map[PairKey]bool{pkFoo: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if len(siblings) != 0 {
		t.Errorf("different method names must not be paired; got %v", siblings)
	}
}

// TestMatchSiblingPairs_SameNameDifferentPackageNotCrossConnected is the MAJOR-1
// regression guard. Two DISTINCT same-named receiver types live in different
// packages: parser.Cache and cache.Cache. parser.Cache shares an interface with
// SOME OTHER type, so a connected-set entry for the parser.Cache pair exists — but
// the candidate pair under test is a real cross-package duplicate of a method on
// parser.Cache vs cache.Cache that share NO interface. With bare-name keying, the
// unrelated parser.Cache interface membership would wrongly suppress this pair
// (over-suppression). With package-qualified keying, the pair is REPORTED.
func TestMatchSiblingPairs_SameNameDifferentPackageNotCrossConnected(t *testing.T) {
	parserCache := mkEndpoint("Get", "internal/parser/cache.go", "Cache")
	cacheCache := mkEndpoint("Get", "internal/cache/cache.go", "Cache")
	// A third, unrelated type that DOES share an interface with parser.Cache.
	someStore := mkEndpoint("Get", "internal/store/store.go", "Store")

	endpoints := []methodEndpoint{parserCache, cacheCache, someStore}

	// Only parser.Cache <-> store.Store share an interface. The bare name "Cache"
	// appears on both parser.Cache and cache.Cache, but cache.Cache shares nothing.
	connected := map[string]bool{
		receiverPairKey(parserCache.receiverID, someStore.receiverID): true,
	}

	dupPK := NewPairKey("internal/parser/cache.go", "Get", "internal/cache/cache.go", "Get")
	inputSet := map[PairKey]bool{dupPK: true}

	siblings := matchSiblingPairs(endpoints, connected, inputSet)
	if siblings[dupPK] {
		t.Error("MAJOR-1: a real cross-package duplicate on two same-named types (parser.Cache vs cache.Cache) " +
			"that share NO interface must be REPORTED, not suppressed by an unrelated same-named type's interface membership")
	}
}

// TestReceiverPairsSharingInterface_SameNameDifferentPackageReturnsDistinctIds is
// the MAJOR-1 guard at the 2-hop layer: the Cypher result parser must key on the
// package-qualified id (name+file), not the bare name, so two same-named receiver
// types in different packages that share an interface with a THIRD type produce
// distinct connected-set keys (not a single bare-name collision).
func TestReceiverPairsSharingInterface_SameNameDifferentPackageReturnsDistinctIds(t *testing.T) {
	// Fake 2-hop result: store.Store implements an interface that BOTH
	// parser.Cache and cache.Cache also implement. The query returns each type
	// with its file, so the parser.Cache<->Store and cache.Cache<->Store pairs
	// must be two distinct connected-set keys.
	e := &Expander{execCypherFn: func(_ context.Context, _, cypher, _ string) [][]string {
		if !strings.Contains(cypher, "IMPLEMENTS") {
			t.Fatalf("unexpected cypher: %q", cypher)
		}
		return [][]string{
			{`"Cache"`, `"internal/parser/cache.go"`, `"Store"`, `"internal/store/store.go"`},
			{`"Cache"`, `"internal/cache/cache.go"`, `"Store"`, `"internal/store/store.go"`},
		}
	}}

	connected := e.receiverPairsSharingInterface(context.Background(), "g", []string{"Cache", "Store"})

	parserCacheID := receiverID("internal/parser/cache.go", "Cache")
	cacheCacheID := receiverID("internal/cache/cache.go", "Cache")
	storeID := receiverID("internal/store/store.go", "Store")

	if !connected[receiverPairKey(parserCacheID, storeID)] {
		t.Error("parser.Cache <-> Store must be connected")
	}
	if !connected[receiverPairKey(cacheCacheID, storeID)] {
		t.Error("cache.Cache <-> Store must be connected")
	}
	if len(connected) != 2 {
		t.Errorf("expected 2 distinct package-qualified connections, got %d: %v", len(connected), connected)
	}
}
