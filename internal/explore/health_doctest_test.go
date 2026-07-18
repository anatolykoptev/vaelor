package explore

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestCollectSymbolMetrics_JSDocPath verifies that a JS-language symbol with a
// non-underscore camelCase name and a DocComment is counted as exported and
// documented by collectSymbolMetrics.  This pins the JS path of
// langutil.IsExportedForDoc routed through collectSymbolMetrics.
//
// Red-on-revert: reverting the "javascript"/"typescript" case in IsExportedForDoc
// to an uppercase-first check makes isPublic=false + lowercase JS name invisible
// → exportedCount stays 0, documentedCount stays 0, failing both assertions.
func TestCollectSymbolMetrics_JSDocPath(t *testing.T) {
	t.Parallel()
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

	sm := collectSymbolMetrics([]*parser.Symbol{sym})
	if sm.exportedCount != 1 {
		t.Errorf("exportedCount = %d, want 1: JS camelCase symbol must be counted as exported", sm.exportedCount)
	}
	if sm.documentedCount != 1 {
		t.Errorf("documentedCount = %d, want 1: documented JS symbol must be counted", sm.documentedCount)
	}
}

// TestCollectSymbolMetrics_RustIsPublic verifies that a Rust symbol with
// IsPublic=true and a snake_case name (e.g. "build_graph") is counted as
// exported and documented when it has a DocComment.  This pins the IsPublic
// early-return path in langutil.IsExportedForDoc.
//
// Red-on-revert: removing the IsPublic early-return in IsExportedForDoc causes
// "build_graph" (lowercase, Rust) to fall through to the non-underscore check
// and still pass (build_graph does not start with '_') — so a plain IsPublic
// revert is NOT the critical path to test here.  Instead this test pins that
// a Rust pub fn with a DocComment increments both counters regardless of the
// name-case, which holds iff IsPublic is honoured before any name check.
// Use a deliberately underscore-prefixed name to make the IsPublic gate
// the only path that classifies it as exported.
func TestCollectSymbolMetrics_RustIsPublicUnderscore(t *testing.T) {
	t.Parallel()
	// "_internal_helper" starts with '_', so name-based checks would return
	// not-exported.  Only the IsPublic=true early-return makes it exported.
	sym := &parser.Symbol{
		Name:       "_internal_helper",
		Kind:       parser.KindFunction,
		Language:   "rust",
		IsPublic:   true, // pub fn — explicit visibility overrides name heuristic
		DocComment: "// _internal_helper does X.",
		StartLine:  1,
		EndLine:    20,
		Complexity: 2,
		File:       "/repo/src/lib.rs",
	}

	sm := collectSymbolMetrics([]*parser.Symbol{sym})
	if sm.exportedCount != 1 {
		t.Errorf("exportedCount = %d, want 1: Rust pub fn must be exported even with underscore name", sm.exportedCount)
	}
	if sm.documentedCount != 1 {
		t.Errorf("documentedCount = %d, want 1: documented Rust pub fn must be counted", sm.documentedCount)
	}
}
