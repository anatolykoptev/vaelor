; tree-sitter query for C++ symbol extraction.
; Used by internal/parser to extract functions, classes, structs, enums,
; namespaces, templates, type aliases, methods, and imports.

; ---------------------------------------------------------------------------
; Functions
; ---------------------------------------------------------------------------

; Top-level function definitions (includes qualified out-of-line methods like Config::Config).
(function_definition) @symbol.function

; ---------------------------------------------------------------------------
; Classes
; ---------------------------------------------------------------------------

; Class specifiers.
(class_specifier
  name: (type_identifier) @symbol.name) @symbol.class

; Template class: template<typename T> class Foo { ... }
(template_declaration
  (class_specifier
    name: (type_identifier) @symbol.name) @symbol.class)

; ---------------------------------------------------------------------------
; Structs
; ---------------------------------------------------------------------------

; Named struct specifiers.
(struct_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; ---------------------------------------------------------------------------
; Enums
; ---------------------------------------------------------------------------

; Named enum specifiers (includes enum class via enum_specifier in tree-sitter-cpp).
(enum_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; ---------------------------------------------------------------------------
; Namespaces
; ---------------------------------------------------------------------------

; Named namespace definitions (skip anonymous namespaces — they have no name child).
(namespace_definition
  name: (namespace_identifier) @symbol.name) @symbol.type

; ---------------------------------------------------------------------------
; Type aliases and typedefs
; ---------------------------------------------------------------------------

; C++11 using type alias: using Alias = OriginalType;
(alias_declaration
  name: (type_identifier) @symbol.name) @symbol.type

; C-style typedef: typedef OldType NewType;
(type_definition
  declarator: (type_identifier) @symbol.name) @symbol.type

; ---------------------------------------------------------------------------
; Template functions
; ---------------------------------------------------------------------------

; Template function: template<typename T> void foo() { ... }
(template_declaration
  (function_definition) @symbol.function)

; ---------------------------------------------------------------------------
; Methods inside class bodies
; ---------------------------------------------------------------------------

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

; ---------------------------------------------------------------------------
; Methods inside struct bodies
; ---------------------------------------------------------------------------

; Method declarations inside struct bodies.
(struct_specifier
  body: (field_declaration_list
    (declaration
      declarator: (function_declarator)) @symbol.method))

; Method field-declarations inside struct bodies.
(struct_specifier
  body: (field_declaration_list
    (field_declaration
      declarator: (function_declarator)) @symbol.method))

; ---------------------------------------------------------------------------
; Global variables/constants
; ---------------------------------------------------------------------------

; Top-level declarations with an initializer (constants, globals).
; Only captures declarations that have an init_declarator to avoid over-capturing.
(translation_unit
  (declaration
    declarator: (init_declarator)) @symbol.var)

; ---------------------------------------------------------------------------
; Imports
; ---------------------------------------------------------------------------

; #include <header>
(preproc_include path: (system_lib_string) @import.path)

; #include "header"
(preproc_include path: (string_literal) @import.path)

; NOTE: using_declaration and using_directive node types are not available
; in this tree-sitter-cpp grammar version.
