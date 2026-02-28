package codegraph

import (
	"strings"
	"testing"
	"time"
)

// TestIsFresh verifies the freshness check against TTL.
func TestIsFresh(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		builtAt    time.Time
		ttlSeconds int
		want       bool
	}{
		{
			name:       "fresh: just built",
			builtAt:    time.Now().Add(-10 * time.Second),
			ttlSeconds: 3600,
			want:       true,
		},
		{
			name:       "stale: well past ttl",
			builtAt:    time.Now().Add(-2 * time.Hour),
			ttlSeconds: 3600,
			want:       false,
		},
		{
			name:       "boundary: exactly at ttl is stale",
			builtAt:    time.Now().Add(-time.Hour),
			ttlSeconds: 3600,
			want:       false,
		},
		{
			name:       "zero ttl: always stale",
			builtAt:    time.Now(),
			ttlSeconds: 0,
			want:       false,
		},
		{
			name:       "negative ttl: always stale",
			builtAt:    time.Now(),
			ttlSeconds: -1,
			want:       false,
		},
		{
			name:       "zero time: stale",
			builtAt:    time.Time{},
			ttlSeconds: 3600,
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isFresh(tc.builtAt, tc.ttlSeconds)
			if got != tc.want {
				t.Errorf("isFresh(%v, %d) = %v; want %v", tc.builtAt, tc.ttlSeconds, got, tc.want)
			}
		})
	}
}

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

	// Must contain MERGE statements.
	if !strings.Contains(cypher, "MERGE") {
		t.Error("expected MERGE in vertex batch Cypher")
	}

	// Must contain vertex labels.
	if !strings.Contains(cypher, "Symbol") {
		t.Error("expected 'Symbol' label in vertex batch Cypher")
	}

	// Must contain the symbol names.
	if !strings.Contains(cypher, "ParseFile") {
		t.Error("expected 'ParseFile' in vertex batch Cypher")
	}

	if !strings.Contains(cypher, "BuildCallGraph") {
		t.Error("expected 'BuildCallGraph' in vertex batch Cypher")
	}

	// Must contain ON CREATE SET / ON MATCH SET since props are non-empty.
	if !strings.Contains(cypher, "ON CREATE SET") {
		t.Error("expected 'ON CREATE SET' in vertex batch Cypher")
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

	// Must have MATCH for endpoint lookup.
	if !strings.Contains(cypher, "MATCH") {
		t.Error("expected MATCH in edge batch Cypher")
	}

	// Must have MERGE for edge creation.
	if !strings.Contains(cypher, "MERGE") {
		t.Error("expected MERGE in edge batch Cypher")
	}

	// Must contain edge labels.
	if !strings.Contains(cypher, "CONTAINS") {
		t.Error("expected 'CONTAINS' edge label in Cypher")
	}

	if !strings.Contains(cypher, "CALLS") {
		t.Error("expected 'CALLS' edge label in Cypher")
	}

	// Must reference symbol names.
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

// TestRelPath verifies relPath behavior for various input combinations.
func TestRelPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		abs  string
		root string
		want string
	}{
		{
			name: "absolute path with root prefix",
			abs:  "/home/krolik/src/go-code/internal/parser/parser.go",
			root: "/home/krolik/src/go-code",
			want: "internal/parser/parser.go",
		},
		{
			name: "absolute path without root prefix",
			abs:  "/tmp/other/file.go",
			root: "/home/krolik/src/go-code",
			want: "../../../../tmp/other/file.go",
		},
		{
			name: "empty root returns abs unchanged",
			abs:  "/some/absolute/path.go",
			root: "",
			want: "/some/absolute/path.go",
		},
		{
			name: "already relative path with root",
			abs:  "/home/krolik/src/go-code/cmd/main.go",
			root: "/home/krolik/src/go-code",
			want: "cmd/main.go",
		},
		{
			name: "root equal to abs directory",
			abs:  "/home/krolik/src/go-code",
			root: "/home/krolik/src/go-code",
			want: ".",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := relPath(tc.abs, tc.root)
			if got != tc.want {
				t.Errorf("relPath(%q, %q) = %q; want %q", tc.abs, tc.root, got, tc.want)
			}
		})
	}
}
