package codegraph

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
// This happy-path test survives the pr-review-council #295 remediation
// (fail-closed + chunked positive-IN rewrite, see
// TestPruneStaleDeadCodeScores_FailsClosedOnUnparseableOrphan /
// TestPruneStaleDeadCodeScores_ZeroOrphansWipesRepoRows for the new
// failure-path coverage) — both name and file parse cleanly here, so the
// fail-closed guard never fires and the net deletion behaviour is unchanged.
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

// TestPruneStaleDeadCodeScores_FailsClosedOnUnparseableOrphan is the RED proof
// for the pr-review-council #295 HIGH finding: the original pruneStaleDeadCodeScores
// computed its keep-set via best-effort parsing — any orphan vertex whose
// name/file failed to extract was silently DROPPED from the keep-set (a bare
// `continue`), narrowing it. Since deletion was an anti-join ("stored key NOT
// IN keep-set"), a narrowed keep-set could delete a DIFFERENT, unrelated
// function's still-legitimate row — one that never even failed to parse — just
// because the read that round was compromised by a sibling malformed vertex.
//
// Fixture: the graph's ONLY orphan is "Malformed" (kind=function, file set to
// the empty string), which fails the file=="" parse check. code_dead_code_scores
// has a stored row for "StillOrphan"/"pkg/still.go" — a function that is NOT
// among the vertices returned by this round's orphan query at all (simulating:
// we cannot tell from this compromised read whether it is still an orphan).
//
// Assert: prune returns (0, nil), the StillOrphan row SURVIVES (fail-closed —
// no DELETE runs at all this round), and deadCodeScorePruneAbortedTotal
// increments by exactly 1.
//
// RED / anti-vacuous proof: with the fail-closed guard neutered back to a bare
// `continue` (the original bug), Malformed drops out of the keep-set, the
// keep-set ends up empty, and StillOrphan's key — absent from an empty
// keep-set — becomes eligible for deletion under the anti-join/toDelete
// computation, flipping the row-survival assertion to FAIL. Confirmed
// empirically (see commit message / STATUS report) by temporarily reverting
// the abort to `continue` and re-running this test.
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestPruneStaleDeadCodeScores_FailsClosedOnUnparseableOrphan(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() }) // see LIFO-ordering note on the sibling test above

	store := NewStore(pool)

	const testGraph = "code_prunefailclosedtest"
	const repoKey = "code_prunefailclosedtest_repo"

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

	// The graph's ONLY vertex/orphan: file is an empty string, so
	// strings.Trim(row[1], `"`) == "" — this is the malformed-orphan branch.
	if err := store.ExecCypherWrite(ctx,
		testGraph, `CREATE (s:Symbol {name: 'Malformed', file: '', kind: 'function'})`); err != nil {
		t.Fatalf("fixture write failed: %v", err)
	}

	// Seed a stored row for a DIFFERENT function that is NOT among the current
	// orphan query's results at all (not a vertex in this graph). Its
	// "true" current-orphan status is unknowable from this compromised read.
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO code_dead_code_scores (repo_key, name, file, score, scored_at)
		VALUES ($1, 'StillOrphan', 'pkg/still.go', 0.6, now())`,
		repoKey); err != nil {
		conn.Release()
		t.Fatalf("seed code_dead_code_scores: %v", err)
	}
	conn.Release()

	before := testutil.ToFloat64(deadCodeScorePruneAbortedTotal)

	pruned, err := store.pruneStaleDeadCodeScores(ctx, testGraph, repoKey)
	if err != nil {
		t.Fatalf("pruneStaleDeadCodeScores: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 rows pruned (fail-closed abort), got %d", pruned)
	}

	after := testutil.ToFloat64(deadCodeScorePruneAbortedTotal)
	if after-before != 1 {
		t.Errorf("expected deadCodeScorePruneAbortedTotal to increment by 1, got delta %v", after-before)
	}

	verifyConn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE (verify): %v", err)
	}
	defer verifyConn.Release()

	var stillOrphanCount int
	if err := verifyConn.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_dead_code_scores WHERE repo_key = $1 AND name = 'StillOrphan'`,
		repoKey).Scan(&stillOrphanCount); err != nil {
		t.Fatalf("query stillorphan count: %v", err)
	}
	if stillOrphanCount != 1 {
		t.Errorf("expected StillOrphan row to SURVIVE (fail-closed — no DELETE this round), found %d row(s)", stillOrphanCount)
	}
}

