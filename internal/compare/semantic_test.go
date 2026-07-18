package compare

import (
	"math"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"similar", []float32{1, 1, 0}, []float32{1, 0.9, 0.1}, 0.98},
		{"empty", nil, nil, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 0.05 {
				t.Errorf("cosineSimilarity = %.3f, want ~%.3f", got, tt.want)
			}
		})
	}
}

func TestFilterEmbeddable(t *testing.T) {
	syms := []*parser.Symbol{
		{Name: "Foo", Kind: "function", Body: "func Foo() {}"},
		{Name: "Bar", Kind: "type", Body: "type Bar struct{}"},
		{Name: "Baz", Kind: "method", Body: "func (b *B) Baz() {}"},
		{Name: "Empty", Kind: "function", Body: ""},
	}
	result := filterEmbeddable(syms, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Name != "Foo" {
		t.Errorf("expected Foo, got %s", result[0].Name)
	}
	if result[1].Name != "Baz" {
		t.Errorf("expected Baz, got %s", result[1].Name)
	}
}

func TestFilterEmbeddableLimit(t *testing.T) {
	syms := make([]*parser.Symbol, 10)
	for i := range syms {
		syms[i] = &parser.Symbol{Name: "Fn", Kind: "function", Body: "body"}
	}
	result := filterEmbeddable(syms, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
}
