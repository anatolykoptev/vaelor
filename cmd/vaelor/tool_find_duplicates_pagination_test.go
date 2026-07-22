package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/semhealth"
)

// makeDupGroups builds N groups for testing pagination.
func makeDupGroups(n int) []semhealth.DupGroup {
	groups := make([]semhealth.DupGroup, n)
	for i := range groups {
		groups[i] = semhealth.DupGroup{
			Tier: dupTierExact,
			Symbols: []semhealth.DupSymbol{
				{Name: "funcA", File: "file.go", Line: i + 1, Kind: "function"},
				{Name: "funcB", File: "file.go", Line: i + 100, Kind: "function"},
			},
			AvgSimilarity: 1.0,
		}
	}
	return groups
}

// TestFormatTriage_OffsetPagination verifies that offset skips the first N
// groups and the continuation footer shows the correct next offset.
// RED-on-revert: if offset is ignored (always starts from 0), the output
// contains group 1 instead of group 6.
func TestFormatTriage_OffsetPagination(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups:     makeDupGroups(30),
		Candidates: 30,
		ReportedByTier: map[string]int{
			dupTierExact:     30,
			dupTierVeryClose: 0,
			dupTierRelated:   0,
		},
	}

	out := formatTriage(res, "", 5, 10)

	// Should show offset header.
	if !strings.Contains(out, "offset=5") {
		t.Fatalf("output must show offset=5, got:\n%s", out)
	}
	// Should show "showing 6-15 of 30".
	if !strings.Contains(out, "showing 6-15 of 30") {
		t.Fatalf("output must show range 6-15 of 30, got:\n%s", out)
	}
	// Should have continuation footer with next offset=15.
	if !strings.Contains(out, "offset=15") {
		t.Fatalf("output must show next offset=15, got:\n%s", out)
	}
}

// TestFormatTriage_OffsetBeyondEnd verifies that offset >= total returns
// a clear "no more groups" message.
func TestFormatTriage_OffsetBeyondEnd(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups:     makeDupGroups(10),
		Candidates: 10,
		ReportedByTier: map[string]int{
			dupTierExact: 10,
		},
	}

	out := formatTriage(res, "", 20, 10)

	if !strings.Contains(out, "no more groups at offset=20") {
		t.Fatalf("output must show 'no more groups' message, got:\n%s", out)
	}
}

// TestFormatTriage_ContinuationFooterOnLimit verifies that when there are
// more groups than the limit, a continuation footer is emitted even at
// offset=0.
func TestFormatTriage_ContinuationFooterOnLimit(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups:     makeDupGroups(50),
		Candidates: 50,
		ReportedByTier: map[string]int{
			dupTierExact: 50,
		},
	}

	out := formatTriage(res, "", 0, 20)

	if !strings.Contains(out, "[truncated:") {
		t.Fatalf("output must have continuation footer when limit < total, got:\n%s", out)
	}
	if !strings.Contains(out, "offset=20") {
		t.Fatalf("footer must show next offset=20, got:\n%s", out)
	}
}

// TestFormatTriage_NoContinuationWhenAllFit verifies no footer when all
// groups fit within the limit.
func TestFormatTriage_NoContinuationWhenAllFit(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups:     makeDupGroups(5),
		Candidates: 5,
		ReportedByTier: map[string]int{
			dupTierExact: 5,
		},
	}

	out := formatTriage(res, "", 0, 20)

	if strings.Contains(out, "[truncated:") {
		t.Fatalf("output must NOT have continuation footer when all fit, got:\n%s", out)
	}
}

// TestFilterOffsetAndLimitGroups_TierFilterThenOffset verifies that tier
// filtering happens before offset/limit.
func TestFilterOffsetAndLimitGroups_TierFilterThenOffset(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, Symbols: []semhealth.DupSymbol{{Name: "a"}}},
		{Tier: dupTierVeryClose, Symbols: []semhealth.DupSymbol{{Name: "b"}}},
		{Tier: dupTierExact, Symbols: []semhealth.DupSymbol{{Name: "c"}}},
		{Tier: dupTierExact, Symbols: []semhealth.DupSymbol{{Name: "d"}}},
		{Tier: dupTierVeryClose, Symbols: []semhealth.DupSymbol{{Name: "e"}}},
		{Tier: dupTierExact, Symbols: []semhealth.DupSymbol{{Name: "f"}}},
	}

	// Filter to exact only (4 groups), then offset=1, limit=2 → groups c, d.
	page, total := filterOffsetAndLimitGroups(groups, dupTierExact, 1, 2)
	if total != 4 {
		t.Fatalf("total after tier filter = %d, want 4", total)
	}
	if len(page) != 2 {
		t.Fatalf("page len = %d, want 2", len(page))
	}
	if page[0].Symbols[0].Name != "c" {
		t.Fatalf("first page item = %s, want c", page[0].Symbols[0].Name)
	}
	if page[1].Symbols[0].Name != "d" {
		t.Fatalf("second page item = %s, want d", page[1].Symbols[0].Name)
	}
}
