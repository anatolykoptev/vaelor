package codegraph

import (
	"strings"
	"testing"
)

// TestBuildVertexBatch verifies that buildVertexBatch generates valid Cypher
// containing MERGE statements and vertex property names.
func TestBuildVertexBatch(t *testing.T) {
	t.Parallel()

	vertices := []vertexData{
		{
			Label: "Symbol",
			Props: map[string]string{
				"name":       "ParseFile",
				"kind":       "function",
				"file":       "internal/parser/parser.go",
				"start_line": "100",
				"end_line":   "139",
				"signature":  "func ParseFile(path string, source []byte, opts ParseOpts) (*ParseResult, error)",
			},
		},
		{
			Label: "Symbol",
			Props: map[string]string{
				"name":       "BuildCallGraph",
				"kind":       "function",
				"file":       "internal/callgraph/graph.go",
				"start_line": "27",
				"end_line":   "46",
				"signature":  "func BuildCallGraph(symbols []*parser.Symbol, calls []parser.CallSite) *CallGraph",
			},
		},
	}

	cypher := buildVertexBatch("code_test", vertices)

	if cypher == "" {
		t.Fatal("buildVertexBatch returned empty string")
	}

	if !strings.Contains(cypher, "MERGE") {
		t.Error("expected MERGE in vertex batch Cypher")
	}

	if !strings.Contains(cypher, "Symbol") {
		t.Error("expected 'Symbol' label in vertex batch Cypher")
	}

	if !strings.Contains(cypher, "ParseFile") {
		t.Error("expected 'ParseFile' in vertex batch Cypher")
	}

	if !strings.Contains(cypher, "BuildCallGraph") {
		t.Error("expected 'BuildCallGraph' in vertex batch Cypher")
	}

	// AGE uses plain SET, not ON CREATE/MATCH SET.
	if !strings.Contains(cypher, "SET ") {
		t.Error("expected 'SET' in vertex batch Cypher")
	}
}

// TestBuildEdgeBatch verifies that buildEdgeBatch generates valid Cypher
// with MATCH and MERGE for both CONTAINS and CALLS edges.
func TestBuildEdgeBatch(t *testing.T) {
	t.Parallel()

	edges := []edgeData{
		{
			FromLabel: "File",
			FromKey:   "internal/parser/parser.go",
			ToLabel:   "Symbol",
			ToKey:     "ParseFile:internal/parser/parser.go",
			EdgeLabel: "CONTAINS",
			Props:     map[string]string{},
		},
		{
			FromLabel: "Symbol",
			FromKey:   "TraceRepo:internal/callgraph/repo.go",
			ToLabel:   "Symbol",
			ToKey:     "BuildCallGraph:internal/callgraph/graph.go",
			EdgeLabel: "CALLS",
			Props:     map[string]string{"line": "56"},
		},
	}

	cypher := buildEdgeBatch("code_test", edges)

	if cypher == "" {
		t.Fatal("buildEdgeBatch returned empty string")
	}

	if !strings.Contains(cypher, "MATCH") {
		t.Error("expected MATCH in edge batch Cypher")
	}

	if !strings.Contains(cypher, "MERGE") {
		t.Error("expected MERGE in edge batch Cypher")
	}

	if !strings.Contains(cypher, "CONTAINS") {
		t.Error("expected 'CONTAINS' edge label in Cypher")
	}

	if !strings.Contains(cypher, "CALLS") {
		t.Error("expected 'CALLS' edge label in Cypher")
	}

	if !strings.Contains(cypher, "ParseFile") {
		t.Error("expected 'ParseFile' in edge batch Cypher")
	}

	if !strings.Contains(cypher, "TraceRepo") {
		t.Error("expected 'TraceRepo' in edge batch Cypher")
	}
}

// TestBuildVertexBatchEmpty verifies that an empty batch returns an empty string.
func TestBuildVertexBatchEmpty(t *testing.T) {
	t.Parallel()
	got := buildVertexBatch("code_test", nil)
	if got != "" {
		t.Errorf("expected empty string for nil vertices, got %q", got)
	}
}

