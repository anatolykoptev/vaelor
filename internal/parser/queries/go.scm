; tree-sitter query for Go symbol extraction.
; Used by internal/parser to extract functions, types, methods, and imports.

; Top-level function declarations.
; Captures: name = function name, params = parameter list, result = return type.
(function_declaration
  name: (identifier) @symbol.name
  parameters: (parameter_list) @symbol.params
  result: (_)? @symbol.result) @symbol.function

; Method declarations (receiver functions).
; Captures: receiver type, name, params, result.
(method_declaration
  receiver: (parameter_list) @symbol.receiver
  name: (field_identifier) @symbol.name
  parameters: (parameter_list) @symbol.params
  result: (_)? @symbol.result) @symbol.method

; Type declarations (type Foo struct / type Bar interface / type Baz = int).
(type_declaration
  (type_spec
    name: (type_identifier) @symbol.name
    type: (_) @symbol.type_body)) @symbol.type

; Top-level const declarations.
(const_declaration
  (const_spec
    name: (identifier) @symbol.name)) @symbol.const

; Top-level var declarations.
(var_declaration
  (var_spec
    name: (identifier) @symbol.name)) @symbol.var

; Import declarations.
(import_declaration
  (import_spec
    path: (interpreted_string_literal) @import.path))

; Import declarations (grouped).
(import_declaration
  (import_spec_list
    (import_spec
      path: (interpreted_string_literal) @import.path)))
