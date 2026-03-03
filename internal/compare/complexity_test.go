package compare

import (
	"testing"
)

func TestCyclomaticComplexity(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		language string
		expect   int
	}{
		{
			name:   "empty body",
			body:   "",
			expect: 1,
		},
		{
			name:   "simple function",
			body:   "func Foo() { return 1 }",
			expect: 1,
		},
		{
			name:   "single if",
			body:   "func Foo() { if x > 0 { return 1 } return 0 }",
			expect: 2,
		},
		{
			name:   "if-else chain",
			body:   "func Foo() { if x > 0 { return 1 } else if x < 0 { return -1 } else { return 0 } }",
			expect: 3,
		},
		{
			name:   "for loop",
			body:   "func Foo() { for i := 0; i < n; i++ { sum += i } }",
			expect: 2,
		},
		{
			name:   "switch with cases",
			body:   "func Foo() { switch x { case 1: a() case 2: b() case 3: c() default: d() } }",
			expect: 4,
		},
		{
			name:   "logical operators",
			body:   "func Foo() { if a && b || c { return true } }",
			expect: 4,
		},
		{
			name:     "Python patterns",
			body:     "def foo():\n    if x:\n        pass\n    elif y:\n        pass\n    for i in range(n):\n        pass\n    while z:\n        pass",
			language: "python",
			expect:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cyclomaticComplexity(tt.body, tt.language)
			if got != tt.expect {
				t.Errorf("cyclomaticComplexity() = %d, want %d", got, tt.expect)
			}
		})
	}
}
