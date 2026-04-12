package codegraph

import (
	"strings"
	"testing"
)

func TestDiffGraphs_NewSymbols(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
			{Name: "Bar", File: "bar.go", Community: 0},
		},
	}

	d := DiffGraphs(old, new_)

	if len(d.AddedSymbols) != 1 {
		t.Fatalf("expected 1 added symbol, got %d", len(d.AddedSymbols))
	}
	if d.AddedSymbols[0].Name != "Bar" {
		t.Errorf("expected added symbol Bar, got %s", d.AddedSymbols[0].Name)
	}
	if len(d.RemovedSymbols) != 0 {
		t.Errorf("expected 0 removed symbols, got %d", len(d.RemovedSymbols))
	}
}

func TestDiffGraphs_RemovedSymbols(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
			{Name: "Dead", File: "dead.go", Community: 0},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
		},
	}

	d := DiffGraphs(old, new_)

	if len(d.RemovedSymbols) != 1 {
		t.Fatalf("expected 1 removed symbol, got %d", len(d.RemovedSymbols))
	}
	if d.RemovedSymbols[0].Name != "Dead" {
		t.Errorf("expected removed symbol Dead, got %s", d.RemovedSymbols[0].Name)
	}
	if len(d.AddedSymbols) != 0 {
		t.Errorf("expected 0 added symbols, got %d", len(d.AddedSymbols))
	}
}

func TestDiffGraphs_NewAndRemovedEdges(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go"},
			{Name: "Bar", File: "bar.go"},
		},
		Edges: []SnapshotEdge{
			{From: "Foo", Label: "CALLS", To: "Bar"},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go"},
			{Name: "Baz", File: "baz.go"},
		},
		Edges: []SnapshotEdge{
			{From: "Foo", Label: "CALLS", To: "Baz"},
		},
	}

	d := DiffGraphs(old, new_)

	if len(d.AddedEdges) != 1 {
		t.Fatalf("expected 1 added edge, got %d", len(d.AddedEdges))
	}
	if d.AddedEdges[0].To != "Baz" {
		t.Errorf("expected added edge to Baz, got %s", d.AddedEdges[0].To)
	}
	if len(d.RemovedEdges) != 1 {
		t.Fatalf("expected 1 removed edge, got %d", len(d.RemovedEdges))
	}
	if d.RemovedEdges[0].To != "Bar" {
		t.Errorf("expected removed edge to Bar, got %s", d.RemovedEdges[0].To)
	}
}

func TestDiffGraphs_CommunityMigrations(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
			{Name: "Bar", File: "bar.go", Community: 1},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Community: 0},
			{Name: "Bar", File: "bar.go", Community: 0},
		},
	}

	d := DiffGraphs(old, new_)

	if len(d.CommunityMigrations) != 1 {
		t.Fatalf("expected 1 community migration, got %d", len(d.CommunityMigrations))
	}
	m := d.CommunityMigrations[0]
	if m.Name != "Bar" {
		t.Errorf("expected migration for Bar, got %s", m.Name)
	}
	if m.OldCommunity != 1 || m.NewCommunity != 0 {
		t.Errorf("expected community 1→0, got %d→%d", m.OldCommunity, m.NewCommunity)
	}
}

func TestDiffGraphs_ComplexityChanges(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Complexity: 5},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go", Complexity: 15},
		},
	}

	d := DiffGraphs(old, new_)

	if len(d.ComplexityChanges) != 1 {
		t.Fatalf("expected 1 complexity change, got %d", len(d.ComplexityChanges))
	}
	c := d.ComplexityChanges[0]
	if c.Name != "Foo" {
		t.Errorf("expected complexity change for Foo, got %s", c.Name)
	}
	if c.OldComplexity != 5 || c.NewComplexity != 15 {
		t.Errorf("expected complexity 5→15, got %d→%d", c.OldComplexity, c.NewComplexity)
	}
}

func TestDiffGraphs_Summary(t *testing.T) {
	old := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go"},
		},
	}
	new_ := Snapshot{
		Symbols: []SnapshotSymbol{
			{Name: "Foo", File: "foo.go"},
			{Name: "Bar", File: "bar.go"},
		},
	}

	d := DiffGraphs(old, new_)

	if d.Summary == "" {
		t.Error("expected non-empty summary when changes exist")
	}
	if !strings.Contains(d.Summary, "1 new symbol") {
		t.Errorf("expected summary to mention '1 new symbol', got: %s", d.Summary)
	}
}

func TestDiffGraphs_EmptySnapshots(t *testing.T) {
	d := DiffGraphs(Snapshot{}, Snapshot{})

	if d.Summary != "no changes" {
		t.Errorf("expected 'no changes', got: %s", d.Summary)
	}
	if len(d.AddedSymbols) != 0 {
		t.Errorf("expected 0 added symbols, got %d", len(d.AddedSymbols))
	}
	if len(d.RemovedSymbols) != 0 {
		t.Errorf("expected 0 removed symbols, got %d", len(d.RemovedSymbols))
	}
	if len(d.AddedEdges) != 0 {
		t.Errorf("expected 0 added edges, got %d", len(d.AddedEdges))
	}
	if len(d.RemovedEdges) != 0 {
		t.Errorf("expected 0 removed edges, got %d", len(d.RemovedEdges))
	}
}
