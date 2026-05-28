package callgraph

import (
	"regexp"
	"testing"
)

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