// TestBuildEdgeBatchEmpty verifies that an empty batch returns an empty string.
func TestBuildEdgeBatchEmpty(t *testing.T) {
	t.Parallel()
	got := buildEdgeBatch("code_test", nil)
	if got != "" {
		t.Errorf("expected empty string for nil edges, got %q", got)
	}
}

// TestBuildEdgeUnwindBatch_ColonPathIntact is the red→green proof for the actual
// bug scenario (CG-T4): a Route edge whose path contains a colon (e.g. the
// colon-in-path WebRTC path /peer1:unknown) must keep its full path through
// buildEdgeUnwindBatch — the LIVE production edge-insert path.
//
// matchKey / splitCompositeKey tests only cover helper functions. buildEdgeUnwindBatch
// is the path that actually emits the UNWIND Cypher AGE executes; if its Go
// pre-split broke on a colon path, no prior test would have caught it.
//
// Under the old ':' delimiter design this would have been fragile: splitting
// "GET:/peer1:unknown" on ':' with SplitN(..., 2) still yields ["GET", "/peer1:unknown"]
// which is correct, but once the code was changed to use '\x00' the pre-split
// must use the new delimiter exclusively. This test proves the live path is
// colon-safe: the full path reaches the UNWIND map and no '\x00' leaks into
// the emitted Cypher.
func TestBuildEdgeUnwindBatch_ColonPathIntact(t *testing.T) {
	t.Parallel()

	// HANDLES edge: Symbol (setupRoutes in src/routes.ts) → Route (GET /peer1:unknown).
	// ToKey uses compositeKeyDelim — the actual delimiter used in production.
	edges := []edgeData{
		{
			FromLabel: "Symbol",
			FromKey:   "setupRoutes" + compositeKeyDelim + "src/routes.ts",
			ToLabel:   "Route",
			ToKey:     "GET" + compositeKeyDelim + "/peer1:unknown",
			EdgeLabel: "HANDLES",
			Props:     map[string]string{},
		},
	}

	cypher := buildEdgeUnwindBatch("code_test", edges)

	if cypher == "" {
		t.Fatal("buildEdgeUnwindBatch returned empty string for a non-empty edge slice")
	}

	// 1. The full path "/peer1:unknown" must appear intact — not truncated to "/peer1".
	if !strings.Contains(cypher, "/peer1:unknown") {
		t.Errorf("full path '/peer1:unknown' not found in emitted Cypher — colon truncation bug:\n%s", cypher)
	}

	// 2. The method "GET" must appear as the Route method field (tm).
	if !strings.Contains(cypher, "tm: 'GET'") {
		t.Errorf("Route method field 'tm: \\'GET\\'' missing in emitted Cypher:\n%s", cypher)
	}

	// 3. The path must land in field tp (Route ToKey pre-split).
	if !strings.Contains(cypher, "tp: '/peer1:unknown'") {
		t.Errorf("Route path field 'tp: \\'/peer1:unknown\\'' missing in emitted Cypher:\n%s", cypher)
	}

	// 4. No literal '\x00' byte may appear in the emitted Cypher — the delimiter
	//    must have been split away in Go before Cypher is built.
	if strings.Contains(cypher, "\x00") {
		t.Errorf("compositeKeyDelim (\\x00) leaked into emitted Cypher:\n%q", cypher)
	}

	// 5. The Symbol FromKey fields must also be correctly split (fn/ff).
	if !strings.Contains(cypher, "fn: 'setupRoutes'") {
		t.Errorf("Symbol name field 'fn: \\'setupRoutes\\'' missing in emitted Cypher:\n%s", cypher)
	}
	if !strings.Contains(cypher, "ff: 'src/routes.ts'") {
		t.Errorf("Symbol file field 'ff: \\'src/routes.ts\\'' missing in emitted Cypher:\n%s", cypher)
	}
}
