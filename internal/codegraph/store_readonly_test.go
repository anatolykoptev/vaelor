package codegraph

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPoolAGE creates a pgxpool for AGE integration tests in codegraph.
// Reads PR_TEST_DATABASE_URL (isolated DB) — refuses the prod "gocode" DB.
func testPoolAGE(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PR_TEST_DATABASE_URL to an isolated DB to run AGE integration tests")
	}
	cfg, parseErr := pgxpool.ParseConfig(dsn)
	if parseErr != nil {
		t.Fatalf("parse PR_TEST_DATABASE_URL: %v", parseErr)
	}
	if strings.EqualFold(cfg.ConnConfig.Database, "gocode") {
		t.Fatalf("refusing to run AGE tests against the prod gocode DB; " +
			"set PR_TEST_DATABASE_URL to an isolated DB (e.g. gocode_testiso)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedReadGuardGraph creates a throwaway AGE graph with one vertex.
func seedReadGuardGraph(t *testing.T, pool *pgxpool.Pool) (graphName string) {
	t.Helper()
	graphName = fmt.Sprintf("test_roguard_%d", os.Getpid())
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		t.Fatalf("AGE setup: %v", err)
	}
	_, _ = conn.Exec(ctx, fmt.Sprintf(`SELECT drop_graph('%s', true)`, graphName))
	if _, err := conn.Exec(ctx, fmt.Sprintf(`SELECT create_graph('%s')`, graphName)); err != nil {
		t.Fatalf("create_graph: %v", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$ CREATE (:Symbol {name: 'seed'}) $$) AS (v agtype)`, graphName,
	)); err != nil {
		t.Fatalf("seed vertex: %v", err)
	}
	t.Cleanup(func() {
		c, err := pool.Acquire(context.Background())
		if err != nil {
			return
		}
		defer c.Release()
		_, _ = c.Exec(context.Background(), ageSetup)
		_, _ = c.Exec(context.Background(), fmt.Sprintf(`SELECT drop_graph('%s', true)`, graphName))
	})
	return graphName
}

// TestExecCypherReadTx_RejectsWriteServerSide probes the #808 backstop
// DIRECTLY: execCypherReadTx bypasses the isReadOnly lexical gate by design,
// so a write clause must be refused by the read-only transaction itself —
// Postgres answers SQLSTATE 25006 the moment the parsed statement writes.
//
// The assertion is on the ERROR, not just the row count: without pgx.ReadOnly
// the CREATE still rolls back with the tx (count stays 0) but returns no
// error — a silent execution the guard is supposed to make impossible.
// Falsification (red-on-revert): drop pgx.ReadOnly from the BeginTx options →
// err is nil → FAIL.
func TestExecCypherReadTx_RejectsWriteServerSide(t *testing.T) {
	pool := testPoolAGE(t)
	graphName := seedReadGuardGraph(t, pool)
	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.execCypherReadTx(ctx, graphName, `CREATE (:Pwned {name: 'pwn'})`, 1)
	if err == nil {
		t.Fatal("write clause returned no error — read-only tx missing (#808)")
	}

	// Belt: probe on a direct writable connection — the vertex must NOT exist
	// even transiently committed.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire probe conn: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		t.Fatalf("probe setup: %v", err)
	}
	var count string
	err = conn.QueryRow(ctx, fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$ MATCH (n:Pwned) RETURN count(n) $$) AS (c agtype)`,
		graphName,
	)).Scan(&count)
	if err != nil {
		t.Fatalf("probe count: %v", err)
	}
	if count != "0" {
		t.Fatalf("write clause persisted — Pwned count=%s", count)
	}
}

// TestExecCypher_ReadPathWorksInReadOnlyTx is the functional regression: a
// legitimate read must still return rows through the new tx wrapper (guards
// against a "fix" that breaks the read path, e.g. SET inside a read-only tx).
func TestExecCypher_ReadPathWorksInReadOnlyTx(t *testing.T) {
	pool := testPoolAGE(t)
	graphName := seedReadGuardGraph(t, pool)
	store := NewStore(pool)

	rows, err := store.ExecCypher(context.Background(), graphName,
		`MATCH (n:Symbol) RETURN n.name`, 1)
	if err != nil {
		t.Fatalf("read query failed inside read-only tx: %v", err)
	}
	if len(rows) != 1 || !strings.Contains(rows[0][0], "seed") {
		t.Fatalf("read query returned %v, want the seeded vertex", rows)
	}
}
