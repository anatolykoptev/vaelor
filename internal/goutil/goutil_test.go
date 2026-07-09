package goutil

import "testing"

func TestIsStdlibImport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"fmt", true},
		{"net/http", true},
		{"os", true},
		{"github.com/foo/bar", false},
		{"golang.org/x/text", false},
		{"", true},
	}
	for _, tt := range tests {
		if got := IsStdlibImport(tt.path); got != tt.want {
			t.Errorf("IsStdlibImport(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestPackageDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		root, file, want string
	}{
		{"/repo", "/repo/cmd/main.go", "cmd"},
		{"/repo", "/repo/main.go", "repo"},
		{"/repo", "/repo/internal/foo/bar.go", "internal/foo"},
	}
	for _, tt := range tests {
		if got := PackageDir(tt.root, tt.file); got != tt.want {
			t.Errorf("PackageDir(%q, %q) = %q, want %q", tt.root, tt.file, got, tt.want)
		}
	}
}

func TestSortedSetKeys(t *testing.T) {
	t.Parallel()
	m := map[string]struct{}{"c": {}, "a": {}, "b": {}}
	got := SortedSetKeys(m)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("SortedSetKeys = %v, want [a b c]", got)
	}
	if got := SortedSetKeys(nil); len(got) != 0 {
		t.Errorf("SortedSetKeys(nil) = %v, want []", got)
	}
}

func TestCountLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\nb\nc", 3},
		{"a\nb\nc\n", 3},
	}
	for _, tt := range tests {
		if got := CountLines([]byte(tt.input)); got != tt.want {
			t.Errorf("CountLines(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
