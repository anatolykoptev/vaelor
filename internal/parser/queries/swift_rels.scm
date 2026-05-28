; tree-sitter query for Swift type relationship extraction.
; Captures class/struct/actor inheritance and protocol conformance.
;
; Swift uses a single ":" syntax for both extends and implements
; (e.g. "class Cat: Animal, Runnable"). The grammar parses all supertypes
; as inheritance_specifier children of class_declaration.
;
; Node hierarchy confirmed via AST probe:
;   class_declaration
;     type_identifier @rel.subject
;     inheritance_specifier
;       user_type
;         type_identifier @rel.target
;
; Swift grammar uses "inheritance_specifier" (not "delegation_specifier" as in Kotlin).
; relKindForLang("swift") returns RelExtends for all edges.

(class_declaration
  (type_identifier) @rel.subject
  (inheritance_specifier
    (user_type
      (type_identifier) @rel.target)))
