package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestBuildGraphUsesEdges verifies that USES edges are emitted from templateFileRefs.
func TestBuildGraphUsesEdges(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/src/index.astro", RelPath: "src/index.astro", Language: "astro", Size: 100},
	}
	cg := &callgraph.CallGraph{}
	tplRefs := []templateFileRef{
		{relFile: "src/index.astro", resolvedTo: "src/components/Header.astro", line: 5},
		{relFile: "src/index.astro", resolvedTo: "src/components/Footer.astro", line: 8},
	}

	_, edges := buildGraph(root, files, []*parser.Symbol{}, cg, nil, nil, tplRefs)

	var usesEdges []edgeData
	for _, e := range edges {
		if e.EdgeLabel == "USES" {
			usesEdges = append(usesEdges, e)
		}
	}
	if len(usesEdges) != 2 {
		t.Fatalf("expected 2 USES edges, got %d", len(usesEdges))
	}
	for _, e := range usesEdges {
		if e.FromLabel != "File" || e.FromKey != "src/index.astro" {
			t.Errorf("unexpected USES edge from: %+v", e)
		}
		if e.ToLabel != "File" {
			t.Errorf("expected ToLabel=File on USES edge, got %q", e.ToLabel)
		}
		if e.ToKey == "" {
			t.Errorf("expected non-empty ToKey on USES edge")
		}
	}
}
