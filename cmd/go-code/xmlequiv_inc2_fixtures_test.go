package main

import (
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/designmd"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// Shared benign fixtures for the increment-2 XML formatter equivalence goldens.
// These build INPUTS only (no reference to any migrated formatter), so the file
// compiles against both the pre-migration tree (used by the throwaway capture
// test) and the post-migration tree (used by the equivalence assertions) — the
// single source of truth for the fixture values eliminates capture/assert drift.

// benignDesignArgs returns a benign design_search input set (no <, &, or " in any
// attribute value, so the hand-rolled baseline is itself well-formed and
// decodable). Distance 0.05 -> score "0.95" exercises the %.2f pre-format.
func benignDesignArgs() (string, []brandHit, map[string]designmd.BrandMeta, []analyze.PathMapping) {
	return "minimalist dashboard",
		[]brandHit{{
			brand:    "Stripe",
			section:  "Color System",
			distance: 0.05,
			excerpt:  "Uses a calm palette of blues and greens",
			filePath: "design-md/stripe/DESIGN.md",
		}},
		map[string]designmd.BrandMeta{"Stripe": {
			Vibe:    "professional and calm",
			Colors:  []string{"blue", "green"},
			BestFor: "fintech dashboards",
		}},
		nil
}

// benignSemResults returns benign trigram-fallback results. Distance 0.25 ->
// "0.2500" exercises the %.4f trailing-zero pre-format (raw float32 marshal
// would drop to "0.25").
func benignSemResults() []embeddings.SearchResult {
	return []embeddings.SearchResult{
		{SymbolName: "renderCaddy", SymbolKind: "function", StartLine: 42, FilePath: "internal/render/caddy.go", Distance: 0.1234},
		{SymbolName: "renderWidget", SymbolKind: "function", StartLine: 88, FilePath: "internal/render/widget.go", Distance: 0.25},
	}
}
