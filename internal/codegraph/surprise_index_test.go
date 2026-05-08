package codegraph

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIndexSurprise_Integration builds a tiny fixture graph with two symbols in
// different packages connected by a CALLS edge, runs IndexSurpriseEdges and
// IndexSurpriseNodes, then asserts that both r.surprise and s.surprise are > 0.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestIndexSurprise_Integration(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)

	// Use a dedicated test graph so we don't pollute real data.
	const testGraph = "code_surprisetest"

	// Clean up before and after.
	_ = store.DropGraph(ctx, testGraph, testGraph)
	t.Cleanup(func() { _ = store.DropGraph(ctx, testGraph, testGraph) })

	if err := store.EnsureGraph(ctx, testGraph); err != nil {
		t.Fatalf("EnsureGraph: %v", err)
	}

	// Insert two Symbol vertices in different packages.
	symA := "SurpriseTestCaller"
	fileA := "pkg/a/caller.go"
	symB := "SurpriseTestCallee"
	fileB := "pkg/b/callee.go"
	communityA := 1
	communityB := 2

	createA := fmt.Sprintf(
		`CREATE (s:Symbol {name: '%s', file: '%s', community: %d, pagerank: 0.05})`,
		symA, fileA, communityA,
	)
	createB := fmt.Sprintf(
		`CREATE (s:Symbol {name: '%s', file: '%s', community: %d, pagerank: 0.5})`,
		symB, fileB, communityB,
	)
	edge := fmt.Sprintf(
		`MATCH (a:Symbol {name: '%s'}), (b:Symbol {name: '%s'}) CREATE (a)-[r:CALLS]->(b)`,
		symA, symB,
	)

	for _, cypher := range []string{createA, createB, edge} {
		if err := store.ExecCypherWrite(ctx, testGraph, cypher); err != nil {
			t.Fatalf("fixture write failed (%s): %v", cypher[:40], err)
		}
	}

	// Run the indexers.
	if err := IndexSurpriseEdges(ctx, store, testGraph); err != nil {
		t.Fatalf("IndexSurpriseEdges: %v", err)
	}
	if err := IndexSurpriseNodes(ctx, store, testGraph); err != nil {
		t.Fatalf("IndexSurpriseNodes: %v", err)
	}

	// Assert r.surprise > 0 on the CALLS edge.
	t.Run("edge_surprise_positive", func(t *testing.T) {
		q := fmt.Sprintf(
			`MATCH (a:Symbol {name: '%s'})-[r:CALLS]->(b:Symbol {name: '%s'}) RETURN r.surprise`,
			symA, symB,
		)
		rows, err := store.ExecCypher(ctx, testGraph, q, 1)
		if err != nil {
			t.Fatalf("edge surprise query: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("no edge found after indexing")
		}
		score := atofSafe(rows[0][0])
		if score <= 0 {
			t.Errorf("expected r.surprise > 0 for cross-package cross-community edge, got %v", score)
		}
		t.Logf("r.surprise = %v", score)
	})

	// Assert s.surprise > 0 on both endpoint symbols.
	for _, sym := range []struct{ name, file string }{
		{symA, fileA},
		{symB, fileB},
	} {
		sym := sym
		t.Run("node_surprise_positive_"+sym.name, func(t *testing.T) {
			q := fmt.Sprintf(
				`MATCH (s:Symbol {name: '%s', file: '%s'}) RETURN s.surprise`,
				sym.name, sym.file,
			)
			rows, err := store.ExecCypher(ctx, testGraph, q, 1)
			if err != nil {
				t.Fatalf("node surprise query: %v", err)
			}
			if len(rows) == 0 {
				t.Fatalf("symbol %s not found", sym.name)
			}
			score := atofSafe(rows[0][0])
			if score <= 0 {
				t.Errorf("expected s.surprise > 0 for %s, got %v", sym.name, score)
			}
			t.Logf("s.surprise(%s) = %v", sym.name, score)
		})
	}
}

// TestIndexSurpriseEdges_GraphMissing_NoOp verifies that IndexSurpriseEdges
// returns nil (not an error) when the AGE graph does not exist. This exercises
// the IsGraphMissingError guard on the write-path fetch.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestIndexSurpriseEdges_GraphMissing_NoOp(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)

	// Use a graph name that does not exist.
	const testGraph = "code_surpedge_missing_test"
	_ = store.DropGraph(ctx, testGraph, testGraph) // ignore error (may not exist)

	if err := IndexSurpriseEdges(ctx, store, testGraph); err != nil {
		t.Errorf("expected nil error for missing graph, got: %v", err)
	}
}

// TestIndexSurpriseNodes_GraphMissing_NoOp verifies that IndexSurpriseNodes
// returns nil (not an error) when the AGE graph does not exist. This exercises
// the IsGraphMissingError guard on the node-fetch path.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestIndexSurpriseNodes_GraphMissing_NoOp(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)

	// Use a graph name that does not exist.
	const testGraph = "code_surpnode_missing_test"
	_ = store.DropGraph(ctx, testGraph, testGraph) // ignore error (may not exist)

	if err := IndexSurpriseNodes(ctx, store, testGraph); err != nil {
		t.Errorf("expected nil error for missing graph, got: %v", err)
	}
}
