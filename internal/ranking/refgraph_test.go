package ranking

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestBuildRefGraph_CallEdges(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "/repo/cmd/main.go"},
		{Name: "Handler", Kind: parser.KindFunction, File: "/repo/internal/handler.go"},
		{Name: "helper", Kind: parser.KindFunction, File: "/repo/internal/helper.go"},
	}
	calls := []parser.CallSite{
		{Name: "Handler", File: "/repo/cmd/main.go", Line: 10},
		{Name: "helper", File: "/repo/internal/handler.go", Line: 5},
	}

	graph := BuildRefGraph(RefGraphInput{Symbols: symbols, Calls: calls})

	if graph.Weight("/repo/cmd/main.go", "/repo/internal/handler.go") == 0 {
		t.Error("expected edge main.go → handler.go from call")
	}
	if graph.Weight("/repo/internal/handler.go", "/repo/internal/helper.go") == 0 {
		t.Error("expected edge handler.go → helper.go from call")
	}
}

func TestBuildRefGraph_WeightDistribution(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "Do", Kind: parser.KindFunction, File: "/repo/a.go"},
		{Name: "Do", Kind: parser.KindFunction, File: "/repo/b.go"},
	}
	calls := []parser.CallSite{
		{Name: "Do", File: "/repo/caller.go", Line: 5},
	}

	graph := BuildRefGraph(RefGraphInput{Symbols: symbols, Calls: calls})

	wa := graph.Weight("/repo/caller.go", "/repo/a.go")
	wb := graph.Weight("/repo/caller.go", "/repo/b.go")
	if wa != 0.5 || wb != 0.5 {
		t.Errorf("expected 0.5 each for ambiguous def, got a=%f b=%f", wa, wb)
	}
}

func TestBuildRefGraph_SelfCallsExcluded(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "foo", Kind: parser.KindFunction, File: "/repo/a.go"},
	}
	calls := []parser.CallSite{{Name: "foo", File: "/repo/a.go", Line: 10}}

	graph := BuildRefGraph(RefGraphInput{Symbols: symbols, Calls: calls})

	if graph.Weight("/repo/a.go", "/repo/a.go") != 0 {
		t.Error("should not have self-edges")
	}
}

func TestBuildRefGraph_ImportEdges(t *testing.T) {
	t.Parallel()
	imports := map[string][]string{"/repo/a.go": {"/repo/b.go"}}
	graph := BuildRefGraph(RefGraphInput{ImportEdges: imports})

	if graph.Weight("/repo/a.go", "/repo/b.go") == 0 {
		t.Error("expected import edge a.go → b.go")
	}
}

func TestBuildRefGraph_MergedEdges(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "B", Kind: parser.KindFunction, File: "/repo/b.go"},
	}
	calls := []parser.CallSite{{Name: "B", File: "/repo/a.go", Line: 5}}
	imports := map[string][]string{"/repo/a.go": {"/repo/b.go"}}

	graph := BuildRefGraph(RefGraphInput{Symbols: symbols, Calls: calls, ImportEdges: imports})

	if graph.Weight("/repo/a.go", "/repo/b.go") < 2.0 {
		t.Error("expected merged weight >= 2.0 (call + import)")
	}
}

func TestBuildRefGraph_Empty(t *testing.T) {
	t.Parallel()
	graph := BuildRefGraph(RefGraphInput{})
	if graph.Len() != 0 {
		t.Errorf("expected empty graph, got %d edges", graph.Len())
	}
}
