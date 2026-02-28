; tree-sitter query for C# symbol extraction.
; Used by internal/parser to extract classes, interfaces, structs, enums, methods, and imports.

; Using directives — simple identifier (e.g. using System;).
(using_directive
  (identifier) @import.path)

; Using directives — qualified name (e.g. using System.Collections.Generic;).
(using_directive
  (qualified_name) @import.path)

; Namespace declarations.
(namespace_declaration
  name: (identifier) @symbol.name) @symbol.type

; Class declarations.
(class_declaration
  name: (identifier) @symbol.name) @symbol.class

; Interface declarations.
(interface_declaration
  name: (identifier) @symbol.name) @symbol.interface

; Struct declarations.
(struct_declaration
  name: (identifier) @symbol.name) @symbol.type

; Enum declarations.
(enum_declaration
  name: (identifier) @symbol.name) @symbol.type

; Method declarations (instance and static methods).
(method_declaration
  name: (identifier) @symbol.name) @symbol.method

; Constructor declarations.
(constructor_declaration
  name: (identifier) @symbol.name) @symbol.method
