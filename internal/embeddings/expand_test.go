package embeddings

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestAgeExpandSetupNoLOAD verifies that ageExpandSetup no longer contains a LOAD directive.
// Regression guard: per-connection LOAD was removed in favour of shared_preload_libraries.
// If this test fails, someone re-introduced LOAD — verify postgresql.conf instead.
func TestAgeExpandSetupNoLOAD(t *testing.T) {
	if strings.Contains(strings.ToUpper(ageExpandSetup), "LOAD") {
		t.Errorf("ageExpandSetup must not contain LOAD directive (rely on shared_preload_libraries): %q", ageExpandSetup)
	}
}

// TestBuildNameFilter_EscapesInjectionClasses pins the literal rendering for
// the three breakout classes (#802):
//
//   - backslash: `foo\` must render as 'foo\\' — unescaped, the backslash
//     escapes the closing quote and the literal swallows the rest of the
//     query. RED on the pre-fix quote-only escaper.
//   - single quote: "O'Brien" → 'O\'Brien'.
//   - dollar tag: a value containing cypherDollarTag must lose it so the tag
//     can never close the SQL dollar-quote carrying the Cypher text.
func TestBuildNameFilter_EscapesInjectionClasses(t *testing.T) {
	got := buildNameFilter("a", []string{`foo\`, "O'Brien", "x" + cypherDollarTag + "y"})
	want := `a.name = 'foo\\' OR a.name = 'O\'Brien' OR a.name = 'xy'`
	if got != want {
		t.Fatalf("buildNameFilter injection-safe rendering:\n got %q\nwant %q", got, want)
	}
}

// TestWrapCypherSQL_TaggedDollarQuoteAndGraphName pins the SQL wrapper: the
// Cypher text travels inside the named dollar tag, and the graph name is a
// '...' SQL literal escaped by quote doubling.
func TestWrapCypherSQL_TaggedDollarQuoteAndGraphName(t *testing.T) {
	got := wrapCypherSQL("g'r$a/ph", "MATCH (n) RETURN n", "x agtype")
	want := `SELECT * FROM ag_catalog.cypher('g''r$a/ph', $vaelor$ MATCH (n) RETURN n $vaelor$) AS (x agtype)`
	if got != want {
		t.Fatalf("wrapCypherSQL:\n got %q\nwant %q", got, want)
	}
}

// TestExecCypher_ReadOnlyTxRejectsWrite is the live-AGE proof of the #802
// guard: a write clause issued through execCypherN (the path used by
// semantic_search expansion and find_duplicates) must be rejected by the
// read-only transaction server-side — whatever the Cypher text contains.
// Reads PR_TEST_DATABASE_URL; skips if unset.
//
// RED on the pre-fix code (no transaction): CREATE executes and the probe
// finds the vertex. With the read-only tx the server refuses and count(n)
// stays 0.
func TestExecCypher_ReadOnlyTxRejectsWrite(t *testing.T) {
	pool := testPoolAGE(t)
	graphName, cleanup := seedCommunityGraph(t, pool)
	defer cleanup()

	exp := NewExpander(pool)
	ctx := context.Background()

	exp.execCypherN(ctx, graphName,
		`CREATE (:Pwned {name: 'pwn'})`, "v agtype")

	// Probe on a direct writable connection — the vertex must NOT exist.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET search_path TO ag_catalog, "$user", public`); err != nil {
		t.Fatalf("setup: %v", err)
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
		t.Fatalf("write clause executed through execCypherN — read-only tx guard missing (#802): Pwned count=%s", count)
	}
}
