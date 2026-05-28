; tree-sitter query for Swift call-site extraction.
; Captures plain function calls and method calls via navigation expression.
;
; Swift grammar (smacker/go-tree-sitter, alex-pinkus/tree-sitter-swift):
; - Plain calls:  call_expression > simple_identifier @call.function
; - Method calls: call_expression > navigation_expression > navigation_suffix > simple_identifier @call.method
; Node shapes confirmed via AST probe on the Swift grammar.

; Plain function call: print("hi"), compute(1, 2)
(call_expression
  (simple_identifier) @call.function)

; Method call: obj.method(), self.uppercased()
; Captures the final identifier in the navigation suffix.
(call_expression
  (navigation_expression
    (navigation_suffix
      (simple_identifier) @call.method)))
