; Regular function calls
(call_expression
  function: (identifier) @call.function)

; Method calls
(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))

; Function references passed as arguments — heuristic argref.
(call_expression
  arguments: (arguments
    (identifier) @call.argref))

; JSX expression references: onClick={handler}, ref={myRef}
; Captures function identifiers used as JSX attribute values — heuristic
; argref since the identifier may be a value/prop, not a callable.
(jsx_expression
  (identifier) @call.argref)
