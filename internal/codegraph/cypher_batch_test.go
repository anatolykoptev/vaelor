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
