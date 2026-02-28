package codegraph

import "testing"

// TestRelPath verifies relPath behavior for various input combinations.
func TestRelPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		abs  string
		root string
		want string
	}{
		{
			name: "absolute path with root prefix",
			abs:  "/home/krolik/src/go-code/internal/parser/parser.go",
			root: "/home/krolik/src/go-code",
			want: "internal/parser/parser.go",
		},
		{
			name: "absolute path without root prefix",
			abs:  "/tmp/other/file.go",
			root: "/home/krolik/src/go-code",
			want: "../../../../tmp/other/file.go",
		},
		{
			name: "empty root returns abs unchanged",
			abs:  "/some/absolute/path.go",
			root: "",
			want: "/some/absolute/path.go",
		},
		{
			name: "already relative path with root",
			abs:  "/home/krolik/src/go-code/cmd/main.go",
			root: "/home/krolik/src/go-code",
			want: "cmd/main.go",
		},
		{
			name: "root equal to abs directory",
			abs:  "/home/krolik/src/go-code",
			root: "/home/krolik/src/go-code",
			want: ".",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := relPath(tc.abs, tc.root)
			if got != tc.want {
				t.Errorf("relPath(%q, %q) = %q; want %q", tc.abs, tc.root, got, tc.want)
			}
		})
	}
}
