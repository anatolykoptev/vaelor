; tree-sitter query for TypeScript type relationship extraction.
; Captures class extends, class implements, and interface extends.

; Class extends: class Child extends Base {}
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (extends_clause
      (identifier) @rel.target)))

; Class implements: class Service implements IHandler {}
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (implements_clause
      (type_identifier) @rel.impl_target)))

; Interface extends: interface IChild extends IBase {}
(interface_declaration
  name: (type_identifier) @rel.subject
  (extends_type_clause
    (type_identifier) @rel.target))
