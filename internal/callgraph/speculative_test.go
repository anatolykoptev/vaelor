package callgraph

import (
	"regexp"
	"testing"
)

// TestBuildSearchPattern_Swift verifies that the Swift case is handled by
// buildSearchPattern (internal/callgraph/speculative.go).
// Pattern must match plain `func greet(` and optional-receiver `func String.shout(`.
func TestBuildSearchPattern_Swift(t *testing.T) {
	cases := []struct {
		callName string
		input    string
		wantHit  bool
	}{
		// plain function
		{"greet", "func greet(name: String) -> String {", true},
		// extension-receiver (rare but valid in Swift)
		{"shout", "func String.shout() -> String {", true},
		// should NOT match a call site (no "func" keyword)
		{"greet", "let x = greet(\"world\")", false},
		// should NOT match a different function name
		{"greet", "func greeting(name: String) -> String {", false},
	}

	for _, c := range cases {
		t.Run(c.callName+"_"+c.input[:min(12, len(c.input))], func(t *testing.T) {
			pat := buildSearchPattern(c.callName, "swift")
			rx, err := regexp.Compile(pat)
			if err != nil {
				t.Fatalf("buildSearchPattern(%q, %q) produced invalid regexp %q: %v", c.callName, "swift", pat, err)
			}
			got := rx.MatchString(c.input)
			if got != c.wantHit {
				t.Errorf("pattern %q against %q: got match=%v, want %v", pat, c.input, got, c.wantHit)
			}
		})
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildSearchPattern_Kotlin verifies that the Kotlin case is handled by
// buildSearchPattern (internal/callgraph/speculative.go).
// Pattern must match plain `fun greet(` and extension-receiver `fun String.shout(`.
func TestBuildSearchPattern_Kotlin(t *testing.T) {
	cases := []struct {
		callName string
		input    string
		wantHit  bool
	}{
		// plain function
		{"greet", "fun greet(name: String): String {", true},
		// extension-receiver function
		{"shout", "fun String.shout(): String {", true},
		// should NOT match a call site (no "fun" keyword)
		{"greet", "val x = greet(\"world\")", false},
		// should NOT match a different function name
		{"greet", "fun greeting(name: String): String {", false},
	}

	for _, c := range cases {
		t.Run(c.callName, func(t *testing.T) {
			pat := buildSearchPattern(c.callName, "kotlin")
			rx, err := regexp.Compile(pat)
			if err != nil {
				t.Fatalf("buildSearchPattern(%q, %q) produced invalid regexp %q: %v", c.callName, "kotlin", pat, err)
			}
			got := rx.MatchString(c.input)
			if got != c.wantHit {
				t.Errorf("pattern %q against %q: got match=%v, want %v", pat, c.input, got, c.wantHit)
			}
		})
	}
}
