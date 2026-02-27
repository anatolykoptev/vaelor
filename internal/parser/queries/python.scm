; tree-sitter query for Python symbol extraction.
; Used by internal/parser to extract functions, classes, methods, and imports.

; Top-level function definitions (def at module scope).
(module
  (function_definition
    name: (identifier) @symbol.name) @symbol.function)

; Class definitions.
(class_definition
  name: (identifier) @symbol.name) @symbol.class

; Methods inside classes (function_definition directly inside a class block).
(class_definition
  body: (block
    (function_definition
      name: (identifier) @symbol.name) @symbol.method))

; Import statements: import os / import os.path
(import_statement
  name: (dotted_name) @import.path)

; From-import statements: from pathlib import Path
(import_from_statement
  module_name: (dotted_name) @import.path)
