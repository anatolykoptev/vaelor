package main

import "testing"

func TestNormalizeCallTraceDirection(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"", "callees"},
		{"callees", "callees"},
		{"forward", "callees"},
		{"callers", "callers"},
		{"reverse", "callers"},
		{"unknown", "callees"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeCallTraceDirection(tc.input)
			if got != tc.want {
				t.Errorf("normalizeCallTraceDirection(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
