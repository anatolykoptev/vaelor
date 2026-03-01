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
