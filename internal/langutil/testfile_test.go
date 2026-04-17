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
		// svelte/astro via infix — previously not caught by IsTestFile.
		{"Button.test.svelte", true},
		{"Modal.spec.svelte", true},
		{"Layout.test.astro", true},
		// __tests__ directory variant — both with and without infix in basename.
		{"src/components/__tests__/Button.test.ts", true},
		{"src/__tests__/helper.ts", true},          // directory alone is sufficient
		{"src/components/__tests__/util.go", true}, // directory alone, Go file
		// False positives guarded by extension allowlist.
		{"foo.test.md", false},
		{"bar.spec.json", false},
		{"data.spec.csv", false},
		// Negative: __tests__ substring must be a path segment, not a prefix/suffix.
		{"src/test_data/file.ts", false},
	}
	for _, tt := range tests {
		if got := IsTestFile(tt.path); got != tt.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestTestStem(t *testing.T) {
	tests := []struct {
		path     string
		wantStem string
		wantOK   bool
	}{
		// .test. infix variants.
		{"Button.test.ts", "Button", true},
		{"Button.test.tsx", "Button", true},
		{"Button.test.js", "Button", true},
		{"Button.test.svelte", "Button", true},
		{"Button.test.astro", "Button", true},
		// .spec. infix variants.
		{"Modal.spec.ts", "Modal", true},
		{"Modal.spec.svelte", "Modal", true},
		{"Layout.spec.astro", "Layout", true},
		// full path — should strip directory, use base.
		{"src/components/Button.test.ts", "Button", true},
		{"src/components/__tests__/Button.test.ts", "Button", true},
		// multi-dot stem — first .test. occurrence wins.
		{"manifest.test.config.ts", "manifest", true},
		// leading dot guard — .test.ts has no stem before the dot.
		{".test.ts", "", false},
		// extension not in allowlist — infix present but extension rejected.
		{"foo.test.md", "", false},
		{"bar.spec.json", "", false},
		{"data.spec.csv", "", false},
		// negative cases — no infix.
		{"random.ts", "", false},
		{"main.go", "", false},
		{"order_test.go", "", false},
		{"test_order.py", "", false},
	}
	for _, tt := range tests {
		gotStem, gotOK := TestStem(tt.path)
		if gotStem != tt.wantStem || gotOK != tt.wantOK {
			t.Errorf("TestStem(%q) = (%q, %v), want (%q, %v)", tt.path, gotStem, gotOK, tt.wantStem, tt.wantOK)
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
