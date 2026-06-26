package explore

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestCollectSymbolMetrics_JSDocPath verifies that a JS-language symbol with a
// non-underscore name and a DocComment is counted as documented (docRatio > 0).
// This pins the JS path of langutil.IsExportedForDoc routed through
// collectSymbolMetrics — the explore-package consumer of the extracted helper.
//
// Red-on-revert: removing the JS case from IsExportedForDoc (or reverting to
// an uppercase-first check) makes isPublic=false + lowercase JS name invisible
// to the exporter → exportedCount stays 0 → docRatio stays 0, failing the assertion.
func TestCollectSymbolMetrics_JSDocPath(t *testing.T) {
	// camelCase JS function: not public by IsPublic flag, not uppercase-first,
	// but should count as exported under JS rules.
	sym := &parser.Symbol{
		Name:       "renderWidget",
		Kind:       parser.KindFunction,
		Language:   "javascript",
		IsPublic:   false,
		DocComment: "renderWidget renders the widget.",
		StartLine:  1,
		EndLine:    10,
		Complexity: 1,
		File:       "/repo/widget.js",
	}
	files := []*ingest.File{
		{Path: "/repo/widget.js", RelPath: "widget.js"},
	}

	h := computeHealth([]*parser.Symbol{sym}, files)
	if h == nil {
		t.Fatal("computeHealth() = nil, want non-nil")
	}
	// docRatio = 1/1 = 1.0 → docScore = clamp(1.0/0.6) = 1.0 → score > 0
	if h.Score == 0 {
		t.Errorf("Score = 0, want > 0: JS exported symbol with DocComment must be counted")
	}
}

// TestCollectSymbolMetrics_RustIsPublic verifies that a Rust symbol with
// IsPublic=true and a lowercase name (e.g. "build_graph") is counted as
// documented when it has a DocComment.  This pins the multi-language IsPublic
// path introduced when the helper was generalised beyond Go.
//
// Red-on-revert: reverting the IsPublic early-return in IsExportedForDoc causes
// "build_graph" (lowercase, Rust language) to fall through to the uppercase-first
// check, returning false → exportedCount=0 → docRatio=0, failing the assertion.
func TestCollectSymbolMetrics_RustIsPublic(t *testing.T) {
	sym := &parser.Symbol{
		Name:       "build_graph",
		Kind:       parser.KindFunction,
		Language:   "rust",
		IsPublic:   true, // pub fn — authoritative export signal
		DocComment: "build_graph constructs the dependency graph.",
		StartLine:  1,
		EndLine:    20,
		Complexity: 2,
		File:       "/repo/src/graph.rs",
	}
	files := []*ingest.File{
		{Path: "/repo/src/graph.rs", RelPath: "src/graph.rs"},
	}

	h := computeHealth([]*parser.Symbol{sym}, files)
	if h == nil {
		t.Fatal("computeHealth() = nil, want non-nil")
	}
	// docRatio = 1/1 = 1.0 → docScore clamped to 1.0 → contributes to score
	if h.Score == 0 {
		t.Errorf("Score = 0, want > 0: Rust pub fn with DocComment must be counted as documented")
	}
}
