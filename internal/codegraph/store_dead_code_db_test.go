package codegraph

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPruneStaleDeadCodeScores_RemovesNowCalledKeepsOrphan verifies that
// pruneStaleDeadCodeScores deletes code_dead_code_scores rows for functions
// that are no longer orphans in the live AGE graph (gained an incoming CALLS
// edge), while KEEPING rows for functions that are still orphans — including
// ones whose rerank batch may have been skipped this round (BUG: prior
// behaviour, upsertDeadCodeScores, was insert/update-only and never pruned,
// so code_dead_code_scores only ever grew and code_health over-counted dead
// functions monotonically).
//
// Fixture: three Symbol vertices (Caller, Alive, Orphan), one CALLS edge
// Caller -> Alive (so Alive is a non-orphan; Orphan has no incoming edge so
// it remains an orphan). Seed code_dead_code_scores with stale rows for BOTH
// Alive and Orphan, then assert only Alive's row is deleted.
//
// RED proof: before pruneStaleDeadCodeScores exists, this file does not
// compile — that is the RED state. Once wired, reverting the DELETE inside
// pruneStaleDeadCodeScores to a no-op must flip the "Alive row deleted"
// assertion to FAIL (anti-vacuous check performed manually, documented in
// the commit).
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestPruneStaleDeadCodeScores_RemovesNowCalledKeepsOrphan(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	// NOTE: intentionally NOT `defer pool.Close()`. testing.T.Cleanup handlers
	// run strictly AFTER the test function's own defers, so a bare
	// `defer pool.Close()` closes the pool BEFORE any t.Cleanup(cleanup) below
	// can acquire a connection — the DELETE/DropGraph then silently no-op
	// against a closed pool and prod DB rows/graphs leak (confirmed
	// empirically: this exact pattern in the sibling harness this test
	// mirrors, snapshot_age_test.go / surprise_index_test.go, leaves
	// "code_surprisetest" behind in the live gocode DB today). Registering
	// pool.Close() via t.Cleanup too — BEFORE cleanup — makes LIFO ordering
	// close the pool LAST, after cleanup has actually run.
	t.Cleanup(func() { pool.Close() })

	store := NewStore(pool)

	// Use a dedicated test graph + repo_key so we don't pollute real data.
	const testGraph = "code_prunedeadtest"
	const repoKey = "code_prunedeadtest_repo"

	// Clean up before and after: drop the graph and delete any leftover
	// code_dead_code_scores rows for this test's repo_key.
	cleanup := func() {
		_ = store.DropGraph(ctx, testGraph, testGraph)
		conn, cErr := store.acquireAGE(ctx)
		if cErr == nil {
			_, _ = conn.Exec(ctx, `DELETE FROM code_dead_code_scores WHERE repo_key = $1`, repoKey)
			conn.Release()
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	if err := store.EnsureGraph(ctx, testGraph); err != nil {
		t.Fatalf("EnsureGraph: %v", err)
	}
	if err := store.EnsureLabels(ctx, testGraph); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}

	// Insert three Symbol vertices kind=function. Caller has no incoming edge
	// (also technically an orphan, but irrelevant to this test — it's not
	// seeded in code_dead_code_scores). Alive gains an incoming CALLS edge
	// from Caller, so it is NOT an orphan. Orphan has no incoming edge, so it
	// IS still an orphan.
	fileCaller := "pkg/prune/caller.go"
	fileAlive := "pkg/prune/alive.go"
	fileOrphan := "pkg/prune/orphan.go"

	creates := []string{
		fmt.Sprintf(`CREATE (s:Symbol {name: 'Caller', file: '%s', kind: 'function'})`, fileCaller),
		fmt.Sprintf(`CREATE (s:Symbol {name: 'Alive', file: '%s', kind: 'function'})`, fileAlive),
		fmt.Sprintf(`CREATE (s:Symbol {name: 'Orphan', file: '%s', kind: 'function'})`, fileOrphan),
		`MATCH (a:Symbol {name: 'Caller'}), (b:Symbol {name: 'Alive'}) CREATE (a)-[r:CALLS]->(b)`,
	}
	for _, cypher := range creates {
		if err := store.ExecCypherWrite(ctx, testGraph, cypher); err != nil {
			t.Fatalf("fixture write failed (%s): %v", cypher, err)
		}
	}

	// Seed code_dead_code_scores with stale rows for BOTH Alive (now a
	// non-orphan — must be pruned) and Orphan (still an orphan — must be kept).
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE: %v", err)
	}
	for _, seed := range []struct{ name, file string }{
		{"Alive", fileAlive},
		{"Orphan", fileOrphan},
	} {
		_, err := conn.Exec(ctx, `
			INSERT INTO code_dead_code_scores (repo_key, name, file, score, scored_at)
			VALUES ($1, $2, $3, 0.6, now())
			ON CONFLICT (repo_key, name, file) DO UPDATE
			SET score = EXCLUDED.score, scored_at = EXCLUDED.scored_at`,
			repoKey, seed.name, seed.file)
		if err != nil {
			conn.Release()
			t.Fatalf("seed code_dead_code_scores(%s): %v", seed.name, err)
		}
	}
	conn.Release()

	pruned, err := store.pruneStaleDeadCodeScores(ctx, testGraph, repoKey)
	if err != nil {
		t.Fatalf("pruneStaleDeadCodeScores: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 row pruned, got %d", pruned)
	}

	// Verify directly against the table: Alive's row must be gone, Orphan's
	// row must remain.
	verifyConn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE (verify): %v", err)
	}
	defer verifyConn.Release()

	var aliveCount int
	if err := verifyConn.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_dead_code_scores WHERE repo_key = $1 AND name = 'Alive'`,
		repoKey).Scan(&aliveCount); err != nil {
		t.Fatalf("query alive count: %v", err)
	}
	if aliveCount != 0 {
		t.Errorf("expected Alive row to be deleted (no longer an orphan), but found %d row(s)", aliveCount)
	}

	var orphanCount int
	if err := verifyConn.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_dead_code_scores WHERE repo_key = $1 AND name = 'Orphan'`,
		repoKey).Scan(&orphanCount); err != nil {
		t.Fatalf("query orphan count: %v", err)
	}
	if orphanCount != 1 {
		t.Errorf("expected Orphan row to be kept (still an orphan), found %d row(s)", orphanCount)
	}
}
