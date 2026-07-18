package compare

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestDocRatioJSExported verifies that a JS function with a lowercase name
// (exported by JS convention) with a DocComment is counted toward docRatio.
// Before fix: isExported("removeBackground") = false → docRatio=0 (RED).
// After fix: language-aware check → docRatio=1.0 (GREEN).
func TestDocRatioJSExported(t *testing.T) {
	syms := []*parser.Symbol{
		{
			Name:       "removeBackground",
			Language:   "javascript",
			DocComment: "removes background from the image",
			Kind:       parser.KindFunction,
		},
	}

	ratio := computeDocRatio(syms)
	if ratio != 1.0 {
		t.Errorf("want docRatio=1.0 for JS exported func with doccomment, got %f", ratio)
	}
}
