; tree-sitter query for C++ call extraction.

; Simple function calls: foo()
(call_expression
  function: (identifier) @call.function)

; Method calls: obj.method()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))

; Qualified calls: ns::func(), Class::staticMethod()
(call_expression
  function: (qualified_identifier) @call.function)

; Template function calls: make_shared<Foo>()
(call_expression
  function: (template_function) @call.function)

; new expressions: new MyClass(...)
(new_expression
  type: (type_identifier) @call.function)

; Function references passed as arguments — heuristic argref.
(call_expression
  arguments: (argument_list
    (identifier) @call.argref))
