(call
  function: (identifier) @call.function)

(call
  function: (attribute
    attribute: (identifier) @call.method))

; Function references passed as arguments — heuristic. Tagged as argref so
; the call graph drops unresolved ones by default (vars, locals, attrs).
(call
  arguments: (argument_list
    (identifier) @call.argref))

; Decorator references (the decorator itself is a "call" to the function)
(decorator (identifier) @call.function)
(decorator (attribute attribute: (identifier) @call.method))
; Decorator with call syntax: @decorator(args)
(decorator (call function: (identifier) @call.function))
(decorator (call function: (attribute attribute: (identifier) @call.method)))

; super() calls
(call function: (identifier) @call.function (#eq? @call.function "super"))
