(invocation_expression
  function: (identifier) @call.function)

(invocation_expression
  function: (member_access_expression
    name: (identifier) @call.method))

; Function references passed as arguments — heuristic argref.
(invocation_expression
  arguments: (argument_list
    (argument
      (identifier) @call.argref)))
