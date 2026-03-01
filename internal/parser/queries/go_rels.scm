; tree-sitter query for Go type relationship extraction.
; Captures struct embedding and interface composition.

; Struct embedding: type Foo struct { Bar }
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (struct_type
      (field_declaration_list
        (field_declaration
          type: (type_identifier) @rel.target)))))

; Struct embedding with qualified type: type Foo struct { pkg.Bar }
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (struct_type
      (field_declaration_list
        (field_declaration
          type: (qualified_type) @rel.target)))))

; Struct embedding with pointer: type Foo struct { *Bar }
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (struct_type
      (field_declaration_list
        (field_declaration
          type: (pointer_type
            (type_identifier) @rel.target))))))

; Interface composition: type Foo interface { Bar } (via type_elem)
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (interface_type
      (type_elem
        (type_identifier) @rel.target))))

; Interface composition with qualified type: type Foo interface { io.Reader }
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (interface_type
      (type_elem
        (qualified_type) @rel.target))))
