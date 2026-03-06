; tree-sitter query for C++ type relationship extraction.
; Captures class/struct inheritance (extends).

; Class inheritance with simple base: class Child : public Base {}
(class_specifier
  name: (type_identifier) @rel.subject
  (base_class_clause
    (type_identifier) @rel.target))

; Class inheritance with qualified base: class Child : public ns::Base {}
(class_specifier
  name: (type_identifier) @rel.subject
  (base_class_clause
    (qualified_identifier) @rel.target))

; Struct inheritance with simple base: struct Child : Base {}
(struct_specifier
  name: (type_identifier) @rel.subject
  (base_class_clause
    (type_identifier) @rel.target))

; Struct inheritance with qualified base: struct Child : ns::Base {}
(struct_specifier
  name: (type_identifier) @rel.subject
  (base_class_clause
    (qualified_identifier) @rel.target))
