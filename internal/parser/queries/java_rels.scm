; tree-sitter query for Java type relationship extraction.
; Captures class extends, class implements, and interface extends.

; Class extends: class Dog extends Animal {}
(class_declaration
  name: (identifier) @rel.subject
  (superclass
    (type_identifier) @rel.target))

; Class implements: class Dog implements Runnable {}
(class_declaration
  name: (identifier) @rel.subject
  (super_interfaces
    (type_list
      (type_identifier) @rel.impl_target)))

; Interface extends: interface Child extends Base {}
(interface_declaration
  name: (identifier) @rel.subject
  (extends_interfaces
    (type_list
      (type_identifier) @rel.target)))
