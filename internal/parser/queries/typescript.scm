; tree-sitter query for TypeScript/JavaScript symbol extraction.
; Used by internal/parser to extract functions, classes, interfaces, and imports.

; Function declarations.
(function_declaration
  name: (identifier) @symbol.name
  parameters: (formal_parameters) @symbol.params
  return_type: (type_annotation)? @symbol.result) @symbol.function

; Arrow function assignments at module level.
(lexical_declaration
  (variable_declarator
    name: (identifier) @symbol.name
    value: [(arrow_function) (function)] @symbol.body)) @symbol.function

; Class declarations.
(class_declaration
  name: (type_identifier) @symbol.name) @symbol.class

; Interface declarations (TypeScript).
(interface_declaration
  name: (type_identifier) @symbol.name) @symbol.interface

; Type alias declarations (TypeScript).
(type_alias_declaration
  name: (type_identifier) @symbol.name) @symbol.type

; Method definitions inside classes.
(method_definition
  name: (property_identifier) @symbol.name
  parameters: (formal_parameters) @symbol.params) @symbol.method

; Import declarations.
(import_statement
  source: (string) @import.path)

; Export + function.
(export_statement
  declaration: (function_declaration
    name: (identifier) @symbol.name)) @symbol.function
