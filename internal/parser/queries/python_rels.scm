; tree-sitter query for Python type relationship extraction.
; Captures class inheritance (extends).

; Single/multiple inheritance with simple identifier: class Child(Base)
(class_definition
  name: (identifier) @rel.subject
  superclasses: (argument_list
    (identifier) @rel.target))

; Inheritance with dotted name: class Child(module.Base)
(class_definition
  name: (identifier) @rel.subject
  superclasses: (argument_list
    (attribute) @rel.target))
