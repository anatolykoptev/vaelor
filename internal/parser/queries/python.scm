; tree-sitter query for Python symbol extraction.
; Used by internal/parser to extract functions, classes, methods, and imports.

; === Top-level functions ===

; Plain function at module scope: def func()
(module
  (function_definition
    name: (identifier) @symbol.name) @symbol.function)

; Decorated function at module scope: @decorator def func()
(module
  (decorated_definition
    definition: (function_definition
      name: (identifier) @symbol.name) @symbol.function))

; === Classes ===

; Plain class: class Foo:
(class_definition
  name: (identifier) @symbol.name) @symbol.class

; Decorated class: @decorator class Foo:
(decorated_definition
  definition: (class_definition
    name: (identifier) @symbol.name) @symbol.class)

; === Methods inside classes ===

; Plain method in plain class: class Foo: def bar()
(class_definition
  body: (block
    (function_definition
      name: (identifier) @symbol.name) @symbol.method))

; Decorated method in plain class: class Foo: @deco def bar()
(class_definition
  body: (block
    (decorated_definition
      definition: (function_definition
        name: (identifier) @symbol.name) @symbol.method)))

; Plain method in decorated class: @deco class Foo: def bar()
(decorated_definition
  definition: (class_definition
    body: (block
      (function_definition
        name: (identifier) @symbol.name) @symbol.method)))

; Decorated method in decorated class: @deco class Foo: @deco def bar()
(decorated_definition
  definition: (class_definition
    body: (block
      (decorated_definition
        definition: (function_definition
          name: (identifier) @symbol.name) @symbol.method))))

; === Module-level variables (constants) ===

; Assignment at module scope: FOO = "bar"
(module
  (expression_statement
    (assignment
      left: (identifier) @symbol.name) @symbol.var))

; === Imports ===

; Import statements: import os / import os.path
(import_statement
  name: (dotted_name) @import.path)

; From-import statements: from pathlib import Path
(import_from_statement
  module_name: (dotted_name) @import.path)
