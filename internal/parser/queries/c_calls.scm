(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))

; Function references passed as arguments — heuristic argref.
(call_expression
  arguments: (argument_list
    (identifier) @call.argref))
