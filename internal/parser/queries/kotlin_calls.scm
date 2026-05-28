; tree-sitter query for Kotlin call-site extraction.
; Captures plain function calls and method calls via navigation expression.

; Plain function call: println("hi"), compute(1, 2)
(call_expression
  (simple_identifier) @call.function)

; Method call: obj.method(), foo.bar.baz()
; Captures the final identifier in the navigation suffix.
(call_expression
  (navigation_expression
    (navigation_suffix
      (simple_identifier) @call.method)))
