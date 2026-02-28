(invocation_expression
  function: (identifier) @call.function)

(invocation_expression
  function: (member_access_expression
    name: (identifier) @call.method))
