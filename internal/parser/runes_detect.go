package parser

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// runeKindMap is the canonical Svelte 5 rune list, sourced from
// packages/svelte/src/compiler/phases/2-analyze/visitors/CallExpression.js
// (the switch-case in CallExpression visitor), Svelte v5.x main branch as of 2026-04-16.
//
// Anti-patterns NOT in this map (do not add):
//   - $$slots, $$props, $$restProps — legacy Svelte 4 double-dollar variables, not runes.
//   - $.proxy, $.computed, $.user_effect, etc. — Svelte 5 internal helpers (start with "$.").
//   - $inspect.with — this is a CHAINED method on $inspect(value) results, not an
//     independent rune. On the AST level it surfaces as a separate call_expression
//     on the result of $inspect(); the inner $inspect(value) is correctly classified.
//
// Detection strategy: direct map lookup on the call target text. No prefix matching, no regex.
var runeKindMap = map[string]string{
	"$state":           "state",
	"$state.raw":       "state",
	"$state.eager":     "state",
	"$state.snapshot":  "state",
	"$derived":         "derived",
	"$derived.by":      "derived",
	"$effect":          "effect",
	"$effect.pre":      "effect",
	"$effect.tracking": "effect",
	"$effect.root":     "effect",
	"$effect.pending":  "effect",
	"$props":           "props",
	"$props.id":        "props",
	"$bindable":        "bindable",
	"$inspect":         "inspect",
	"$inspect.trace":   "inspect",
	"$host":            "host",
}

// walkRuneNodes recursively walks the AST and emits KindRune symbols for:
//   - variable_declarator whose value is a rune call_expression
//   - expression_statement whose expression is (or begins with) a rune call_expression,
//     including chained forms like $inspect(val).with(cb) where $inspect is the root call
func walkRuneNodes(node *sitter.Node, src []byte, out *[]*Symbol, path string) {
	switch node.Type() {
	case "variable_declarator":
		if sym := runeFromDeclarator(node, src, path); sym != nil {
			*out = append(*out, sym)
		}
	case "expression_statement":
		// Walk the expression chain rooted at child(0) to find the innermost
		// rune call_expression. Handles both direct calls ($effect(...)) and
		// chained calls ($inspect(val).with(cb)) where the rune is the chain root.
		if node.ChildCount() > 0 {
			if sym := runeFromExprChain(node.Child(0), src, path); sym != nil {
				*out = append(*out, sym)
			}
		}
	}
	for i := range int(node.ChildCount()) {
		walkRuneNodes(node.Child(i), src, out, path)
	}
}

// runeFromExprChain finds the innermost rune call_expression in a call chain.
// Handles:
//   - direct:   $effect(...)           → call_expression{function=identifier}
//   - chained:  $inspect(val).with(cb) → call_expression{function=member_expression{
//     object=call_expression{function=identifier("$inspect")}}}
//
// Returns the first (leftmost) rune call found by peeling the chain inward.
func runeFromExprChain(node *sitter.Node, src []byte, path string) *Symbol {
	if node == nil || node.Type() != "call_expression" {
		return nil
	}
	// Try this node first (handles direct rune call).
	if sym := runeFromCallExpr(node, src, path, ""); sym != nil {
		return sym
	}
	// If function is a member_expression, the object might be a rune call chain.
	// e.g. $inspect(val).with(cb): function=member_expression{object=$inspect(val)}
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil && funcNode.Type() == "member_expression" {
		obj := funcNode.ChildByFieldName("object")
		return runeFromExprChain(obj, src, path)
	}
	return nil
}

// innermostRuneCall peels a call chain to find the innermost call_expression that
// is a rune. Handles assignment-form chains like:
//
//	let stop = $inspect(count).with(callback);
//
// where the value is call_expression{function=member_expression{object=$inspect(count)}}.
// Returns the innermost rune call_expression node, or nil if none found.
func innermostRuneCall(node *sitter.Node) *sitter.Node {
	if node == nil || node.Type() != "call_expression" {
		return nil
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil && funcNode.Type() == "member_expression" {
		obj := funcNode.ChildByFieldName("object")
		if inner := innermostRuneCall(obj); inner != nil {
			return inner
		}
	}
	return node
}

// runeFromDeclarator attempts to build a KindRune symbol from a variable_declarator
// whose `value` child is a rune call_expression. Returns nil if not a rune.
//
// Handles three forms:
//   - Simple:       let count = $state(0)
//   - Chain:        let stop = $inspect(count).with(callback)
//   - Destructure:  let { name } = $props()
func runeFromDeclarator(node *sitter.Node, src []byte, path string) *Symbol {
	nameNode := node.ChildByFieldName("name")
	valueNode := node.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil {
		return nil
	}

	if nameNode.Type() == "identifier" {
		varName := nameNode.Content(src)
		innerCall := innermostRuneCall(valueNode)
		if innerCall == nil {
			return nil
		}
		return runeFromCallExpr(innerCall, src, path, varName)
	}

	// Destructuring pattern (object_pattern, array_pattern, etc.).
	// e.g. let { name = "anon", count } = $props();
	// Emit a single rune symbol with empty varName (falls back to "$props").
	// Anchor to the declarator's start line so the rune is locatable.
	innerCall := innermostRuneCall(valueNode)
	if innerCall == nil {
		return nil
	}
	sym := runeFromCallExpr(innerCall, src, path, "")
	if sym == nil {
		return nil
	}
	sym.StartLine = node.StartPoint().Row + 1
	sym.EndLine = node.EndPoint().Row + 1
	return sym
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
