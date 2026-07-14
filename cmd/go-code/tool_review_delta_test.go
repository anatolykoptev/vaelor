package main

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/review"
)

// buildLargeImpacted returns n synthetic impacted symbols with varied
// distance/confidence so ranking behaviour is actually exercised (not just
// count truncation on an already-uniform slice).
func buildLargeImpacted(n int) []review.ImpactedSymbol {
	out := make([]review.ImpactedSymbol, n)
	for i := 0; i < n; i++ {
		out[i] = review.ImpactedSymbol{
			Name:       fmt.Sprintf("Symbol%03d", i),
			File:       fmt.Sprintf("pkg/file%03d.go", i%40),
			Distance:   1 + i%5, // distances 1..5, cycling
			Confidence: float64(100-i%100) / 100.0,
			ChangedBy:  "ChangedFn",
		}
	}
	return out
}

// TestCapImpactedSymbols_DefaultCapsTo50WithHonestMarker builds a 300-entry
// impacted list (the shape #391 dogfooding hit on a real multi-day delta) and
// asserts the default response caps to maxReviewImpacted, reports the TRUE
// total (300, not the capped count), and marks truncated=true — nothing is
// silently dropped.
func TestCapImpactedSymbols_DefaultCapsTo50WithHonestMarker(t *testing.T) {
	result := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(300),
		Risk:            review.RiskGuidance{RiskLevel: "medium"},
	}
	resp := buildDeltaXML(result)

	capped := capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, false)

	if capped.Total != 300 {
		t.Fatalf("Total = %d, want 300 (true count, unaffected by capping)", capped.Total)
	}
	if capped.Shown != maxReviewImpacted {
		t.Fatalf("Shown = %d, want %d", capped.Shown, maxReviewImpacted)
	}
	if len(capped.Symbols) != maxReviewImpacted {
		t.Fatalf("len(Symbols) = %d, want %d", len(capped.Symbols), maxReviewImpacted)
	}
	if !capped.Truncated {
		t.Fatal("Truncated = false, want true (300 > 50)")
	}

	// Ranking check: the shown entries must be the most meaningful ones —
	// distance 1 (closest) sorted ahead of any higher-distance entry, and
	// within equal distance, higher confidence first. A naive source-order
	// prefix would NOT guarantee this since input cycles distance 1..5.
	for i := 1; i < len(capped.Symbols); i++ {
		prev, cur := capped.Symbols[i-1], capped.Symbols[i]
		if prev.Distance > cur.Distance {
			t.Fatalf("entries not distance-ranked: [%d].Distance=%d > [%d].Distance=%d", i-1, prev.Distance, i, cur.Distance)
		}
		if prev.Distance == cur.Distance && prev.Confidence < cur.Confidence {
			t.Fatalf("entries not confidence-ranked within equal distance: [%d].Confidence=%v < [%d].Confidence=%v",
				i-1, prev.Confidence, i, cur.Confidence)
		}
	}

	// The XML itself must carry the marker as attributes on <impacted_symbols>,
	// not bury it in prose — a consumer parsing XML must see it structurally.
	resp.ImpactedSymbols = capped
	data, err := xml.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `total="300"`) {
		t.Errorf("marshaled XML missing total=\"300\": %s", firstN(out, 400))
	}
	if !strings.Contains(out, `shown="50"`) {
		t.Errorf("marshaled XML missing shown=\"50\": %s", firstN(out, 400))
	}
	if !strings.Contains(out, `truncated="true"`) {
		t.Errorf("marshaled XML missing truncated=\"true\": %s", firstN(out, 400))
	}
}

