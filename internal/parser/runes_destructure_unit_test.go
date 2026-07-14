package parser

import (
	"slices"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

// firstDestructurePattern parses code with the TS grammar and returns the first
// object_pattern / array_pattern node (the destructuring target of a declarator).
func firstDestructurePattern(t *testing.T, code string) (*sitter.Node, []byte) {
	t.Helper()
	src := []byte(code)
	caps := tsLang.Capabilities()
	if caps.SitterLanguage == nil {
		t.Fatal("tsLang has no sitter language")
	}
	root, closeFn, err := parseTree(caps.SitterLanguage, src, nil)
	if err != nil {
		t.Fatalf("parse %q: %v", code, err)
	}
	t.Cleanup(closeFn)

	var pat *sitter.Node
	var find func(n *sitter.Node)
	find = func(n *sitter.Node) {
		if pat != nil {
			return
		}
		if n.Type() == "object_pattern" || n.Type() == "array_pattern" {
			pat = n
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			find(n.Child(i))
		}
	}
	find(root)
	return pat, src
}

// TestDestructuredBindingNames exercises every destructuring node shape directly,
// so the helper's behaviour matrix is proven independent of the $props()
// integration path. The "rename+default" and "array default" cases route through
// an assignment_pattern node and were the silent-drop bug (review HIGH).
func TestDestructuredBindingNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code string
		want []string
	}{
		{"shorthand", "let { a, b } = x;", []string{"a", "b"}},
		{"shorthand + default", "let { a = 1, b } = x;", []string{"a", "b"}},
		{"rename", "let { k: alias } = x;", []string{"alias"}},
		{"rename + default", "let { k: n = 1 } = x;", []string{"n"}},
		{"rest", "let { a, ...rest } = x;", []string{"a", "rest"}},
		{"array + default", "let [a = 1, b] = x;", []string{"a", "b"}},
		{"nested object", "let { outer: { inner } } = x;", []string{"inner"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pat, src := firstDestructurePattern(t, c.code)
			if pat == nil {
				t.Fatalf("no destructuring pattern found in %q", c.code)
			}
			got := destructuredBindingNames(pat, src)
			slices.Sort(got)
			want := slices.Clone(c.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("destructuredBindingNames(%q) = %v, want %v", c.code, got, want)
			}
		})
	}
}
