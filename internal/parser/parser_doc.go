package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// extractDocComment looks at previous sibling nodes for comment blocks that
// form a documentation comment. It handles both // and /* */ style comments.
// Returns the doc comment text with leading comment markers stripped.
func extractDocComment(node *sitter.Node, source []byte) string {
	// Walk up to the nearest declaration-level parent if needed.
	// In Go, comments precede the declaration (function_declaration, type_declaration, etc.)
	// The node from the query may be the declaration itself, or its child.
	declNode := node
	if declNode.Parent() != nil {
		parentType := declNode.Parent().Type()
		if parentType == "source_file" || parentType == "block" || parentType == "module" ||
			parentType == "program" || parentType == "translation_unit" {
			// Already at top-level
		} else {
			// Check if parent is a declaration-level node
			declNode = declNode.Parent()
		}
	}

	// Collect consecutive comment siblings immediately preceding the declaration.
	var commentLines []string
	prev := declNode.PrevNamedSibling()
	for prev != nil && isCommentNode(prev) {
		text := prev.Content(source)
		commentLines = append([]string{text}, commentLines...)
		// Only include consecutive comments (no gap lines between them).
		if prev.EndPoint().Row+1 < declNode.StartPoint().Row {
			// Check if the gap is just blank lines before the first collected comment.
			if len(commentLines) == 1 {
				commentLines = nil
			}
			break
		}
		prev = prev.PrevNamedSibling()
	}

	if len(commentLines) == 0 {
		return ""
	}

	cleaned := make([]string, 0, len(commentLines))
	for _, line := range commentLines {
		cleaned = append(cleaned, stripCommentMarker(line))
	}
	return strings.Join(cleaned, "\n")
}

// stripCommentMarker removes leading comment syntax (// /* # ) from a single line.
func stripCommentMarker(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "//"):
		line = strings.TrimPrefix(line, "//")
		return strings.TrimPrefix(line, " ")
	case strings.HasPrefix(line, "/*") && strings.HasSuffix(line, "*/"):
		return strings.TrimSpace(line[2 : len(line)-2])
	case strings.HasPrefix(line, "#"):
		line = strings.TrimPrefix(line, "#")
		return strings.TrimPrefix(line, " ")
	default:
		return line
	}
}

// isCommentNode returns true if the node is a comment in any supported language.
func isCommentNode(node *sitter.Node) bool {
	t := node.Type()
	return t == "comment" || t == "line_comment" || t == "block_comment"
}
