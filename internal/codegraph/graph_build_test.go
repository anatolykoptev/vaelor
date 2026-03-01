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
	vertices, _ := buildGraph(root, files, symbols, cg, nil)

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
