package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestBuildGraphSymbolComplexityProps verifies that buildGraph adds complexity
// and lines properties to function/method Symbol vertices, but not to structs.
func TestBuildGraphSymbolComplexityProps(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
	}
	symbols := []*parser.Symbol{
		{
			Name: "Foo", Kind: parser.KindFunction,
			File: "/repo/main.go", StartLine: 1, EndLine: 5,
			Body: "if x { for y { } }",
		},
		{
			Name: "Bar", Kind: parser.KindStruct,
			File: "/repo/main.go", StartLine: 7, EndLine: 10,
		},
	}
	cg := &callgraph.CallGraph{}
	vertices, _ := buildGraph(root, files, symbols, cg, nil, nil)

	var fooFound, barFound bool
	for _, v := range vertices {
		if v.Label != "Symbol" {
			continue
		}
		switch v.Props["name"] {
		case "Foo":
			fooFound = true
			if v.Props["complexity"] == "" {
				t.Error("Foo: missing complexity prop")
			}
			if v.Props["lines"] != "5" {
				t.Errorf("Foo: lines = %q, want 5", v.Props["lines"])
			}
		case "Bar":
			barFound = true
			if _, ok := v.Props["complexity"]; ok {
				t.Error("Bar (struct): should not have complexity prop")
			}
			if _, ok := v.Props["lines"]; ok {
				t.Error("Bar (struct): should not have lines prop")
			}
		}
	}
	if !fooFound {
		t.Error("Foo symbol vertex not found")
	}
	if !barFound {
		t.Error("Bar symbol vertex not found")
	}
}

// TestBuildGraphInheritsEdges verifies that buildGraph creates INHERITS edges
// from type relationships when both subject and target are known symbols.
func TestBuildGraphInheritsEdges(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
	}
	symbols := []*parser.Symbol{
		{Name: "Reader", Kind: parser.KindInterface, File: "/repo/main.go", StartLine: 1, EndLine: 5},
		{Name: "MyReader", Kind: parser.KindStruct, File: "/repo/main.go", StartLine: 7, EndLine: 15},
	}
	rels := []parser.TypeRelationship{
		{Subject: "MyReader", Target: "Reader", Kind: parser.RelEmbeds, Line: 8, File: "/repo/main.go"},
	}
	cg := &callgraph.CallGraph{}
	_, edges := buildGraph(root, files, symbols, cg, nil, rels)

	found := false
	for _, e := range edges {
		if e.EdgeLabel == "INHERITS" && e.FromKey == "MyReader:main.go" && e.ToKey == "Reader:main.go" {
			found = true
		}
	}
	if !found {
		t.Error("missing INHERITS edge: MyReader -> Reader")
	}
}

// TestBuildGraphInheritsEdgesExternalTarget verifies that INHERITS edges are
// skipped when the target type is not a known symbol in the repository.
func TestBuildGraphInheritsEdgesExternalTarget(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
	}
	symbols := []*parser.Symbol{
		{Name: "MyReader", Kind: parser.KindStruct, File: "/repo/main.go", StartLine: 7, EndLine: 15},
	}
	rels := []parser.TypeRelationship{
		{Subject: "MyReader", Target: "ExternalInterface", Kind: parser.RelImplements, Line: 8, File: "/repo/main.go"},
	}
	cg := &callgraph.CallGraph{}
	_, edges := buildGraph(root, files, symbols, cg, nil, rels)

	for _, e := range edges {
		if e.EdgeLabel == "INHERITS" || e.EdgeLabel == "IMPLEMENTS" {
			t.Errorf("unexpected %s edge: %s -> %s (target should be external)", e.EdgeLabel, e.FromKey, e.ToKey)
		}
	}
}