// TestPruneStaleDeadCodeScores_ZeroOrphansWipesRepoRows verifies the
// legitimate counterpart to the fail-closed guard above: a graph with
// function symbols but ZERO current orphans (every function has an incoming
// CALLS edge) is NOT a parse failure — the orphan query validly returns zero
// rows — and must still allow the full, correct deletion of every stored
// score row for the repo (they are all now stale by definition). This guards
// against an over-broad fail-closed check that mistakes "legitimately empty"
// for "malformed".
//
// Fixture: A and B call each other (cycle) so both have an incoming edge —
// zero orphans. code_dead_code_scores has one stale row (for a function no
// longer present/orphaned at all). Assert the row is deleted, the returned
// count equals the total stored (mass-wipe path), and
// deadCodeScorePruneAbortedTotal does NOT increment (distinguishing this from
// the fail-closed test above).
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestPruneStaleDeadCodeScores_ZeroOrphansWipesRepoRows(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() }) // see LIFO-ordering note on the first test above

	store := NewStore(pool)

	const testGraph = "code_prunezerotest"
	const repoKey = "code_prunezerotest_repo"

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

	// A and B call each other — both have an incoming CALLS edge, so the
	// orphan query legitimately returns zero rows.
	creates := []string{
		`CREATE (s:Symbol {name: 'A', file: 'pkg/a.go', kind: 'function'})`,
		`CREATE (s:Symbol {name: 'B', file: 'pkg/b.go', kind: 'function'})`,
		`MATCH (a:Symbol {name: 'A'}), (b:Symbol {name: 'B'}) CREATE (a)-[r:CALLS]->(b)`,
		`MATCH (a:Symbol {name: 'B'}), (b:Symbol {name: 'A'}) CREATE (a)-[r:CALLS]->(b)`,
	}
	for _, cypher := range creates {
		if err := store.ExecCypherWrite(ctx, testGraph, cypher); err != nil {
			t.Fatalf("fixture write failed (%s): %v", cypher, err)
		}
	}

	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO code_dead_code_scores (repo_key, name, file, score, scored_at)
		VALUES ($1, 'Ghost', 'pkg/ghost.go', 0.6, now())`,
		repoKey); err != nil {
		conn.Release()
		t.Fatalf("seed code_dead_code_scores: %v", err)
	}
	conn.Release()

	before := testutil.ToFloat64(deadCodeScorePruneAbortedTotal)

	pruned, err := store.pruneStaleDeadCodeScores(ctx, testGraph, repoKey)
	if err != nil {
		t.Fatalf("pruneStaleDeadCodeScores: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 row pruned (legitimate zero-orphan full wipe), got %d", pruned)
	}

	after := testutil.ToFloat64(deadCodeScorePruneAbortedTotal)
	if after != before {
		t.Errorf("expected deadCodeScorePruneAbortedTotal NOT to increment for a legitimate zero-orphan result, delta %v", after-before)
	}

	verifyConn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE (verify): %v", err)
	}
	defer verifyConn.Release()

	var ghostCount int
	if err := verifyConn.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_dead_code_scores WHERE repo_key = $1 AND name = 'Ghost'`,
		repoKey).Scan(&ghostCount); err != nil {
		t.Fatalf("query ghost count: %v", err)
	}
	if ghostCount != 0 {
		t.Errorf("expected Ghost row to be deleted (zero current orphans), found %d row(s)", ghostCount)
	}
}

// TestDropGraph_DeletesDeadCodeScores is the regression test for pr-review-council
// #295 Finding 3: DropGraph deleted code_graph_meta + code_file_mtimes but NOT
// code_dead_code_scores, so a graph that is dropped and never rebuilt keeps its
// stale dead-code score rows forever (pruneStaleDeadCodeScores only ever runs as
// part of ScoreDeadCodeCandidates, which never runs again for a dropped repo).
//
// Skipped when DATABASE_URL is unset — requires a live PostgreSQL + AGE instance.
func TestDropGraph_DeletesDeadCodeScores(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() }) // see LIFO-ordering note on the first test above

	store := NewStore(pool)

	const testGraph = "code_dropgraph_deadcode_test"
	const repoKey = testGraph

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

	conn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO code_dead_code_scores (repo_key, name, file, score, scored_at)
		VALUES ($1, 'Stale', 'pkg/stale.go', 0.6, now())`,
		repoKey); err != nil {
		conn.Release()
		t.Fatalf("seed code_dead_code_scores: %v", err)
	}
	conn.Release()

	if err := store.DropGraph(ctx, testGraph, repoKey); err != nil {
		t.Fatalf("DropGraph: %v", err)
	}

	verifyConn, err := store.acquireAGE(ctx)
	if err != nil {
		t.Fatalf("acquireAGE (verify): %v", err)
	}
	defer verifyConn.Release()

	var staleCount int
	if err := verifyConn.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_dead_code_scores WHERE repo_key = $1`,
		repoKey).Scan(&staleCount); err != nil {
		t.Fatalf("query stale count: %v", err)
	}
	if staleCount != 0 {
		t.Errorf("expected code_dead_code_scores rows to be deleted when their graph is dropped, found %d row(s)", staleCount)
	}
}
