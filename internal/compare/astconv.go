package compare

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/gum"
)

// ToGumTree converts a tree-sitter node tree into a gum.Tree for AST diffing.
// Sets Value on all leaf named nodes (language-agnostic).
// Calls Refresh() making it ready for gum.Match().
func ToGumTree(node *sitter.Node, source []byte) *gum.Tree {
	t := toGumTreeRec(node, source)
	t.Refresh()
	return t
}

func toGumTreeRec(node *sitter.Node, source []byte) *gum.Tree {
	var children []*gum.Tree
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil {
			children = append(children, toGumTreeRec(child, source))
		}
	}

	var value string
	if node.NamedChildCount() == 0 {
		value = string(source[node.StartByte():node.EndByte()])
	}

	return &gum.Tree{
		Type:     node.Type(),
		Value:    value,
		Children: children,
	}
}
