; tree-sitter query for Rust symbol extraction.
; Extracts functions, methods (with impl context), types, traits, and imports.

; Top-level free functions (not inside impl).
(source_file
  (function_item
    name: (identifier) @symbol.name) @symbol.function)

; Free functions inside mod blocks.
(mod_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.function))

; Methods inside plain impl blocks: impl Type { fn ... }
(impl_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.method))

; Struct definitions.
(struct_item
  name: (type_identifier) @symbol.name) @symbol.type

; Enum definitions.
(enum_item
  name: (type_identifier) @symbol.name) @symbol.type

; Trait definitions.
(trait_item
  name: (type_identifier) @symbol.name) @symbol.interface

; Type alias definitions.
(type_item
  name: (type_identifier) @symbol.name) @symbol.type

; Macro rules definitions (#664: macro kind).
(macro_definition
  name: (identifier) @symbol.name) @symbol.macro

; Module declarations (#664: module kind). Captures both `mod foo;` and
; `mod foo { ... }`. Functions inside mod blocks are already captured by the
; function_item patterns above; this captures the module declaration itself.
(mod_item
  name: (identifier) @symbol.name) @symbol.module

; Const items.
(const_item
  name: (identifier) @symbol.name) @symbol.const

; Static items.
(static_item
  name: (identifier) @symbol.name) @symbol.var

; Use declarations (import paths).
(use_declaration
  argument: (_) @import.path)
