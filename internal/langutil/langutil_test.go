package langutil

import "testing"

func TestIsExportedForDoc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		symName  string
		language string
		isPublic bool
		want     bool
	}{
		// IsPublic early-return — wins regardless of name/language.
		{name: "IsPublic=true always exported", symName: "_internal", language: "rust", isPublic: true, want: true},
		{name: "IsPublic=true empty name", symName: "", language: "go", isPublic: true, want: true},

		// Empty name.
		{name: "empty name go", symName: "", language: "go", isPublic: false, want: false},
		{name: "empty name js", symName: "", language: "javascript", isPublic: false, want: false},

		// Go / Java / C# — uppercase-first.
		{name: "Go exported", symName: "Handler", language: "go", isPublic: false, want: true},
		{name: "Go unexported", symName: "handler", language: "go", isPublic: false, want: false},
		{name: "Java exported", symName: "Builder", language: "java", isPublic: false, want: true},
		{name: "Java unexported", symName: "buildHelper", language: "java", isPublic: false, want: false},
		{name: "C# exported", symName: "MyClass", language: "csharp", isPublic: false, want: true},
		{name: "cs alias exported", symName: "Foo", language: "cs", isPublic: false, want: true},

		// JavaScript / TypeScript — non-underscore = exported.
		{name: "JS camelCase exported", symName: "renderWidget", language: "javascript", isPublic: false, want: true},
		{name: "TS camelCase exported", symName: "parseValue", language: "typescript", isPublic: false, want: true},
		{name: "JS underscore not exported", symName: "_private", language: "javascript", isPublic: false, want: false},
		{name: "TS uppercase exported", symName: "Component", language: "typescript", isPublic: false, want: true},

		// Rust — non-underscore = exported (mirrors isExportedSymbol behavior).
		{name: "Rust snake_case exported", symName: "build_graph", language: "rust", isPublic: false, want: true},
		{name: "Rust underscore not exported", symName: "_internal", language: "rust", isPublic: false, want: false},

		// Python — non-underscore = exported.
		{name: "Python func exported", symName: "parse_config", language: "python", isPublic: false, want: true},
		{name: "Python underscore not exported", symName: "_helper", language: "python", isPublic: false, want: false},

		// Unknown language — non-underscore fallback.
		{name: "Unknown language non-underscore", symName: "myFunc", language: "kotlin", isPublic: false, want: true},
		{name: "Unknown language underscore", symName: "_hidden", language: "kotlin", isPublic: false, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsExportedForDoc(tc.symName, tc.language, tc.isPublic)
			if got != tc.want {
				t.Errorf("IsExportedForDoc(%q, %q, %v) = %v, want %v",
					tc.symName, tc.language, tc.isPublic, got, tc.want)
			}
		})
	}
}
