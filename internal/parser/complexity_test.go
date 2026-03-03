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
		name string
		body string
		want int
	}{
		{"empty", "", 0},
		{"simple", "func foo() { return 42 }", 0},
		{
			"nested_if_for",
			"func f() {\n  if x {\n    for i := range y {\n      if z {\n      }\n    }\n  }\n}",
			// if x → +1 (nesting 0), for → +1+1 (nesting 1), if z → +1+2 (nesting 2) = 6
			6,
		},
		{
			"else_if_flat",
			"func f() {\n  if a {\n  } else if b {\n  } else if c {\n  }\n}",
			// if a → +1, else if b → +1 flat, else if c → +1 flat = 3
			3,
		},
		{
			"logical_operators",
			"func f() {\n  if a && b || c {\n  }\n}",
			// if → +1 (nesting 0), && → +1, || → +1 = 3
			3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CognitiveComplexity(tc.body)
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
	got := CognitiveComplexity(body)
	if got != 6 {
		t.Errorf("CognitiveComplexity(python) = %d, want 6", got)
	}
}

func TestNestingDepth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty", "", 0},
		{"flat", "func foo() { return 1 }", 1},
		{"nested_2", "func f() { if x { if y { } } }", 3},
		{"nested_3", "func f() { for { if a { switch { } } } }", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NestingDepth(tc.body)
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
	got := NestingDepth(body)
	if got != 2 {
		t.Errorf("NestingDepth(python) = %d, want 2", got)
	}
}
