package langutil

import "testing"

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"order_test.go", true},
		{"test_order.py", true},
		{"order_test.py", true},
		{"order_test.rs", true},
		{"app.test.ts", true},
		{"app.spec.js", true},
		{"app.test.tsx", true},
		{"src/test/helper.go", true},
		{"src/tests/util.py", true},
		{"main.go", false},
		{"order.py", false},
		{"testing.go", false},
	}
	for _, tt := range tests {
		if got := IsTestFile(tt.path); got != tt.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestRelPath(t *testing.T) {
	tests := []struct {
		abs, root, want string
	}{
		{"/repo/src/main.go", "/repo", "src/main.go"},
		{"/repo/main.go", "/repo", "main.go"},
		{"/repo/main.go", "", "/repo/main.go"},
		{"main.go", "", "main.go"},
	}
	for _, tt := range tests {
		if got := RelPath(tt.abs, tt.root); got != tt.want {
			t.Errorf("RelPath(%q, %q) = %q, want %q", tt.abs, tt.root, got, tt.want)
		}
	}
}
