; Direct function call: helper()
(call_expression
  function: (identifier) @call.function)

; Method call: self.method(), obj.do_thing()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))

; Scoped function call: Module::func(), Type::new()
(call_expression
  function: (scoped_identifier
    name: (identifier) @call.function))

; Macro invocations: println!(), vec![], format!()
(macro_invocation
  macro: (identifier) @call.function)

; Scoped macro invocations: tokio::select!()
(macro_invocation
  macro: (scoped_identifier
    name: (identifier) @call.function))
