; tree-sitter query for Swift symbol extraction.
; Used by internal/parser to extract classes, structs, protocols, functions, properties, and imports.
;
; NOTE: the Swift grammar (smacker/go-tree-sitter @ dd81d9e, alex-pinkus/tree-sitter-swift)
; uses class_declaration as the umbrella node for class, struct, enum, actor, and extension
; declarations — disambiguated in MapCapture via the "extension" keyword child.
; protocol_declaration is a distinct node.
; FIELD_COUNT=0: no named-field lookup via ChildByFieldName; positional child patterns only.

; Class, struct, enum, actor declarations (all parse as class_declaration).
; Extension is also class_declaration — handler_swift.go MapCapture skips it for KindClass
; and instead relies on the method captures below to extract extension members.
(class_declaration
  (type_identifier) @symbol.name) @symbol.class

; Protocol declarations → KindInterface (Swift protocols ≅ Java/Kotlin interfaces).
(protocol_declaration
  (type_identifier) @symbol.name) @symbol.interface

; Top-level function declarations (source_file > function_declaration).
(source_file
  (function_declaration
    (simple_identifier) @symbol.name) @symbol.function)

; Method declarations inside class/struct/enum/actor/extension bodies.
(class_declaration
  (class_body
    (function_declaration
      (simple_identifier) @symbol.name) @symbol.method))

; Top-level property declarations.
(source_file
  (property_declaration
    (pattern
      (simple_identifier) @symbol.name)) @symbol.var)

; Type alias declarations.
(typealias_declaration
  (type_identifier) @symbol.name) @symbol.type

; Protocol body method declarations.
; Swift protocol methods parse as protocol_function_declaration inside protocol_body —
; a distinct node from function_declaration used in class/struct/actor bodies.
; Confirmed via AST probe: protocol_body > protocol_function_declaration > simple_identifier.
(protocol_declaration
  (protocol_body
    (protocol_function_declaration
      (simple_identifier) @symbol.name) @symbol.method))

; Import declarations (e.g. import Foundation, import UIKit).
(import_declaration
  (identifier) @import.path)
