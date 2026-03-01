package compare

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/gum"
)

func TestToGumTree_Go(t *testing.T) {
	source := []byte(`package main

func Foo() int {
	return 42
}
`)
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	gt := ToGumTree(tree.RootNode(), source)
	if gt == nil {
		t.Fatal("ToGumTree returned nil")
	}
	if gt.Type != "source_file" {
		t.Errorf("root type = %q, want %q", gt.Type, "source_file")
	}

	// Walk tree to find an identifier leaf with value "Foo".
	found := findGumValue(gt, "Foo")
	if !found {
		t.Error("expected to find leaf with value 'Foo'")
	}
}

func TestToGumTree_RoundTrip(t *testing.T) {
	srcA := []byte(`package main

func Hello(name string) string {
	return "Hello, " + name
}
`)
	srcB := []byte(`package main

func Hello(name string, greeting string) string {
	return greeting + ", " + name
}
`)
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	treeA, err := parser.ParseCtx(context.Background(), nil, srcA)
	if err != nil {
		t.Fatalf("parse srcA: %v", err)
	}
	treeB, err := parser.ParseCtx(context.Background(), nil, srcB)
	if err != nil {
		t.Fatalf("parse srcB: %v", err)
	}

	gtA := ToGumTree(treeA.RootNode(), srcA)
	gtB := ToGumTree(treeB.RootNode(), srcB)

	mappings := gum.Match(gtA, gtB)
	if len(mappings) == 0 {
		t.Error("expected non-empty mappings from gum.Match")
	}

	actions := gum.Patch(gtA, gtB, mappings)
	if len(actions) == 0 {
		t.Error("expected non-empty actions from gum.Patch for modified functions")
	}

	t.Logf("mappings: %d, actions: %d", len(mappings), len(actions))
	for i, a := range actions {
		if i >= 5 {
			t.Logf("  ... and %d more", len(actions)-5)
			break
		}
		t.Logf("  action[%d]: %s", i, a)
	}
}

// findGumValue walks a gum.Tree looking for a leaf with the given value.
func findGumValue(t *gum.Tree, value string) bool {
	if t.Value == value {
		return true
	}
	for _, child := range t.Children {
		if findGumValue(child, value) {
			return true
		}
	}
	return false
}
