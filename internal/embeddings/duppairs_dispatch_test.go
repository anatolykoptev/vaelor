package embeddings

import (
	"context"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// fakeCypher is a deterministic execCypherN replacement for the dispatcher
// state-machine tests. It routes by query shape — the IMPLEMENTS count-probe, the
// method-resolution query, the 2-hop interface query, and the heuristic 8-column
// same-name-method query are distinguished by substrings unique to each — and
// returns canned agtype-quoted rows. No live AGE DB is touched.
type fakeCypher struct {
	implementsCount string     // raw agtype rendering of count(r) for the presence-probe
	methodRows      [][]string // rows for resolveMethodEndpoints (3 cols)
	twoHopRows      [][]string // rows for receiverPairsSharingInterface (4 cols)
	heuristicRows   [][]string // rows for pairsSharingInterfaceHeuristic (8 cols)
	seen            []string   // cypher queries observed, for assertions
}

func (f *fakeCypher) exec(_ context.Context, _, cypher, _ string) [][]string {
	f.seen = append(f.seen, cypher)
	switch {
	case strings.Contains(cypher, "[r:IMPLEMENTS]") && strings.Contains(cypher, "count(r)"):
		if f.implementsCount == "" {
			return nil
		}
		return [][]string{{f.implementsCount}}
	case strings.Contains(cypher, "a.kind = 'method'") && strings.Contains(cypher, "a.signature") &&
		!strings.Contains(cypher, "b.kind"):
		return f.methodRows
	case strings.Contains(cypher, "[:IMPLEMENTS]->(i:Symbol)<-[:IMPLEMENTS]-"):
		return f.twoHopRows
	case strings.Contains(cypher, "a.kind = 'method'") && strings.Contains(cypher, "b.kind = 'method'"):
		return f.heuristicRows
	default:
		return nil
	}
}

func newFakeExpander(f *fakeCypher) *Expander {
	return &Expander{execCypherFn: f.exec}
}

// counter reads the current value of the path-labeled discriminator counter via the
// collector Write API (testutil is not vendored). The counter is process-global, so
// dispatcher tests assert a +1 DELTA rather than an absolute value.
func counter(t *testing.T, path string) float64 {
	t.Helper()
	var m dto.Metric
	if err := interfaceSiblingPathTotal.WithLabelValues(path).Write(&m); err != nil {
		t.Fatalf("read counter %q: %v", path, err)
	}
	return m.GetCounter().GetValue()
}

// TestPairsSharingInterface_EdgesAbsent_HeuristicPath asserts requirement (1): when
// the IMPLEMENTS presence-probe returns zero, the dispatcher takes the heuristic
// fallback, increments {path="heuristic"}, and returns the #218 heuristic result.
// The fake heuristic rows mirror the FetchREADME interface-sibling case the #218
// discriminator suppresses (two exported same-name methods, identical
// receiver-stripped signature, distinct receivers).
func TestPairsSharingInterface_EdgesAbsent_HeuristicPath(t *testing.T) {
	sig := "func (g *%s) FetchREADME(ctx context.Context, slug string) (string, error)"
	f := &fakeCypher{
		implementsCount: `0`, // zero IMPLEMENTS edges → heuristic fallback
		heuristicRows: [][]string{{
			`"FetchREADME"`, `"internal/forge/github.go"`, `"method"`, `"` + strings.Replace(sig, "%s", "GitHubForge", 1) + `"`,
			`"FetchREADME"`, `"internal/forge/gitlab.go"`, `"method"`, `"` + strings.Replace(sig, "%s", "GitLabForge", 1) + `"`,
		}},
	}
	e := newFakeExpander(f)

	pk := NewPairKey("internal/forge/github.go", "FetchREADME", "internal/forge/gitlab.go", "FetchREADME")
	pairs := []PairKey{pk}

	before := counter(t, ifacePathHeuristic)
	got, err := e.PairsSharingInterface(context.Background(), "g", pairs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := counter(t, ifacePathHeuristic); after != before+1 {
		t.Errorf("heuristic path counter: got %v, want %v", after, before+1)
	}
	if !got[pk] {
		t.Error("heuristic path must suppress the FetchREADME interface-sibling pair (matches #218 result)")
	}
	// The exact path's 2-hop query must NOT have fired.
	for _, q := range f.seen {
		if strings.Contains(q, "[:IMPLEMENTS]->(i:Symbol)<-[:IMPLEMENTS]-") {
			t.Error("exact 2-hop query fired on the heuristic path")
		}
	}
}

// TestPairsSharingInterface_EdgesPresent_ExactPath asserts requirement (2): when the
// presence-probe reports IMPLEMENTS edges, the dispatcher takes the exact path,
// increments {path="exact"}, suppresses a pair whose receivers share an interface,
// and REPORTS a pair whose receivers do not.
func TestPairsSharingInterface_EdgesPresent_ExactPath(t *testing.T) {
	fetchSig := "func (g *%s) FetchREADME(ctx context.Context) (string, error)"
	validateSig := "func (c *%s) Validate() error"
	f := &fakeCypher{
		implementsCount: `7`, // edges present → exact path
		methodRows: [][]string{
			{`"FetchREADME"`, `"internal/forge/github.go"`, `"` + strings.Replace(fetchSig, "%s", "GitHubForge", 1) + `"`},
			{`"FetchREADME"`, `"internal/forge/gitlab.go"`, `"` + strings.Replace(fetchSig, "%s", "GitLabForge", 1) + `"`},
			{`"Validate"`, `"internal/orders/cart.go"`, `"` + strings.Replace(validateSig, "%s", "Cart", 1) + `"`},
			{`"Validate"`, `"internal/billing/invoice.go"`, `"` + strings.Replace(validateSig, "%s", "Invoice", 1) + `"`},
		},
		// Only GitHubForge and GitLabForge share an interface (both implement Forge).
		// Cart and Invoice appear in NO 2-hop row → they share no interface.
		twoHopRows: [][]string{
			{`"GitHubForge"`, `"internal/forge/github.go"`, `"GitLabForge"`, `"internal/forge/gitlab.go"`},
		},
	}
	e := newFakeExpander(f)

	suppressedPK := NewPairKey("internal/forge/github.go", "FetchREADME", "internal/forge/gitlab.go", "FetchREADME")
	reportedPK := NewPairKey("internal/orders/cart.go", "Validate", "internal/billing/invoice.go", "Validate")
	pairs := []PairKey{suppressedPK, reportedPK}

	before := counter(t, ifacePathExact)
	got, err := e.PairsSharingInterface(context.Background(), "g", pairs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := counter(t, ifacePathExact); after != before+1 {
		t.Errorf("exact path counter: got %v, want %v", after, before+1)
	}
	if !got[suppressedPK] {
		t.Error("exact path must suppress FetchREADME: GitHubForge & GitLabForge share interface Forge")
	}
	if got[reportedPK] {
		t.Error("exact path must REPORT Validate: Cart & Invoice share no interface")
	}
	// The heuristic 8-column query must NOT have fired on the exact path.
	for _, q := range f.seen {
		if strings.Contains(q, "b.kind = 'method'") {
			t.Error("heuristic query fired on the exact path")
		}
	}
}

// TestPairsSharingInterfaceExact_SameNameDifferentPackageNotOverSuppressed is the
// end-to-end MAJOR-1 regression guard. It drives the FULL exact path (method
// resolution → 2-hop → sibling match) through the fake DB and proves bare-name
// receiver collision can no longer over-suppress.
//
// Three method "M" receivers exist:
//   - parser.Cache (internal/parser) — IMPLEMENTS an interface that Forge also does
//   - cache.Cache  (internal/cache)  — implements NOTHING
//   - Forge        (internal/forge)  — IMPLEMENTS the same interface as parser.Cache
//
// The 2-hop returns ONLY the (parser.Cache, Forge) pair. The candidate pair under
// test is cache.Cache.M vs Forge.M — they share NO interface, so it must be REPORTED.
// Under the reverted bare-name keying, cache.Cache's bare receiver "Cache" collides
// with parser.Cache's, the connected key "Cache|Forge" matches, and the pair is
// wrongly SUPPRESSED. Package-qualified keying keeps them distinct → REPORTED.
func TestPairsSharingInterfaceExact_SameNameDifferentPackageNotOverSuppressed(t *testing.T) {
	sig := "func (x *%s) M() error"
	f := &fakeCypher{
		implementsCount: `5`,
		methodRows: [][]string{
			{`"M"`, `"internal/parser/cache.go"`, `"` + strings.Replace(sig, "%s", "Cache", 1) + `"`},
			{`"M"`, `"internal/cache/cache.go"`, `"` + strings.Replace(sig, "%s", "Cache", 1) + `"`},
			{`"M"`, `"internal/forge/forge.go"`, `"` + strings.Replace(sig, "%s", "Forge", 1) + `"`},
		},
		// Only parser.Cache and Forge share an interface.
		twoHopRows: [][]string{
			{`"Cache"`, `"internal/parser/cache.go"`, `"Forge"`, `"internal/forge/forge.go"`},
		},
	}
	e := newFakeExpander(f)

	// Candidate pair: cache.Cache.M vs Forge.M — share no interface.
	reportedPK := NewPairKey("internal/cache/cache.go", "M", "internal/forge/forge.go", "M")
	// Control pair: parser.Cache.M vs Forge.M — DO share an interface → suppressed.
	suppressedPK := NewPairKey("internal/parser/cache.go", "M", "internal/forge/forge.go", "M")
	pairs := []PairKey{reportedPK, suppressedPK}

	got, err := e.pairsSharingInterfaceExact(context.Background(), "g", pairs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[reportedPK] {
		t.Error("MAJOR-1 end-to-end: cache.Cache.M vs Forge.M share no interface and must be REPORTED, " +
			"not suppressed via bare-name collision with parser.Cache")
	}
	if !got[suppressedPK] {
		t.Error("control: parser.Cache.M vs Forge.M share an interface and must be suppressed")
	}
}

// TestGraphHasImplementsEdges_CountParsing is the MINOR regression guard: the
// agtype count rendering must be trimmed AND quote-stripped before the > 0 check.
// A space-padded or quoted "0" must read as "no edges" (false); a positive count
// must read as "has edges" (true).
func TestGraphHasImplementsEdges_CountParsing(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`0`, false},
		{` 0`, false},  // space-padded zero (MINOR: TrimSpace before compare)
		{`"0"`, false}, // quoted zero (MINOR: Trim quotes before compare)
		{` "0" `, false},
		{`7`, true},
		{` 7`, true},
		{`"7"`, true},
		{``, false}, // empty rendering → no edges
	}
	for _, c := range cases {
		f := &fakeCypher{implementsCount: c.raw}
		e := newFakeExpander(f)
		got := e.graphHasImplementsEdges(context.Background(), "g")
		if got != c.want {
			t.Errorf("graphHasImplementsEdges(count=%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
