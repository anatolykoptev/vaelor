(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (selector_expression
    field: (field_identifier) @call.method))

; Function references passed as arguments (e.g. Register("name", handler))
(call_expression
  arguments: (argument_list
    (identifier) @call.function))

; Qualified references in arguments (e.g. Register("name", pkg.Handler))
(call_expression
  arguments: (argument_list
    (selector_expression
      field: (field_identifier) @call.method)))

; Function references in struct literal fields (e.g. Deps{Fn: handler})
; keyed_element has two literal_element children: [0]=key, [1]=value
(keyed_element
  (literal_element)
  (literal_element
    (identifier) @call.function))

; Qualified references in struct literal fields (e.g. Deps{Fn: pkg.Handler})
(keyed_element
  (literal_element)
  (literal_element
    (selector_expression
      field: (field_identifier) @call.method)))
