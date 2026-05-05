(method_invocation
  name: (identifier) @call.method)

; Function references passed as arguments — heuristic argref.
(method_invocation
  arguments: (argument_list
    (identifier) @call.argref))
