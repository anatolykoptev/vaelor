; tree-sitter query for Rust symbol extraction.
; Used by internal/parser to extract functions, types, methods, and imports.

; Top-level free functions (not inside impl).
(source_file
  (function_item
    name: (identifier) @symbol.name) @symbol.function)

; Free functions inside mod blocks.
(mod_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.function))

; Methods inside impl blocks.
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

; Const items.
(const_item
  name: (identifier) @symbol.name) @symbol.const

; Static items.
(static_item
  name: (identifier) @symbol.name) @symbol.var

; Use declarations (import paths).
(use_declaration
  argument: (_) @import.path)
