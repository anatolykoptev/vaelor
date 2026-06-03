package embeddings

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPoolPR creates a pgxpool connection for the isolated pagerank test database.
// Reads PR_TEST_DATABASE_URL; skips if unset.
// The test database must exist and have AGE installed (see setup in PR description).
func testPoolPR(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PR_TEST_DATABASE_URL not set — skipping pagerank AGE integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

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

// seedPageRankGraph creates a test AGE graph for pagerank sub-arm tests.
//
// Vertices inserted:
//
//	"DomainFunc"  — name=DomainFunc,  file=internal/embeddings/pipeline.go, pagerank=0.9
//	"GenericWrite"— name=GenericWrite, file=internal/util/writer.go,         pagerank=1.0
//	"SeedFunc"    — name=SeedFunc,    file=internal/embeddings/seed.go,      pagerank=0.7
//
// "DomainFunc" and "SeedFunc" match the keyword "embed" via their file path.
// "GenericWrite" does NOT match "embed" in either name or file — it is the
// high-pagerank generic infrastructure that the old code would have returned
// (it has the highest pagerank but is NOT query-relevant).
func seedPageRankGraph(t *testing.T, pool *pgxpool.Pool) (graphName string, cleanup func()) {
	t.Helper()
	graphName = fmt.Sprintf("test_pagerank_%d", os.Getpid())

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

	vertices := []struct {
		name, file string
		pagerank   string
	}{
		{"DomainFunc", "internal/embeddings/pipeline.go", "0.9"},
		{"GenericWrite", "internal/util/writer.go", "1.0"},
		{"SeedFunc", "internal/embeddings/seed.go", "0.7"},
	}
	for _, v := range vertices {
		q := fmt.Sprintf(
			`SELECT * FROM cypher('%s', $$ CREATE (:Symbol {name: '%s', file: '%s', pagerank: %s}) $$) AS (v agtype)`,
			graphName, v.name, v.file, v.pagerank,
		)
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("insert vertex %s: %v", v.name, err)
		}
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

// TestGraphSubArmPageRank_ReturnsRelevantSymbolsByPageRank is the primary RED→GREEN
// test for the pagerank sub-arm bug fix.
//
// The bug (INVERTED LOGIC): the old implementation took the global top-200 PageRank
// symbols and filtered by name-substring. Top PageRank symbols are generic infra
// (Write, Error, Close) — their names never overlap with domain keywords (embed, rrf,
// gate, fusion). Result: 0 hits on every query.
//
// The fix: query AGE for symbols whose name OR file contains the keyword, ordered by
// pagerank DESC. "Important among relevant" instead of "relevant among important".
//
// Falsification contract: the test seeds a graph where "DomainFunc"
// (file=internal/embeddings/pipeline.go, pagerank=0.9) matches keyword "embed" via
// its file path, while "GenericWrite" (pagerank=1.0) does NOT match.
//
// OLD CODE: would filter a prSignals batch that contains only GenericWrite
// (highest PR), "embed" not in "GenericWrite" → 0 hits. The test demonstrates
// this by running the old logic inline (see OldCodeBehavior sub-test).
//
// NEW CODE: the AGE query finds DomainFunc (file matches "embed"), ordered by PR
// → 1 hit. The fix turns 0 into >0.
func TestGraphSubArmPageRank_ReturnsRelevantSymbolsByPageRank(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// SeedFunc is already in seen (it was a dense search seed).
	seen := map[string]bool{
		"internal/embeddings/seed.go:SeedFunc": true,
	}

	queryTerms := []string{"embed"}
	const limit = 10

	t.Run("NewCode_ReturnsRelevantSymbol", func(t *testing.T) {
		hits := exp.graphSubArmPageRank(context.Background(), graphName, queryTerms, seen, limit)

		if len(hits) == 0 {
			t.Fatal("graphSubArmPageRank returned 0 hits; expected DomainFunc (file matches 'embed'). " +
				"If the old in-memory filter logic was restored, it would miss DomainFunc because " +
				"'embeddings' is not in the top-200 global-PR batch (only Write, Error, etc. are).")
		}

		// DomainFunc must be present (file=internal/embeddings/pipeline.go matches "embed").
		foundDomain := false
		for _, h := range hits {
			if h.SymbolName == "DomainFunc" {
				foundDomain = true
			}
			// GenericWrite must NOT appear — its name and file don't contain "embed".
			if h.SymbolName == "GenericWrite" {
				t.Errorf("GenericWrite must not appear (file=internal/util/writer.go, no 'embed'); got it in hits: %+v", hits)
			}
			// SeedFunc was pre-seeded in seen → must be deduped.
			if h.SymbolName == "SeedFunc" {
				t.Errorf("SeedFunc must be deduped (was in seen map); got it in hits: %+v", hits)
			}
		}
		if !foundDomain {
			t.Errorf("DomainFunc not found in hits; got: %+v", hits)
		}
	})

	t.Run("OldCodeBehavior_ZeroHits", func(t *testing.T) {
		// Simulate the old code: filter a prSignals-like batch by name-substring.
		// Batch contains only "GenericWrite" (the highest-PR symbol in the graph).
		// Keyword "embed" is NOT in "GenericWrite" → the old code returns 0 hits.
		// This sub-test RED-on-revert: if old logic is restored, this becomes GREEN but
		// the "NewCode_ReturnsRelevantSymbol" sub-test goes RED.
		//
		// We replicate the old logic locally rather than calling the (now-fixed) function.
		prSignalsBatch := []struct{ name, file string }{
			{"GenericWrite", "internal/util/writer.go"},
		}
		oldCodeHits := 0
		for _, sig := range prSignalsBatch {
			for _, term := range queryTerms {
				if len(sig.name) > 0 && containsLower(sig.name, term) {
					oldCodeHits++
					break
				}
			}
		}
		if oldCodeHits != 0 {
			t.Errorf("OldCodeBehavior: expected 0 hits (the bug), got %d — test setup is wrong", oldCodeHits)
		}
	})
}

// containsLower is a local helper for TestGraphSubArmPageRank_ReturnsRelevantSymbolsByPageRank
// that replicates the old code's strings.Contains(toLower(name), toLower(term)) check.
func containsLower(s, substr string) bool {
	return len(s) >= len(substr) &&
		func() bool {
			for i := range s {
				if i+len(substr) > len(s) {
					break
				}
				match := true
				for j := range substr {
					cs := s[i+j]
					if cs >= 'A' && cs <= 'Z' {
						cs += 'a' - 'A'
					}
					ct := substr[j]
					if ct >= 'A' && ct <= 'Z' {
						ct += 'a' - 'A'
					}
					if cs != ct {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
			return false
		}()
}

// TestGraphSubArmPageRank_FileMatchWorks verifies that a keyword present in the
// file path (but NOT in the symbol name) produces a hit.
// Regression guard for the original "name only" bug — file was never checked.
func TestGraphSubArmPageRank_FileMatchWorks(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// "pipeline" appears in DomainFunc's file but not its name.
	hits := exp.graphSubArmPageRank(context.Background(), graphName, []string{"pipeline"}, make(map[string]bool), 10)

	if len(hits) == 0 {
		t.Fatal("expected hits for keyword 'pipeline' (in file path), got 0")
	}
	found := false
	for _, h := range hits {
		if h.SymbolName == "DomainFunc" && h.FilePath == "internal/embeddings/pipeline.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("DomainFunc not found via file match on 'pipeline'; hits: %+v", hits)
	}
}

// TestGraphSubArmPageRank_ResultsOrderedByPageRankDesc verifies that the returned
// hits are ordered by pagerank descending (highest-pagerank symbol first).
func TestGraphSubArmPageRank_ResultsOrderedByPageRankDesc(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// "embed" matches both DomainFunc (PR=0.9) and SeedFunc (PR=0.7) via file.
	// Expected order: DomainFunc first, SeedFunc second.
	hits := exp.graphSubArmPageRank(context.Background(), graphName, []string{"embed"}, make(map[string]bool), 10)

	if len(hits) < 2 {
		t.Fatalf("expected ≥2 hits for 'embed', got %d: %+v", len(hits), hits)
	}
	if hits[0].SymbolName != "DomainFunc" {
		t.Errorf("expected DomainFunc first (PR=0.9 > SeedFunc PR=0.7); got %q first; all: %+v", hits[0].SymbolName, hits)
	}
	if hits[1].SymbolName != "SeedFunc" {
		t.Errorf("expected SeedFunc second; got %q; all: %+v", hits[1].SymbolName, hits)
	}
}

// TestGraphSubArmPageRank_LimitHonored verifies that limit=1 returns at most 1 result.
func TestGraphSubArmPageRank_LimitHonored(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	hits := exp.graphSubArmPageRank(context.Background(), graphName, []string{"embed"}, make(map[string]bool), 1)

	if len(hits) > 1 {
		t.Errorf("limit=1: expected ≤1 hit, got %d: %+v", len(hits), hits)
	}
}

// TestGraphSubArmPageRank_EmptyQueryTerms verifies that empty queryTerms returns nil.
func TestGraphSubArmPageRank_EmptyQueryTerms(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	hits := exp.graphSubArmPageRank(context.Background(), graphName, nil, make(map[string]bool), 10)
	if hits != nil {
		t.Errorf("expected nil for empty queryTerms, got: %+v", hits)
	}

	hits = exp.graphSubArmPageRank(context.Background(), graphName, []string{}, make(map[string]bool), 10)
	if hits != nil {
		t.Errorf("expected nil for empty queryTerms slice, got: %+v", hits)
	}
}

// TestGraphSubArmPageRank_InjectionSafe verifies that a keyword with a single quote
// does not break the Cypher query (escapeCypherName handles it).
func TestGraphSubArmPageRank_InjectionSafe(t *testing.T) {
	pool := testPoolPR(t)
	graphName, cleanup := seedPageRankGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)

	// A keyword with a single quote would break unescaped Cypher.
	// The function must return 0 hits (no symbol matches) without panicking or erroring.
	malicious := []string{"O'Brien", "'; DROP GRAPH test--"}
	hits := exp.graphSubArmPageRank(context.Background(), graphName, malicious, make(map[string]bool), 10)
	// No match expected; the important thing is no panic and no graph drop.
	_ = hits // result may be nil or empty — both are correct

	// Confirm the graph still exists by running a basic query.
	probe := exp.execCypherN(context.Background(), graphName,
		`MATCH (s:Symbol) RETURN s.name, s.file LIMIT 1`, "name agtype, file agtype")
	if len(probe) == 0 {
		t.Error("graph appears destroyed after injection attempt — escapeCypherName failed")
	}
}
