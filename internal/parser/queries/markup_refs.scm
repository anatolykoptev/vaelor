; markup_refs.scm — bare top-level identifier inside an isolated markup {expr}.
;
; A template expression like {count} is extracted (brace-stripped) and reparsed
; as a standalone statement, so it appears as (expression_statement (identifier)).
; tsx_calls.scm does NOT capture this shape (there is no enclosing call_expression
; or jsx_expression once the {expr} is isolated), so reaching React's bare {count}
; parity needs exactly this one pattern. It is emitted as an argref (a heuristic
; value reference, dropped by the callgraph unless field_access is set) — matching
; how React's own {count} is captured, via tsx_calls.scm's
; (jsx_expression (identifier) @call.argref).
;
; Scope: this query is run ONLY by the markup {expr} reparse path
; (markupExprReparse), never by the .tsx/.jsx handler — matching it against a
; whole React file would flood every bare-identifier statement as an argref.
(expression_statement (identifier) @call.argref)
