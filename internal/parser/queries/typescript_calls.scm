(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))

; Function references passed as arguments
(call_expression
  arguments: (arguments
    (identifier) @call.function))
