package compare

import (
	"testing"
)

func TestExtractIdentifiers(t *testing.T) {
	body := `func handleRequest(ctx context.Context, w http.ResponseWriter) {
        data := fetchData(ctx)
        w.Write(data)
    }`
	ids := extractIdentifiers(body)
	if len(ids) == 0 {
		t.Fatal("expected identifiers")
	}
	// Should contain: handleRequest, ctx, context, Context, etc.
	// Should NOT contain: short tokens like "w" (< 3 chars)
	if _, ok := ids["handleRequest"]; !ok {
		t.Error("expected handleRequest")
	}
	if _, ok := ids["w"]; ok {
		t.Error("should not contain single-char 'w'")
	}
}

func TestTokenOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		min  float64
		max  float64
	}{
		{"identical", "func foo(x int) { return x + 1 }", "func foo(x int) { return x + 1 }", 0.99, 1.01},
		{"similar", "func foo(ctx context.Context) { return fetchData(ctx) }", "func bar(ctx context.Context) { return getData(ctx) }", 0.3, 0.95},
		{"different", "func encode(data []byte) string { base64 }", "func listen(port int) error { accept }", 0.0, 0.3},
		{"empty", "", "func foo() {}", 0.0, 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := tokenOverlap(tt.a, tt.b)
			if score < tt.min || score > tt.max {
				t.Errorf("tokenOverlap = %.2f, want [%.2f, %.2f]", score, tt.min, tt.max)
			}
		})
	}
}
