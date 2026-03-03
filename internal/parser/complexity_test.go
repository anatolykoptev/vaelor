package parser

import "testing"

func TestComplexity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty", "", 0},
		{"simple", "func foo() { return 42 }", 1},
		{"one_if", "if x > 0 { return x }", 2},
		{"if_else_if", "if a { } else if b { }", 3},
		{"for_with_conditions", "for i := 0; i < n; i++ { if x && y { } }", 4},
		{"switch_cases", "switch x { case 1: case 2: case 3: }", 4},
		{"logical_or", "if a || b || c { }", 4},
		{"while_loop", "while running { if err { break } }", 3},
		{"try_catch", "try { run() } catch(err) { log(err) }", 2},
		{"python_except", "try: run() except ValueError: pass", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Complexity(tc.body)
			if got != tc.want {
				t.Errorf("Complexity(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestComplexity_MultiLine(t *testing.T) {
	t.Parallel()
	body := `func foo(x int) int {
	if x > 0 {
		return 1
	} else if x < 0 {
		for i := range 10 {
			if i > x {
				return i
			}
		}
	}
	return 0
}`
	got := Complexity(body)
	if got < 4 {
		t.Errorf("multi-line branchy function: expected complexity >= 4, got %d", got)
	}
}

func TestCognitiveComplexity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		language string
		want     int
	}{
		{"empty", "", "go", 0},
		{"simple", "func foo() { return 42 }", "go", 0},
		{
			"nested_if_for",
			"func f() {\n  if x {\n    for i := range y {\n      if z {\n      }\n    }\n  }\n}",
			"go",
			// if x → +1 (nesting 0), for → +1+1 (nesting 1), if z → +1+2 (nesting 2) = 6
			6,
		},
		{
			"else_if_flat",
			"func f() {\n  if a {\n  } else if b {\n  } else if c {\n  }\n}",
			"go",
			// if a → +1, else if b → +1 flat, else if c → +1 flat = 3
			3,
		},
		{
			"logical_operators",
			"func f() {\n  if a && b || c {\n  }\n}",
			"go",
			// if → +1 (nesting 0), && → +1, || → +1 = 3
			3,
		},
		{
			"string_literal_if",
			"func f() {\n  fmt.Println(\"if this were real\")\n}",
			"go",
			0,
		},
		{
			"string_literal_logical",
			"func f() {\n  sql := \"WHERE a && b || c\"\n  return sql\n}",
			"go",
			0,
		},
		{
			"comment_if",
			"func f() {\n  // if needed, add more\n  return 1\n}",
			"go",
			0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CognitiveComplexity(tc.body, tc.language)
			if got != tc.want {
				t.Errorf("CognitiveComplexity(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestCognitiveComplexity_Python(t *testing.T) {
	t.Parallel()
	body := "def foo(x):\n    if x > 0:\n        for i in range(10):\n            if i > x:\n                return i\n    return 0"
	// if → +1 (nesting 0), for → +1+1 (nesting 1), if → +1+2 (nesting 2) = 6
	got := CognitiveComplexity(body, "python")
	if got != 6 {
		t.Errorf("CognitiveComplexity(python) = %d, want 6", got)
	}
}

func TestCognitiveComplexity_PythonStringLiterals(t *testing.T) {
	t.Parallel()
	body := "def f():\n    msg = \"bread and butter\"\n    return msg"
	got := CognitiveComplexity(body, "python")
	if got != 0 {
		t.Errorf("Python string literal ' and ' inflates CognitiveComplexity = %d, want 0", got)
	}
}

func TestNestingDepth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		language string
		want     int
	}{
		{"empty", "", "go", 0},
		{"flat", "func foo() { return 1 }", "go", 0},
		{"nested_1", "func f() { if x { if y { } } }", "go", 2},
		{"nested_2", "func f() { for { if a { switch { } } } }", "go", 3},
		{"string_braces", "func f() {\n  s := \"{ { { }\"\n  return s\n}", "go", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NestingDepth(tc.body, tc.language)
			if got != tc.want {
				t.Errorf("NestingDepth(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestNestingDepth_Python(t *testing.T) {
	t.Parallel()
	body := "def foo():\n    if x:\n        for i in range(10):\n            pass\n    return 0"
	// base=4, max indent=12, depth=(12-4)/4=2
	got := NestingDepth(body, "python")
	if got != 2 {
		t.Errorf("NestingDepth(python) = %d, want 2", got)
	}
}

func TestStripStringLiterals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		has  string // substring that should NOT be present after stripping
	}{
		{"double_quoted", `fmt.Println("if x > 0")`, "if x"},
		{"escaped_quote", `s := "say \"hello\""`, "hello"},
		{"raw_string", "s := `if { } else`", "if {"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripStringLiterals(tc.in)
			if contains(got, tc.has) {
				t.Errorf("stripStringLiterals(%q) still contains %q: %q", tc.in, tc.has, got)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && containsCheck(s, sub)
}

func containsCheck(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
