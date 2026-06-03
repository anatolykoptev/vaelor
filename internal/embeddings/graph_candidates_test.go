package embeddings

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPoolAGE creates a pgxpool connection for AGE integration tests.
// Skips when DATABASE_URL is not set.
func testPoolAGE(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping AGE integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedCommunityGraph creates a test AGE graph with two Symbol vertices that share a
// community label, inserts a third vertex with no community, then returns the graph
// name and a cleanup function. The seeded vertices have:
//
//	"SeedFunc"   — name=SeedFunc, file=seed.go, kind=function, community=comm-42
//	"PeerFunc"   — name=PeerFunc, file=seed.go, kind=function, community=comm-42
//	"NoCommunity" — name=NoCommunity, file=other.go, kind=function, community absent
func seedCommunityGraph(t *testing.T, pool *pgxpool.Pool) (graphName string, cleanup func()) {
	t.Helper()
	graphName = fmt.Sprintf("test_community_%d", os.Getpid())

	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	setup := `SET search_path TO ag_catalog, "$user", public`
	if _, err := conn.Exec(ctx, setup); err != nil {
		t.Fatalf("AGE setup: %v", err)
	}

	// Drop pre-existing test graph (defensive; normally absent).
	_, _ = conn.Exec(ctx, fmt.Sprintf(`SELECT drop_graph('%s', true)`, graphName))

	if _, err := conn.Exec(ctx, fmt.Sprintf(`SELECT create_graph('%s')`, graphName)); err != nil {
		t.Fatalf("create_graph: %v", err)
	}

	// Create Symbol vertex label.
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$ CREATE (:Symbol) $$) AS (v agtype)`, graphName,
	)); err != nil {
		t.Fatalf("create vlabel Symbol: %v", err)
	}

	// Insert two community-tagged vertices and one without.
	seeds := []struct {
		name, file, kind, comm string
	}{
		{"SeedFunc", "seed.go", "function", "comm-42"},
		{"PeerFunc", "seed.go", "function", "comm-42"},
	}
	for _, s := range seeds {
		q := fmt.Sprintf(
			`SELECT * FROM cypher('%s', $$ CREATE (:Symbol {name: '%s', file: '%s', kind: '%s', community: '%s'}) $$) AS (v agtype)`,
			graphName, s.name, s.file, s.kind, s.comm,
		)
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("insert vertex %s: %v", s.name, err)
		}
	}
	// NoCommunity vertex — no community property.
	noCommQ := fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$ CREATE (:Symbol {name: 'NoCommunity', file: 'other.go', kind: 'function'}) $$) AS (v agtype)`,
		graphName,
	)
	if _, err := conn.Exec(ctx, noCommQ); err != nil {
		t.Fatalf("insert NoCommunity vertex: %v", err)
	}

	cleanup = func() {
		cctx := context.Background()
		c, err := pool.Acquire(cctx)
		if err != nil {
			return
		}
		defer c.Release()
		_, _ = c.Exec(cctx, setup)
		_, _ = c.Exec(cctx, fmt.Sprintf(`SELECT drop_graph('%s', true)`, graphName))
	}
	return graphName, cleanup
}

// TestGraphSubArmCommunity_ReturnsHits verifies that graphSubArmCommunity
// correctly resolves a seed's community via a 4-column AGE query and then fetches
// same-community members via a 3-column query, returning >0 results.
//
// Falsification contract: before the fix, execCypher (3-col AS-clause) was used
// for the 4-column RETURN — AGE raised "return row and column definition list do
// not match", conn.Query errored, execCypher returned nil, and the function
// returned nil on every call. With the 3-col execCypher call uncommented and
// execCypherN reverted to execCypher, this test MUST go RED.
func TestGraphSubArmCommunity_ReturnsHits(t *testing.T) {
	pool := testPoolAGE(t)
	graphName, cleanup := seedCommunityGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// seeds[0] = SeedFunc, which has community=comm-42.
	seeds := []SearchResult{
		{SymbolName: "SeedFunc", FilePath: "seed.go"},
	}
	// Mimic buildSeedSet: pre-populate seen with the seed so it is deduped from hits.
	seen := buildSeedSet(seeds)
	const limit = 10

	hits := exp.graphSubArmCommunity(context.Background(), graphName, seeds, seen, limit)

	if len(hits) == 0 {
		t.Fatal("graphSubArmCommunity returned 0 hits; expected PeerFunc (same community=comm-42). " +
			"If execCypherN was reverted to execCypher for the 4-col lookup, AGE raises column arity " +
			"mismatch → nil returned → this test correctly goes RED.")
	}

	// PeerFunc should be present (same community as SeedFunc).
	// SeedFunc itself is in seeds → added to seen by buildSeedSet → excluded from hits.
	foundPeer := false
	for _, h := range hits {
		if h.SymbolName == "PeerFunc" && h.FilePath == "seed.go" {
			foundPeer = true
		}
		// SeedFunc was the seed; it must not appear (dedup via seen map).
		if h.SymbolName == "SeedFunc" {
			t.Errorf("SeedFunc must be excluded (was the seed); got it in hits: %+v", hits)
		}
	}
	if !foundPeer {
		t.Errorf("PeerFunc not found in community hits; got: %+v", hits)
	}
}

// TestGraphSubArmCommunity_NoHitsWhenSeedHasNoCommunity verifies that when the
// top seed has no community property (AGE returns null), the function returns nil
// without error — not a test of the column-arity bug, but of the null-community guard.
func TestGraphSubArmCommunity_NoHitsWhenSeedHasNoCommunity(t *testing.T) {
	pool := testPoolAGE(t)
	graphName, cleanup := seedCommunityGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// NoCommunity has no community property → s.community returns null in AGE.
	seeds := []SearchResult{
		{SymbolName: "NoCommunity", FilePath: "other.go"},
	}
	seen := make(map[string]bool)

	hits := exp.graphSubArmCommunity(context.Background(), graphName, seeds, seen, 10)

	if len(hits) != 0 {
		t.Errorf("expected nil hits for seed with no community; got %d hits: %+v", len(hits), hits)
	}
}
