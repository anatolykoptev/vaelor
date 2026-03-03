(call
  function: (identifier) @call.function)

(call
  function: (attribute
    attribute: (identifier) @call.method))

; Function references passed as arguments
(call
  arguments: (argument_list
    (identifier) @call.function))
