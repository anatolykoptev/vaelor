package parser

import sitter "github.com/smacker/go-tree-sitter"

// typeAliasNodeTypes are the tree-sitter AST node types that unambiguously
// represent a type alias (as opposed to a struct/enum/interface definition).
// When ExpandSymbolKinds is ON, a KindType symbol whose capture node is one of
// these types is refined to KindTypeAlias.
//
//   - type_item (Rust): `type Foo = Bar;` — always a type alias.
//   - type_alias_declaration (TypeScript): `type Foo = ...` — always a type alias.
//   - alias_declaration (C++): `using Foo = Bar;` — always a type alias.
//   - type_definition (C/C++): `typedef OldType NewType;` — always a type alias.
//
// Go type aliases (`type Foo = Bar`) are NOT included here because the
// tree-sitter-go grammar does not distinguish `type Foo = Bar` (alias) from
// `type Foo Bar` (defined type) at the node-type level — both are `type_spec`.
// A follow-up can add Go-specific refinement if needed.
var typeAliasNodeTypes = map[string]bool{
	"type_item":              true, // Rust
	"type_alias_declaration": true, // TypeScript
	"alias_declaration":      true, // C++
	"type_definition":        true, // C / C++
}

// refineTypeAliasKind checks whether a KindType symbol's capture node is a
// type-alias AST node and returns KindTypeAlias if so. Returns the original
// kind otherwise. Called only when ParseOpts.ExpandSymbolKinds is true.
func refineTypeAliasKind(sym *Symbol, node *sitter.Node) NodeKind {
	if typeAliasNodeTypes[node.Type()] {
		return KindTypeAlias
	}
	return sym.Kind
}
