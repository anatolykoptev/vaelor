; tree-sitter query for PHP symbol extraction.
; Used by internal/parser to extract functions, classes, interfaces, traits,
; methods, constants, and imports from PHP source files.

; Top-level functions
(function_definition
  name: (name) @symbol.name) @symbol.function

; Class declarations
(class_declaration
  name: (name) @symbol.name) @symbol.class

; Interface declarations
(interface_declaration
  name: (name) @symbol.name) @symbol.interface

; Trait declarations (mapped to type — no dedicated captureKind for traits)
(trait_declaration
  name: (name) @symbol.name) @symbol.type

; Methods inside classes
(class_declaration
  body: (declaration_list
    (method_declaration
      name: (name) @symbol.name) @symbol.method))

; Methods inside interfaces
(interface_declaration
  body: (declaration_list
    (method_declaration
      name: (name) @symbol.name) @symbol.method))

; Methods inside traits
(trait_declaration
  body: (declaration_list
    (method_declaration
      name: (name) @symbol.name) @symbol.method))

; Constants
(const_declaration
  (const_element
    (name) @symbol.name) @symbol.const)

; Use/import statements
(namespace_use_declaration
  (namespace_use_clause) @import.path)
