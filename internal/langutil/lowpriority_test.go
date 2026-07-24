package langutil

import "testing"

func TestIsGeneratedFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"foo.pb.go", true},
		{"foo.pb.gw.go", true},
		{"bar_generated.go", true},
		{"bar_generated.py", true},
		{"baz_generated.rs", true},
		{"model.g.dart", true},
		{"main.go", false},
		{"generated.go", false}, // no underscore prefix / infix marker
		{"handler_test.go", false},
	}
	for _, tt := range tests {
		if got := IsGeneratedFile(tt.path); got != tt.want {
			t.Errorf("IsGeneratedFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsLowPriorityFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		// test files
		{"order_test.go", true},
		{"app.test.ts", true},
		{"src/__tests__/x.ts", true},
		// generated files
		{"foo.pb.go", true},
		{"model.g.dart", true},
		{"bar_generated.go", true},
		// regular source
		{"main.go", false},
		{"src/handler.go", false},
	}
	for _, tt := range tests {
		if got := IsLowPriorityFile(tt.path); got != tt.want {
			t.Errorf("IsLowPriorityFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
