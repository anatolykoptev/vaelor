; tree-sitter query for Ruby symbol extraction.
; Used by internal/parser to extract methods, classes, modules, constants, and imports.

; Require / require_relative calls at the top-level program scope only.
; Scoping to (program ...) avoids matching string arguments in nested method calls.
(program
  (call
    method: (identifier)
    arguments: (argument_list
      (string
        (string_content) @import.path))))

; Top-level methods (def at program level).
(program
  (method
    name: (identifier) @symbol.name) @symbol.function)

; Instance methods inside class bodies.
(class
  body: (body_statement
    (method
      name: (identifier) @symbol.name) @symbol.method))

; Singleton methods (def self.foo) — class-level methods.
(singleton_method
  name: (identifier) @symbol.name) @symbol.method

; Class declarations.
(class
  name: (constant) @symbol.name) @symbol.class

; Module declarations.
(module
  name: (constant) @symbol.name) @symbol.type

; Constant assignments (e.g. MAX_RETRIES = 3).
(assignment
  left: (constant) @symbol.name) @symbol.const
