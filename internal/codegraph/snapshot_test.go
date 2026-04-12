package codegraph

import (
	"encoding/json"
	"testing"
)

// TestSnapshotSymbol_RoundTrip verifies that marshalling and unmarshalling
// []SnapshotSymbol preserves all fields, including the zero-value omitempty field.
func TestSnapshotSymbol_RoundTrip(t *testing.T) {
	t.Parallel()

	input := []SnapshotSymbol{
		{Name: "ParseFile", Kind: "function", File: "parser/parse.go", Community: 3, Complexity: 12},
		{Name: "Config", Kind: "type", File: "config/config.go", Community: 1},
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got []SnapshotSymbol
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got) != len(input) {
		t.Fatalf("got %d symbols, want %d", len(got), len(input))
	}

	for i, want := range input {
		g := got[i]
		if g.Name != want.Name || g.Kind != want.Kind || g.File != want.File ||
			g.Community != want.Community || g.Complexity != want.Complexity {
			t.Errorf("[%d] got %+v, want %+v", i, g, want)
		}
	}
}

// TestSnapshotEdge_RoundTrip verifies that marshalling and unmarshalling
// []SnapshotEdge preserves all fields.
func TestSnapshotEdge_RoundTrip(t *testing.T) {
	t.Parallel()

	input := []SnapshotEdge{
		{From: "ParseFile:parser/parse.go", To: "Tokenize:lexer/lexer.go", Label: "CALLS"},
		{From: "Animal:model/animal.go", To: "Living:model/living.go", Label: "INHERITS"},
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got []SnapshotEdge
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got) != len(input) {
		t.Fatalf("got %d edges, want %d", len(got), len(input))
	}
	for i, want := range input {
		g := got[i]
		if g.From != want.From || g.To != want.To || g.Label != want.Label {
			t.Errorf("[%d] got {%q %q %q}, want {%q %q %q}",
				i, g.From, g.To, g.Label, want.From, want.To, want.Label)
		}
	}
}

// TestBuildSnapshot_FromVerticesEdges verifies that buildSnapshot correctly:
// - includes only Symbol vertices (not File, Package)
// - includes only semantic edges (CALLS, etc.), not CONTAINS edges
func TestBuildSnapshot_FromVerticesEdges(t *testing.T) {
	t.Parallel()

	vertices := []vertexData{
		{
			Label: "Symbol",
			Props: map[string]string{
				"name":       "ParseFile",
				"kind":       "function",
				"file":       "parser/parse.go",
				"community":  "2",
				"complexity": "7",
			},
		},
		{
			Label: "Symbol",
			Props: map[string]string{
				"name":      "Config",
				"kind":      "type",
				"file":      "config/config.go",
				"community": "1",
			},
		},
		{
			Label: "File",
			Props: map[string]string{
				"path":     "parser/parse.go",
				"language": "go",
			},
		},
		{
			Label: "Package",
			Props: map[string]string{
				"name": "parser",
				"path": "parser",
			},
		},
	}

	edges := []edgeData{
		{
			FromLabel: "Symbol",
			FromKey:   "ParseFile:parser/parse.go",
			ToLabel:   "Symbol",
			ToKey:     "Config:config/config.go",
			EdgeLabel: "CALLS",
		},
		{
			FromLabel: "File",
			FromKey:   "parser/parse.go",
			ToLabel:   "Symbol",
			ToKey:     "ParseFile:parser/parse.go",
			EdgeLabel: "CONTAINS",
		},
		{
			FromLabel: "Package",
			FromKey:   "parser",
			ToLabel:   "File",
			ToKey:     "parser/parse.go",
			EdgeLabel: "CONTAINS",
		},
	}

	snap := buildSnapshot(vertices, edges)

	// Only Symbol vertices should appear.
	if len(snap.Symbols) != 2 {
		t.Fatalf("got %d symbols, want 2", len(snap.Symbols))
	}

	// Verify symbol fields.
	symByName := make(map[string]SnapshotSymbol)
	for _, s := range snap.Symbols {
		symByName[s.Name] = s
	}

	parseFile, ok := symByName["ParseFile"]
	if !ok {
		t.Fatal("ParseFile symbol not found")
	}
	if parseFile.Kind != "function" {
		t.Errorf("ParseFile.Kind: got %q, want %q", parseFile.Kind, "function")
	}
	if parseFile.File != "parser/parse.go" {
		t.Errorf("ParseFile.File: got %q, want %q", parseFile.File, "parser/parse.go")
	}
	if parseFile.Community != 2 {
		t.Errorf("ParseFile.Community: got %d, want 2", parseFile.Community)
	}
	if parseFile.Complexity != 7 {
		t.Errorf("ParseFile.Complexity: got %d, want 7", parseFile.Complexity)
	}

	cfg, ok := symByName["Config"]
	if !ok {
		t.Fatal("Config symbol not found")
	}
	if cfg.Complexity != 0 {
		t.Errorf("Config.Complexity: got %d, want 0 (omitempty)", cfg.Complexity)
	}

	// Only the CALLS edge should appear; CONTAINS edges must be filtered out.
	if len(snap.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(snap.Edges))
	}
	e := snap.Edges[0]
	if e.Label != "CALLS" {
		t.Errorf("edge label: got %q, want %q", e.Label, "CALLS")
	}
	if e.From != "ParseFile:parser/parse.go" {
		t.Errorf("edge From: got %q, want %q", e.From, "ParseFile:parser/parse.go")
	}
	if e.To != "Config:config/config.go" {
		t.Errorf("edge To: got %q, want %q", e.To, "Config:config/config.go")
	}
}
