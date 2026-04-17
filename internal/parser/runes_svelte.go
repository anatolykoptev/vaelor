package parser

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// runeKindMap maps canonical rune call texts (as they appear in source, including
// dotted variants) to their RuneKind category string.
var runeKindMap = map[string]string{
	"$state":        "state",
	"$state.raw":    "state",
	"$derived":      "derived",
	"$derived.by":   "derived",
	"$effect":       "effect",
	"$effect.pre":   "effect",
	"$effect.root":  "effect",
	"$props":        "props",
	"$props.id":     "props",
	"$bindable":     "bindable",
	"$inspect":      "inspect",
	"$inspect.with": "inspect",
}

// appendRuneSymbols walks vs.Code parsed with the TS grammar, finds rune call
// expressions, synthesizes KindRune symbols (with virtual line numbers), remaps
// them to original-file coordinates, and appends them to result.Symbols.
//
// Called from svelteHandler.Parse after parseWithTSAndRemap.
func appendRuneSymbols(result *ParseResult, vs *preproc.VirtualSource, path string) {
	if vs == nil || len(vs.Code) == 0 {
		return
	}
	caps := tsLang.Capabilities()
	if caps.SitterLanguage == nil {
		return
	}

	ps := sitter.NewParser()
	defer ps.Close()
	ps.SetLanguage(caps.SitterLanguage)

	tree, err := ps.ParseCtx(context.Background(), nil, vs.Code)
	if err != nil {
		return
	}
	defer tree.Close()

	var syms []*Symbol
	walkRuneNodes(tree.RootNode(), vs.Code, &syms, path)

	// Remap virtual line numbers to original .svelte coordinates.
	for _, sym := range syms {
		origStart := virtualToOriginal(vs.LineMap, sym.StartLine)
		if origStart == 0 {
			continue // on padding — drop
		}
		origEnd := virtualToOriginal(vs.LineMap, sym.EndLine)
		if origEnd == 0 {
			origEnd = origStart
		}
		sym.StartLine = origStart
		sym.EndLine = origEnd
		sym.Language = vs.Lang
		result.Symbols = append(result.Symbols, sym)
	}
}

// walkRuneNodes recursively walks the AST and emits KindRune symbols for:
//   - variable_declarator whose value is a rune call_expression
//   - expression_statement (or expression_statement > await_expression) that is
//     a standalone rune call_expression
func walkRuneNodes(node *sitter.Node, src []byte, out *[]*Symbol, path string) {
	switch node.Type() {
	case "variable_declarator":
		if sym := runeFromDeclarator(node, src, path); sym != nil {
			*out = append(*out, sym)
		}
	case "expression_statement":
		// expression_statement > call_expression (standalone rune call)
		if node.ChildCount() > 0 {
			child := node.Child(0)
			if sym := runeFromCallExpr(child, src, path, ""); sym != nil {
				*out = append(*out, sym)
			}
		}
	}
	for i := range int(node.ChildCount()) {
		walkRuneNodes(node.Child(i), src, out, path)
	}
}

// runeFromDeclarator attempts to build a KindRune symbol from a variable_declarator
// whose `value` child is a rune call_expression. Returns nil if not a rune.
func runeFromDeclarator(node *sitter.Node, src []byte, path string) *Symbol {
	nameNode := node.ChildByFieldName("name")
	valueNode := node.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil {
		return nil
	}
	// Only handle simple identifier names (not destructuring).
	if nameNode.Type() != "identifier" {
		return nil
	}
	varName := nameNode.Content(src)
	return runeFromCallExpr(valueNode, src, path, varName)
}

// runeFromCallExpr returns a KindRune symbol if callNode is a rune call_expression.
// varName is the bound variable name (empty for standalone expressions).
func runeFromCallExpr(callNode *sitter.Node, src []byte, path string, varName string) *Symbol {
	if callNode == nil || callNode.Type() != "call_expression" {
		return nil
	}
	funcNode := callNode.ChildByFieldName("function")
	if funcNode == nil {
		return nil
	}
	callText := funcNode.Content(src)
	runeKind, ok := runeKindMap[callText]
	if !ok {
		return nil
	}

	// For standalone calls with no variable binding, use the rune name as symbol name.
	name := varName
	if name == "" {
		// Strip the leading $ for the synthetic name base; keep $ in Name for clarity.
		name = "$" + strings.TrimPrefix(strings.SplitN(callText, ".", 2)[0], "$")
	}

	return &Symbol{
		Name:      name,
		Kind:      KindRune,
		RuneKind:  runeKind,
		Language:  "svelte",
		File:      path,
		StartLine: callNode.StartPoint().Row + 1,
		EndLine:   callNode.EndPoint().Row + 1,
	}
}
