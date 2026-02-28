; tree-sitter query for Java symbol extraction.
; Used by internal/parser to extract classes, interfaces, enums, methods, and imports.

; Class declarations.
(class_declaration
  name: (identifier) @symbol.name) @symbol.class

; Interface declarations.
(interface_declaration
  name: (identifier) @symbol.name) @symbol.interface

; Enum declarations.
(enum_declaration
  name: (identifier) @symbol.name) @symbol.type

; Method declarations inside class bodies.
(class_declaration
  body: (class_body
    (method_declaration
      name: (identifier) @symbol.name) @symbol.method))

; Constructor declarations inside class bodies.
(class_declaration
  body: (class_body
    (constructor_declaration
      name: (identifier) @symbol.name) @symbol.method))

; Method declarations inside interface bodies.
(interface_declaration
  body: (interface_body
    (method_declaration
      name: (identifier) @symbol.name) @symbol.method))

; Method declarations inside enum bodies.
(enum_declaration
  body: (enum_body
    (enum_body_declarations
      (method_declaration
        name: (identifier) @symbol.name) @symbol.method)))

; Constructor declarations inside enum bodies.
(enum_declaration
  body: (enum_body
    (enum_body_declarations
      (constructor_declaration
        name: (identifier) @symbol.name) @symbol.method)))

; Import declarations.
(import_declaration
  (scoped_identifier) @import.path)
