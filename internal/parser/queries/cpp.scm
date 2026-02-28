; tree-sitter query for C++ symbol extraction.
; Used by internal/parser to extract functions, classes, structs, enums, methods, and imports.

; Top-level function definitions (includes qualified out-of-line methods like Config::Config).
(function_definition) @symbol.function

; Class specifiers.
(class_specifier
  name: (type_identifier) @symbol.name) @symbol.class

; Named struct specifiers.
(struct_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Named enum specifiers.
(enum_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Method declarations inside class bodies (constructor/method prototypes).
; "declaration" nodes with a function_declarator inside a class body.
(class_specifier
  body: (field_declaration_list
    (declaration
      declarator: (function_declarator)) @symbol.method))

; Method field-declarations inside class bodies (e.g. "std::string address() const;").
(class_specifier
  body: (field_declaration_list
    (field_declaration
      declarator: (function_declarator)) @symbol.method))

; #include <header>
(preproc_include path: (system_lib_string) @import.path)

; #include "header"
(preproc_include path: (string_literal) @import.path)