// TestCapImpactedSymbols_FullImpactReturnsComplete verifies the opt-in
// full_impact path (full=true) returns every entry, uncapped, with
// Truncated=false — the escape hatch the default cap must not foreclose.
func TestCapImpactedSymbols_FullImpactReturnsComplete(t *testing.T) {
	result := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(300),
	}
	resp := buildDeltaXML(result)

	full := capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, true)

	if full.Total != 300 {
		t.Fatalf("Total = %d, want 300", full.Total)
	}
	if full.Shown != 300 {
		t.Fatalf("Shown = %d, want 300 (uncapped)", full.Shown)
	}
	if len(full.Symbols) != 300 {
		t.Fatalf("len(Symbols) = %d, want 300", len(full.Symbols))
	}
	if full.Truncated {
		t.Fatal("Truncated = true, want false — full_impact must not report truncation")
	}

	// Every distinct symbol name from the input must be present — full means
	// full, not "capped at some other, larger number".
	seen := make(map[string]bool, len(full.Symbols))
	for _, s := range full.Symbols {
		seen[s.Name] = true
	}
	for i := 0; i < 300; i++ {
		name := fmt.Sprintf("Symbol%03d", i)
		if !seen[name] {
			t.Fatalf("full_impact dropped %s — opt-in list must be complete", name)
		}
	}
}

// TestCapImpactedSymbols_SmallListNotTruncated guards against a cap that
// fires even when there's nothing to truncate.
func TestCapImpactedSymbols_SmallListNotTruncated(t *testing.T) {
	result := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(5),
	}
	resp := buildDeltaXML(result)

	capped := capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, false)
	if capped.Truncated {
		t.Fatal("Truncated = true for a 5-entry list under the 50 cap")
	}
	if capped.Total != 5 || capped.Shown != 5 || len(capped.Symbols) != 5 {
		t.Fatalf("got Total=%d Shown=%d len=%d, want all 5", capped.Total, capped.Shown, len(capped.Symbols))
	}
}

// TestBuildDeltaXML_ImpactedSymbolsCarriesFullBaseline verifies buildDeltaXML
// itself (used by both review_delta and review_pr) reports an honest,
// uncapped Total/Shown baseline — review_pr's dry-run path marshals this
// directly without calling capImpactedSymbols, so it must not silently
// under-report.
func TestBuildDeltaXML_ImpactedSymbolsCarriesFullBaseline(t *testing.T) {
	result := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(120),
	}
	resp := buildDeltaXML(result)
	if resp.ImpactedSymbols.Total != 120 {
		t.Fatalf("baseline Total = %d, want 120", resp.ImpactedSymbols.Total)
	}
	if resp.ImpactedSymbols.Shown != 120 {
		t.Fatalf("baseline Shown = %d, want 120", resp.ImpactedSymbols.Shown)
	}
	if resp.ImpactedSymbols.Truncated {
		t.Fatal("baseline Truncated = true, want false (buildDeltaXML never caps)")
	}
	if len(resp.ImpactedSymbols.Symbols) != 120 {
		t.Fatalf("len(Symbols) = %d, want 120", len(resp.ImpactedSymbols.Symbols))
	}
}

// TestSizeReduction_BeforeAfterDemo renders the SAME 300-impacted-symbol
// synthetic delta with the old shape (full_impact=true, i.e. what every
// response looked like before this change) vs the new default (capped), and
// logs the char-count drop. Not a pass/fail assertion beyond "after < before"
// — it exists to make the #391 size reduction visible in `go test -v` output.
func TestSizeReduction_BeforeAfterDemo(t *testing.T) {
	result := &review.DeltaResult{
		ChangedFiles: []review.FileDiff{
			{Path: "internal/review/delta.go", Added: 40, Removed: 12},
		},
		ImpactedSymbols: buildLargeImpacted(300),
		Risk:            review.RiskGuidance{RiskLevel: "medium", RiskScore: 0.5},
	}
	resp := buildDeltaXML(result)

	before := resp
	before.ImpactedSymbols = capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, true) // full_impact=true == pre-#391 shape
	beforeData, err := xml.Marshal(before)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	after := resp
	after.ImpactedSymbols = capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, false) // new default
	afterData, err := xml.Marshal(after)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	beforeLen, afterLen := len(beforeData), len(afterData)
	t.Logf("#391 size demo (300 impacted_symbols): before=%d chars, after=%d chars, drop=%d chars (%.0f%%)",
		beforeLen, afterLen, beforeLen-afterLen, 100*float64(beforeLen-afterLen)/float64(beforeLen))

	if afterLen >= beforeLen {
		t.Fatalf("expected capped default (%d) < full (%d)", afterLen, beforeLen)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
