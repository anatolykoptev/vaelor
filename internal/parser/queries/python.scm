; tree-sitter query for Python symbol extraction.
; Used by internal/parser to extract functions, classes, methods, and imports.

; Top-level function definitions.
(function_definition
  name: (identifier) @symbol.name
  parameters: (parameters) @symbol.params
  return_type: (type)? @symbol.result) @symbol.function

; Async function definitions.
(decorated_definition
  definition: (function_definition
    name: (identifier) @symbol.name)) @symbol.function

; Class definitions.
(class_definition
  name: (identifier) @symbol.name
  bases: (argument_list)? @symbol.bases) @symbol.class

; Methods inside classes (same as functions, but captured in class body context).
(class_definition
  body: (block
    (function_definition
      name: (identifier) @symbol.name
      parameters: (parameters) @symbol.params) @symbol.method))

; Import statements.
(import_statement
  name: (dotted_name) @import.path)

; From-import statements.
(import_from_statement
  module_name: (dotted_name) @import.path)

; Assignment to global variables.
(module
  (expression_statement
    (assignment
      left: (identifier) @symbol.name)) @symbol.var)
