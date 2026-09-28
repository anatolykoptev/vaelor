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
// the breakout classes (#802):
//
//   - backslash: `foo\` must render as 'foo\\' — unescaped, the backslash
//     escapes the closing quote and the literal swallows the rest of the
//     query. RED on the pre-fix quote-only escaper.
//   - single quote: "O'Brien" → 'O\'Brien'.
//   - control chars: literal \n/\r/\t are escaped (mirrors
//     codegraph.escapeCypher); null bytes are stripped.
//
// Dollar-quote breakout needs no handling at this layer: wrapCypherSQL
// derives a tag absent from the assembled body, so any "$...$" text in a
// name is inert (see TestWrapCypherSQL_TagAvoidsBodyCollision).
func TestBuildNameFilter_EscapesInjectionClasses(t *testing.T) {
	got := buildNameFilter("a", []string{`foo\`, "O'Brien", "x$cq$y", "line\nbreak"})
	want := `a.name = 'foo\\' OR a.name = 'O\'Brien' OR a.name = 'x$cq$y' OR a.name = 'line\nbreak'`
	if got != want {
		t.Fatalf("buildNameFilter injection-safe rendering:\n got %q\nwant %q", got, want)
	}
}

// TestWrapCypherSQL_TaggedDollarQuoteAndGraphName pins the SQL wrapper: the
// Cypher text travels inside a dollar quote, and the graph name is a '...'
// SQL literal escaped by quote doubling.
func TestWrapCypherSQL_TaggedDollarQuoteAndGraphName(t *testing.T) {
	got, ok := wrapCypherSQL("g'r$a/ph", "MATCH (n) RETURN n", "x agtype")
	if !ok {
		t.Fatal("wrapCypherSQL rejected a benign cypher body")
	}
	want := `SELECT * FROM ag_catalog.cypher('g''r$a/ph', $cq$ MATCH (n) RETURN n $cq$) AS (x agtype)`
	if got != want {
		t.Fatalf("wrapCypherSQL:\n got %q\nwant %q", got, want)
	}
}

// TestWrapCypherSQL_TagAvoidsBodyCollision: a Cypher body containing the
// default tag forces a different dollar-quote tag, so no value can close the
// SQL string early (#802). The tag is derived at assembly — reassembly
// attacks (e.g. "$v$cq$aelor$" under a single-pass strip) are impossible by
// construction.
func TestWrapCypherSQL_TagAvoidsBodyCollision(t *testing.T) {
	const cols = "x agtype"
	cypher := `MATCH (a) WHERE a.name = 'x$cq$y' RETURN a.name`
	got, ok := wrapCypherSQL("g", cypher, cols)
	if !ok {
		t.Fatal("wrapCypherSQL rejected a body needing only a non-default tag")
	}

	prefix := `SELECT * FROM ag_catalog.cypher('g', `
	rest := strings.TrimPrefix(got, prefix)
	if rest == got {
		t.Fatalf("wrapCypherSQL missing expected prefix: %q", got)
	}
	tag, _, ok := strings.Cut(rest, " ")
	if !ok || !strings.HasPrefix(tag, "$cq") || !strings.HasSuffix(tag, "$") {
		t.Fatalf("no dollar-quote tag after cypher( arg: %q", got)
	}
	if tag == "$cq$" {
		t.Fatalf("default tag used though the body contains it: %q", got)
	}
	if !strings.Contains(got, tag+" "+cypher+" "+tag) {
		t.Fatalf("body not wrapped in the chosen tag %q: %q", tag, got)
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
