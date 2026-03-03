; Regular function calls
(call_expression
  function: (identifier) @call.function)

; Method calls
(call_expression
  function: (member_expression
    property: (property_identifier) @call.method))

; Function references passed as arguments
(call_expression
  arguments: (arguments
    (identifier) @call.function))

; JSX expression references: onClick={handler}, ref={myRef}
; Captures function identifiers used as JSX attribute values.
(jsx_expression
  (identifier) @call.function)
