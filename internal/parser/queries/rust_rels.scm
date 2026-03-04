; Trait implementation: impl Handler for MyHandler
(impl_item
  trait: (type_identifier) @rel.subject
  type: (type_identifier) @rel.impl_target)

; Trait impl with generic target: impl Handler for Arc<Foo>
(impl_item
  trait: (type_identifier) @rel.subject
  type: (generic_type
    type: (type_identifier) @rel.impl_target))

; Trait impl with scoped trait: impl std::fmt::Display for Foo
(impl_item
  trait: (scoped_type_identifier
    name: (type_identifier) @rel.subject)
  type: (type_identifier) @rel.impl_target)
