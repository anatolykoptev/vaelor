package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestIsExportedSymbol(t *testing.T) {
	tests := []struct {
		name string
		lang string
		want bool
	}{
		// Go: uppercase = exported
		{"HandleAuth", "go", true},
		{"Client", "go", true},
		{"handleAuth", "go", false},
		{"clientPool", "go", false},
		// Java and C#: same uppercase rule
		{"MyClass", "java", true},
		{"myMethod", "java", false},
		{"MyService", "csharp", true},
		{"myField", "cs", false},
		// Python: underscore = private
		{"handle_auth", "python", true},
		{"PublicFunc", "python", true},
		{"_private", "python", false},
		{"__dunder", "python", false},
		// JavaScript/TypeScript
		{"fetchData", "javascript", true},
		{"_internal", "javascript", false},
		// Rust
		{"new_client", "rust", true},
		{"_unused", "rust", false},
		// Edge cases
		{"", "go", false},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_"+tc.lang, func(t *testing.T) {
			got := isExportedSymbol(tc.name, tc.lang)
			if got != tc.want {
				t.Errorf("isExported(%q, %q) = %v, want %v", tc.name, tc.lang, got, tc.want)
			}
		})
	}
}

func TestExtractAPISurface(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			Name:      "HandleAuth",
			Kind:      parser.KindFunction,
			Signature: "func HandleAuth(w http.ResponseWriter, r *http.Request)",
			File:      "/repo/auth/handler.go",
		},
		{
			Name:      "Client",
			Kind:      parser.KindType,
			Signature: "type Client struct",
			File:      "/repo/client/client.go",
		},
		{
			Name:      "handleInternal",
			Kind:      parser.KindFunction,
			Signature: "func handleInternal()",
			File:      "/repo/auth/handler.go",
		},
		{
			Name:      "clientPool",
			Kind:      parser.KindVar,
			Signature: "var clientPool sync.Pool",
			File:      "/repo/client/client.go",
		},
	}

	got := ExtractAPISurface(symbols, "go")

	if len(got) != 2 {
		t.Fatalf("ExtractAPISurface returned %d symbols, want 2", len(got))
	}

	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["HandleAuth"] {
		t.Error("expected HandleAuth in result")
	}
	if !names["Client"] {
		t.Error("expected Client in result")
	}
	if names["handleInternal"] {
		t.Error("handleInternal should be excluded (unexported)")
	}
	if names["clientPool"] {
		t.Error("clientPool should be excluded (var kind + unexported)")
	}

	// Check Package is set to directory of file.
	for _, s := range got {
		if s.Package == "" {
			t.Errorf("symbol %q has empty Package", s.Name)
		}
	}
}

func TestExtractAPISurface_FiltersByKind(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "MyConst", Kind: parser.KindConst, File: "/repo/pkg/x.go"},
		{Name: "MyVar", Kind: parser.KindVar, File: "/repo/pkg/x.go"},
		{Name: "MyFunc", Kind: parser.KindFunction, File: "/repo/pkg/x.go"},
		{Name: "MyInterface", Kind: parser.KindInterface, File: "/repo/pkg/x.go"},
		{Name: "MyMethod", Kind: parser.KindMethod, File: "/repo/pkg/x.go"},
		{Name: "MyType", Kind: parser.KindType, File: "/repo/pkg/x.go"},
	}

	got := ExtractAPISurface(symbols, "go")

	if len(got) != 4 {
		t.Fatalf("expected 4 symbols (function, interface, method, type), got %d", len(got))
	}
}

func TestComputeAPIDiff(t *testing.T) {
	a := []APISymbol{
		{Name: "HandleAuth", Kind: "function", Signature: "func HandleAuth() error"},
		{Name: "Client", Kind: "type", Signature: "type Client struct"},
	}
	b := []APISymbol{
		{Name: "HandleAuth", Kind: "function", Signature: "func HandleAuth(ctx context.Context) error"},
		{Name: "Server", Kind: "type", Signature: "type Server struct"},
	}

	diff := ComputeAPIDiff(a, b)

	if diff.Common != 1 {
		t.Errorf("Common = %d, want 1", diff.Common)
	}
	if diff.OnlyACount != 1 {
		t.Errorf("OnlyACount = %d, want 1", diff.OnlyACount)
	}
	if diff.OnlyBCount != 1 {
		t.Errorf("OnlyBCount = %d, want 1", diff.OnlyBCount)
	}
	if diff.ChangedSig != 1 {
		t.Errorf("ChangedSig = %d, want 1", diff.ChangedSig)
	}

	if len(diff.OnlyA) != 1 || diff.OnlyA[0].Name != "Client" {
		t.Errorf("OnlyA = %v, want [{Client ...}]", diff.OnlyA)
	}
	if len(diff.OnlyB) != 1 || diff.OnlyB[0].Name != "Server" {
		t.Errorf("OnlyB = %v, want [{Server ...}]", diff.OnlyB)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].Name != "HandleAuth" {
		t.Errorf("Changed = %v, want [{HandleAuth ...}]", diff.Changed)
	}
}

func TestComputeAPIDiff_Empty(t *testing.T) {
	diff := ComputeAPIDiff(nil, nil)
	if diff.Common != 0 || diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Errorf("empty diff should have all zeros, got %+v", diff)
	}
}

func TestComputeAPIDiff_Identical(t *testing.T) {
	syms := []APISymbol{
		{Name: "Foo", Kind: "function", Signature: "func Foo()"},
		{Name: "Bar", Kind: "type", Signature: "type Bar struct"},
	}

	diff := ComputeAPIDiff(syms, syms)

	if diff.Common != 2 {
		t.Errorf("Common = %d, want 2", diff.Common)
	}
	if diff.ChangedSig != 0 {
		t.Errorf("ChangedSig = %d, want 0 for identical surfaces", diff.ChangedSig)
	}
	if diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Errorf("expected no OnlyA/OnlyB for identical surfaces")
	}
}
