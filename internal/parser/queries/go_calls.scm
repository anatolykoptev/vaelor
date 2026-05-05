(call_expression
  function: (identifier) @call.function)

(call_expression
  function: (selector_expression
    field: (field_identifier) @call.method))

; Function references passed as arguments (e.g. Register("name", handler)).
; Heuristic — most identifier args are values, not function refs. Tagged as
; argref so the call graph drops the unresolved ones by default.
(call_expression
  arguments: (argument_list
    (identifier) @call.argref))

; Qualified references in arguments (e.g. Register("name", pkg.Handler)).
; Heuristic — `opts.Slug` etc. land here too.
(call_expression
  arguments: (argument_list
    (selector_expression
      field: (field_identifier) @call.argref_method)))

; Function references in struct literal fields (e.g. Deps{Fn: handler}).
; keyed_element has two literal_element children: [0]=key, [1]=value.
; Heuristic — string/int literal values are skipped, but plain identifier
; values are often plain references to other vars, not function refs.
(keyed_element
  (literal_element)
  (literal_element
    (identifier) @call.argref))

; Qualified references in struct literal fields (e.g. Deps{Fn: pkg.Handler}).
(keyed_element
  (literal_element)
  (literal_element
    (selector_expression
      field: (field_identifier) @call.argref_method)))
