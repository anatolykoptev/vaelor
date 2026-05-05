(call
  method: (identifier) @call.method)

; Function references passed as arguments — heuristic argref.
(call
  arguments: (argument_list
    (identifier) @call.argref))
