(method_invocation
  name: (identifier) @call.method)

; Function references passed as arguments
(method_invocation
  arguments: (argument_list
    (identifier) @call.function))
