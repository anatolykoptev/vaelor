package deadcode

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// httpHandlerPatterns identify HTTP handler functions by signature.
var httpHandlerPatterns = []string{
	"http.ResponseWriter",
	"*http.Request",
	"gin.Context",
	"echo.Context",
	"fiber.Ctx",
	"chi.Router",
}

// wellKnownInterfaceMethods are method names commonly required by interfaces.
var wellKnownInterfaceMethods = map[string]bool{
	"ServeHTTP":     true,
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"Close":         true,
	"Read":          true,
	"Write":         true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
}

// constructorNames are method names that serve as class constructors in various
// languages. They are called implicitly by `new ClassName()` and should never
// be flagged as dead code.
var constructorNames = map[string]bool{
	"__construct": true, // PHP
	"__init__":    true, // Python
	"constructor": true, // JS/TS class
}

// isHTTPHandler checks if a symbol's signature indicates it's an HTTP handler.
func isHTTPHandler(sym *parser.Symbol) bool {
	sig := sym.Signature
	for _, pattern := range httpHandlerPatterns {
		if strings.Contains(sig, pattern) {
			return true
		}
	}
	return false
}

// isWellKnownInterfaceMethod checks if the function name matches a well-known interface method.
func isWellKnownInterfaceMethod(sym *parser.Symbol) bool {
	return sym.Kind == parser.KindMethod && wellKnownInterfaceMethods[sym.Name]
}

// isEntryPoint returns true for well-known entry point functions.
func isEntryPoint(name string) bool {
	switch name {
	case "main", "init", "TestMain":
		return true
	}
	return false
}

// isTestFunc returns true for Go test/benchmark/example/fuzz functions.
func isTestFunc(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			next := rune(name[len(prefix)])
			if unicode.IsUpper(next) || next == '_' {
				return true
			}
		}
	}
	return false
}

// isTestFile returns true if the file path ends with _test.go or similar test patterns.
func isTestFile(file string) bool {
	lower := strings.ToLower(file)
	testSuffixes := []string{
		"_test.go", "_test.py", "_test.rs",
		".test.ts", ".test.js",
		".spec.ts", ".spec.js",
	}
	for _, suf := range testSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/")
}

// isExported returns true if the name starts with an uppercase letter (Go convention).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// classifyConfidence assigns a confidence level based on symbol properties.
func classifyConfidence(sym *parser.Symbol, exported bool) string {
	if exported {
		return ConfidenceLow
	}
	if sym.Kind == parser.KindMethod {
		return ConfidenceMedium
	}
	if sym.Receiver != "" && strings.Contains(sym.Receiver, " for ") {
		return ConfidenceMedium
	}
	return ConfidenceHigh
}

// lines returns the number of source lines for a symbol.
func lines(sym *parser.Symbol) int {
	if sym.EndLine >= sym.StartLine {
		return int(sym.EndLine-sym.StartLine) + 1
	}
	return 1
}
