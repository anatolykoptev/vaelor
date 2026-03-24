package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestExtractTestedByEdges_Go(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "ProcessOrder", Kind: parser.KindFunction, File: "order.go", Language: "go"},
		{Name: "TestProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Test_ProcessOrder_empty", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "BenchmarkProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Helper", Kind: parser.KindFunction, File: "order.go", Language: "go"},
	}

	edges := ExtractTestedByEdges("", symbols)

	found := map[string]bool{}
	for _, e := range edges {
		found[e.FromKey+"->"+e.ToKey] = true
	}
	if !found["TestProcessOrder:order_test.go->ProcessOrder:order.go"] {
		t.Error("missing TestProcessOrder -> ProcessOrder")
	}
	if !found["Test_ProcessOrder_empty:order_test.go->ProcessOrder:order.go"] {
		t.Error("missing Test_ProcessOrder_empty -> ProcessOrder")
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 edges, got %d", len(edges))
	}
}

func TestExtractTestedByEdges_Python(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "process_order", Kind: parser.KindFunction, File: "order.py", Language: "python"},
		{Name: "test_process_order", Kind: parser.KindFunction, File: "test_order.py", Language: "python"},
		{Name: "TestOrder", Kind: parser.KindType, File: "test_order.py", Language: "python"},
		{Name: "Order", Kind: parser.KindType, File: "order.py", Language: "python"},
	}

	edges := ExtractTestedByEdges("", symbols)
	if len(edges) < 1 {
		t.Fatal("expected at least 1 edge")
	}
}
