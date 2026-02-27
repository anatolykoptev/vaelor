; tree-sitter query for TypeScript/JavaScript symbol extraction.
; Used by internal/parser to extract functions, classes, interfaces, and imports.

; Function declarations: function foo() {}
(function_declaration
  name: (identifier) @symbol.name
  parameters: (formal_parameters) @symbol.params) @symbol.function

; Class declarations: class Foo {}
(class_declaration
  name: (type_identifier) @symbol.name) @symbol.class

; Interface declarations (TypeScript): interface Foo {}
(interface_declaration
  name: (type_identifier) @symbol.name) @symbol.interface

; Type alias declarations (TypeScript): type Foo = ...
(type_alias_declaration
  name: (type_identifier) @symbol.name) @symbol.type

; Method definitions inside classes.
(class_declaration
  body: (class_body
    (method_definition
      name: (property_identifier) @symbol.name) @symbol.method))

; Arrow functions at module level: const foo = () => {}
(lexical_declaration
  (variable_declarator
    name: (identifier) @symbol.name
    value: (arrow_function))) @symbol.function

; Exported arrow functions: export const foo = () => {}
(export_statement
  (lexical_declaration
    (variable_declarator
      name: (identifier) @symbol.name
      value: (arrow_function)))) @symbol.function

; Export + function declaration: export function foo() {}
(export_statement
  declaration: (function_declaration
    name: (identifier) @symbol.name)) @symbol.function

; Import declarations: import { x } from 'module'
(import_statement
  source: (string) @import.path)
