package review

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestChangedSymbols(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 5, EndLine: 15},
		{Name: "Bar", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 20, EndLine: 30},
		{Name: "Baz", Kind: parser.KindFunction, File: "/repo/util.go", StartLine: 1, EndLine: 10},
	}
	diffs := []FileDiff{
		{Path: "main.go", LineRanges: []LineRange{{10, 12}}},
		{Path: "other.go", LineRanges: []LineRange{{1, 5}}},
	}

	changed := ChangedSymbols(symbols, diffs, "/repo")
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed symbol, got %d", len(changed))
	}
	if changed[0].Symbol.Name != "Foo" {
		t.Errorf("expected Foo, got %s", changed[0].Symbol.Name)
	}
	if changed[0].ChangeType != ChangeModified {
		t.Errorf("expected modified, got %s", changed[0].ChangeType)
	}
}

func TestChangedSymbolsNewFile(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "New", Kind: parser.KindFunction, File: "/repo/new.go", StartLine: 1, EndLine: 10},
	}
	diffs := []FileDiff{
		{Path: "new.go", Added: 10, Removed: 0, LineRanges: []LineRange{{1, 10}}},
	}

	changed := ChangedSymbols(symbols, diffs, "/repo")
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed symbol, got %d", len(changed))
	}
	if changed[0].ChangeType != ChangeAdded {
		t.Errorf("expected added, got %s", changed[0].ChangeType)
	}
}
