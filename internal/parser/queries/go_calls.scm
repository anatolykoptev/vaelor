(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (selector_expression
    field: (field_identifier) @call.method))

; Function references passed as arguments (e.g. Register("name", handler))
(call_expression
  arguments: (argument_list
    (identifier) @call.function))

; Qualified references in arguments (e.g. Register("name", pkg.Handler))
(call_expression
  arguments: (argument_list
    (selector_expression
      field: (field_identifier) @call.method)))
