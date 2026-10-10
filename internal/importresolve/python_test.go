package importresolve

import "testing"

func TestPythonModuleMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		module, importer, file string
		want                   bool
	}{
		{"absolute file", "providers.open_ai", "/r/app/a.py", "/r/providers/open_ai.py", true},
		{"absolute under source root", "providers.open_ai", "/r/app/a.py", "/r/src/providers/open_ai.py", true},
		{"absolute package init", "providers", "/r/app/a.py", "/r/providers/__init__.py", true},
		{"different module", "providers.open_ai", "/r/app/a.py", "/r/providers/other.py", false},
		{"suffix must align on components", "ders.open_ai", "/r/app/a.py", "/r/providers/open_ai.py", false},
		{"third party never matches a longer path", "openai", "/r/app/a.py", "/r/providers/open_ai.py", false},
		{"relative sibling", ".open_ai", "/r/providers/child.py", "/r/providers/open_ai.py", true},
		{"relative sibling wrong dir", ".open_ai", "/r/app/child.py", "/r/providers/open_ai.py", false},
		{"relative parent wrong dir", "..providers.open_ai", "/r/app/sub/x.py", "/r/providers/open_ai.py", false},
		{"relative parent", "..providers.open_ai", "/r/app/sub/x.py", "/r/app/providers/open_ai.py", true},
		{"relative from init", ".open_ai", "/r/providers/__init__.py", "/r/providers/open_ai.py", true},
		{"non python file", "providers.open_ai", "/r/app/a.py", "/r/providers/open_ai.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PythonModuleMatches(tc.module, tc.importer, tc.file); got != tc.want {
				t.Fatalf("PythonModuleMatches(%q, %q, %q) = %v, want %v", tc.module, tc.importer, tc.file, got, tc.want)
			}
		})
	}
}
