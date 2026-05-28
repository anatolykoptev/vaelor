; tree-sitter query for Kotlin type relationship extraction.
; Captures class/interface inheritance via delegation specifiers.
;
; Kotlin uses a single ":" syntax for both extends and implements
; (e.g. "class Cat : Animal, Runnable"). The grammar parses all supertypes
; as delegation_specifier children of class_declaration. relKindForLang("kotlin")
; returns RelExtends; the caller (runRelQuery) uses rel.impl_target for RelImplements
; only when that capture is present. Since Kotlin does not distinguish extends vs
; implements syntactically, all supertypes are emitted via @rel.target.

(class_declaration
  (type_identifier) @rel.subject
  (delegation_specifier
    (user_type
      (type_identifier) @rel.target)))
