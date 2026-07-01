package parser

import (
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// runeKindMap is the canonical Svelte 5 rune list, sourced from
// packages/svelte/src/compiler/phases/2-analyze/visitors/CallExpression.js
// (the switch-case in CallExpression visitor), Svelte v5.x main branch as of 2026-04-16.
//
// Anti-patterns NOT in this map (do not add):
//   - $$slots, $$props, $$restProps -- legacy Svelte 4 double-dollar variables, not runes.
//   - $.proxy, $.computed, $.user_effect, etc. -- Svelte 5 internal helpers (start with "$.").
//   - $inspect.with -- this is a CHAINED method on $inspect(value) results, not an
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
		runeFromDeclaratorAll(node, src, path, out)
	case "expression_statement":
		// Walk the expression chain rooted at child(0) to find the innermost
		// rune call_expression. Handles both direct calls ($effect(...)) and
		// chained calls ($inspect(val).with(cb)) where the rune is the chain root.
		//
		// The symbol Name is suffixed with ":L<line>" so that two $effect statements
		// in the same file produce distinct (repo_key, file_path, symbol_name) DB rows.
		// Format matches the secondary symbol emitted by runeFromDeclaratorAll so
		// consumers (symbol search, ILIKE %$effect%) see a consistent naming scheme.
		if node.ChildCount() > 0 {
			if sym := runeFromExprChain(node.Child(0), src, path); sym != nil {
				sym.Name = sym.Name + ":L" + strconv.FormatUint(uint64(sym.StartLine), 10)
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
//   - direct:   $effect(...)           -> call_expression{function=identifier}
//   - chained:  $inspect(val).with(cb) -> call_expression{function=member_expression{
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

// runeFromDeclaratorAll emits all KindRune symbols for a variable_declarator node.
//
// For bound identifiers (let x = $state(0)), two symbols are emitted:
//   - Name = varName  ("x")        -- allows lookup by variable name
//   - Name = "$state:L<line>"     -- allows lookup by rune token; line-disambiguated
//     so two $state bindings in the same file get distinct DB rows in the
//     (repo_key, file_path, symbol_name) PRIMARY KEY.
//
// Same line range and RuneKind for both. The rune-token name is the root rune
// (e.g. "$state" for "$state.raw", "$derived" for "$derived.by").
//
// For destructuring patterns (let { name, count } = $props()), the rune-token
// symbol (Name = "$props") is emitted AND one KindRune symbol per destructured
// binding name (Name = "name", "count") with the same RuneKind, so each prop is
// discoverable by its own identifier.
func runeFromDeclaratorAll(node *sitter.Node, src []byte, path string, out *[]*Symbol) {
	nameNode := node.ChildByFieldName("name")
	valueNode := node.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil {
		return
	}

	if nameNode.Type() == "identifier" {
		varName := nameNode.Content(src)
		innerCall := innermostRuneCall(valueNode)
		if innerCall == nil {
			return
		}
		// Primary symbol: name = variable name (e.g. "count").
		primary := runeFromCallExpr(innerCall, src, path, varName)
		if primary == nil {
			return
		}
		*out = append(*out, primary)

		// Secondary symbol: name = "$state:L<line>" (e.g. "$state:L7").
		// Line suffix disambiguates multiple rune bindings in the same file so each
		// gets a distinct (repo_key, file_path, symbol_name) DB row. Semantic search
		// (trigram similarity) and ILIKE %$state% both match the suffixed form.
		tokenName := runeTokenName(innerCall, src)
		if tokenName != "" && tokenName != varName {
			suffixedName := tokenName + ":L" + strconv.FormatUint(uint64(primary.StartLine), 10)
			secondary := &Symbol{
				Name:      suffixedName,
				Kind:      KindRune,
				RuneKind:  primary.RuneKind,
				Language:  primary.Language,
				File:      primary.File,
				StartLine: primary.StartLine,
				EndLine:   primary.EndLine,
			}
			*out = append(*out, secondary)
		}
		return
	}

	// Destructuring pattern (object_pattern, array_pattern, etc.).
	// e.g. let { name = "anon", count } = $props();
	innerCall := innermostRuneCall(valueNode)
	if innerCall == nil {
		return
	}
	// Token symbol with empty varName (falls back to "$props"), anchored to the
	// declarator start line so the rune is locatable. Existing behaviour.
	token := runeFromCallExpr(innerCall, src, path, "")
	if token == nil {
		return
	}
	token.StartLine = node.StartPoint().Row + 1
	token.EndLine = node.EndPoint().Row + 1
	*out = append(*out, token)

	// One KindRune symbol per destructured binding name, so each prop is
	// discoverable by its own identifier (e.g. searching for "count"). Same
	// RuneKind / Language / line range as the token symbol above.
	for _, bind := range destructuredBindingNames(nameNode, src) {
		named := runeFromCallExpr(innerCall, src, path, bind)
		if named == nil {
			continue
		}
		named.StartLine = token.StartLine
		named.EndLine = token.EndLine
		*out = append(*out, named)
	}
}

// destructuredBindingNames returns the bound identifier names of a destructuring
// pattern node (object_pattern / array_pattern). Node types are from the
// tree-sitter TypeScript grammar:
//
//	{ count }            -> shorthand_property_identifier_pattern      -> ["count"]
//	{ name = "anon" }    -> object_assignment_pattern{left: shorthand} -> ["name"]
//	{ key: alias }       -> pair_pattern{value: identifier}            -> ["alias"]
//	{ ...rest }          -> rest_pattern{identifier}                   -> ["rest"]
//
// Nested patterns (a pair_pattern whose value is itself a pattern) are traversed.
func destructuredBindingNames(pattern *sitter.Node, src []byte) []string {
	if pattern == nil {
		return nil
	}
	var names []string
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "shorthand_property_identifier_pattern", "identifier":
			names = append(names, n.Content(src))
		case "object_assignment_pattern":
			// { name = default } — the bound name is the left side.
			if left := n.ChildByFieldName("left"); left != nil {
				visit(left)
			}
		case "pair_pattern":
			// { key: value } — the bound name is the value (may be a nested pattern).
			if v := n.ChildByFieldName("value"); v != nil {
				visit(v)
			}
		case "rest_pattern":
			// { ...rest } — the bound name is the identifier child.
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if c := n.NamedChild(i); c.Type() == "identifier" {
					names = append(names, c.Content(src))
				}
			}
		case "object_pattern", "array_pattern":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				visit(n.NamedChild(i))
			}
		}
	}
	if pattern.Type() == "object_pattern" || pattern.Type() == "array_pattern" {
		for i := 0; i < int(pattern.NamedChildCount()); i++ {
			visit(pattern.NamedChild(i))
		}
	} else {
		visit(pattern)
	}
	return names
}

// runeTokenName derives the canonical rune token name (e.g. "$state") from the
// innermost rune call_expression node. Returns "" if the call is not a known rune.
// Normalises dotted variants: "$state.raw" -> "$state", "$derived.by" -> "$derived".
func runeTokenName(callNode *sitter.Node, src []byte) string {
	if callNode == nil || callNode.Type() != "call_expression" {
		return ""
	}
	funcNode := callNode.ChildByFieldName("function")
	if funcNode == nil {
		return ""
	}
	callText := funcNode.Content(src)
	if _, ok := runeKindMap[callText]; !ok {
		return ""
	}
	return "$" + strings.TrimPrefix(strings.SplitN(callText, ".", 2)[0], "$")
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
