; tree-sitter query for PHP call extraction.
; Used by internal/parser to detect function and method calls.

; Function calls: helper($x)
(function_call_expression
  function: (name) @call.function)

; Instance method calls: $obj->method()
(member_call_expression
  name: (name) @call.method)

; Static method calls: Class::method()
(scoped_call_expression
  name: (name) @call.method)

; Function references passed as arguments
(function_call_expression
  arguments: (arguments
    (argument
      (name) @call.function)))

; Constructor calls: new \Namespace\ClassName()
(object_creation_expression
  (qualified_name (name) @call.function))

; Constructor calls: new ClassName() (simple name)
(object_creation_expression
  (name) @call.function)
