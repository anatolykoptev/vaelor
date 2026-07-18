package deadcode

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// cppSpecialMethods are C++ methods called implicitly by the compiler/runtime.
var cppSpecialMethods = map[string]bool{
	"main": true,
	// Rule of five constructors/operators are handled by prefix checks below.
}

// isCppImplicitMethod returns true if a C++ symbol is called implicitly.
func isCppImplicitMethod(sym *parser.Symbol) bool {
	if sym.Language != "cpp" && sym.Language != "c" {
		return false
	}
	if cppSpecialMethods[sym.Name] {
		return true
	}
	// Destructors: ~ClassName
	if strings.HasPrefix(sym.Name, "~") {
		return true
	}
	// Operator overloads: operator==, operator<<, etc.
	if strings.HasPrefix(sym.Name, "operator") {
		return true
	}
	// Virtual/override methods (called via vtable) and friend functions.
	for _, attr := range sym.Attributes {
		if attr == "virtual" || attr == "override" || attr == "friend" {
			return true
		}
	}
	return false
}
