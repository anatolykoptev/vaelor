package codegraph

import "testing"

func TestSymbolComplexity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty", "", 1},
		{"simple", "return 42", 1},
		{"one_if", "if x > 0 { return x }", 2},
		{"if_else_if", "if a { } else if b { }", 3},
		{"for_with_conditions", "for i := 0; i < n; i++ { if x && y { } }", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := symbolComplexity(tc.body)
			if got != tc.want {
				t.Errorf("symbolComplexity(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
