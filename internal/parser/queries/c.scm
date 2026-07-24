; tree-sitter query for C symbol extraction.
; Used by internal/parser to extract functions, types, structs, enums, and imports.

; Function definitions (with a body).
(function_definition) @symbol.function

; Function declarations / prototypes (e.g. "Config* create_config(...);" ).
(declaration
  declarator: (pointer_declarator
    declarator: (function_declarator))) @symbol.function

(declaration
  declarator: (function_declarator)) @symbol.function

; Named struct definitions (e.g. "struct Server { ... }").
(struct_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Typedef (anonymous struct aliased to a name, or plain typedef).
; "typedef struct { ... } Config;"
(type_definition
  declarator: (type_identifier) @symbol.name) @symbol.type

; Named enum definitions.
(enum_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; #include <header.h>
(preproc_include path: (system_lib_string) @import.path)

; #include "header.h"
(preproc_include path: (string_literal) @import.path)

; #define macros (#664: macro kind).
; Object-like macros: #define MAX_BUFFER 4096
(preproc_def
  name: (identifier) @symbol.name) @symbol.macro

; Function-like macros: #define SQUARE(x) ((x) * (x))
(preproc_function_def
  name: (identifier) @symbol.name) @symbol.macro
