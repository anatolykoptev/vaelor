; tree-sitter query for Kotlin symbol extraction.
; Used by internal/parser to extract classes, objects, functions, properties, and imports.
;
; NOTE: the Kotlin grammar (smacker/go-tree-sitter @ dd81d9e) defines FIELD_COUNT=0
; — no named fields — so all captures use positional child patterns, not name: syntax.

; Class declarations (includes data/sealed/abstract/open modifiers — all parse as class_declaration).
(class_declaration
  (type_identifier) @symbol.name) @symbol.class

; Object declarations (top-level singletons, companion objects excluded — they use companion_object node).
(object_declaration
  (type_identifier) @symbol.name) @symbol.class

; Top-level function declarations (source_file > function_declaration).
(source_file
  (function_declaration
    (simple_identifier) @symbol.name) @symbol.function)

; Method declarations inside class bodies.
(class_declaration
  (class_body
    (function_declaration
      (simple_identifier) @symbol.name) @symbol.method))

; Methods inside companion objects.
(companion_object
  (class_body
    (function_declaration
      (simple_identifier) @symbol.name) @symbol.method))

; Methods inside top-level object declarations.
(object_declaration
  (class_body
    (function_declaration
      (simple_identifier) @symbol.name) @symbol.method))

; Top-level property declarations.
(source_file
  (property_declaration
    (variable_declaration
      (simple_identifier) @symbol.name)) @symbol.var)

; Type aliases.
(type_alias
  (type_identifier) @symbol.name) @symbol.type

; Import directives.
(import_header
  (identifier) @import.path)
