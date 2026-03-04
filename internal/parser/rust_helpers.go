package parser

import sitter "github.com/smacker/go-tree-sitter"

// hasVisibilityModifier checks if a Rust node has a `pub` visibility modifier.
func hasVisibilityModifier(node *sitter.Node) bool {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child != nil && child.Type() == "visibility_modifier" {
			return true
		}
	}
	return false
}

// extractRustAttributes collects #[...] attribute items preceding a node.
func extractRustAttributes(node *sitter.Node, source []byte) []string {
	parent := node.Parent()
	if parent == nil {
		return nil
	}
	idx := nodeIndex(node, parent)
	var attrs []string
	for i := idx - 1; i >= 0; i-- {
		sib := parent.Child(i)
		if sib == nil {
			break
		}
		switch sib.Type() {
		case "attribute_item":
			attrs = append(attrs, sib.Content(source))
		case "line_comment", "block_comment":
			continue
		default:
			goto done
		}
	}
done:
	// Reverse to preserve source order.
	for i, j := 0, len(attrs)-1; i < j; i, j = i+1, j-1 {
		attrs[i], attrs[j] = attrs[j], attrs[i]
	}
	return attrs
}

// nodeIndex returns the index of node within parent's children.
func nodeIndex(node, parent *sitter.Node) int {
	count := int(parent.ChildCount())
	for i := range count {
		if parent.Child(i) == node {
			return i
		}
	}
	return -1
}

// implReceiver extracts the receiver type from a method's parent impl_item.
// Returns "Type" for plain impl, "Trait for Type" for trait impl.
func implReceiver(methodNode *sitter.Node, source []byte) string {
	declList := methodNode.Parent()
	if declList == nil || declList.Type() != "declaration_list" {
		return ""
	}
	implNode := declList.Parent()
	if implNode == nil || implNode.Type() != "impl_item" {
		return ""
	}
	typeNode := implNode.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	typeName := typeNode.Content(source)
	traitNode := implNode.ChildByFieldName("trait")
	if traitNode != nil {
		return traitNode.Content(source) + " for " + typeName
	}
	return typeName
}
