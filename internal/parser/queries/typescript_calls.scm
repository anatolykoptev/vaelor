(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))
