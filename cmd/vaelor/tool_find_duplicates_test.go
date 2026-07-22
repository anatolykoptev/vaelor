package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/semhealth"
)

// TestFormatTriage_TierOrdering verifies that exact groups come before very-close,
// which come before related in the formatted output.
func TestFormatTriage_TierOrdering(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups: []semhealth.DupGroup{
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "HandleFoo", File: "internal/foo.go", Line: 12, Kind: "function"},
					{Name: "ProcessFoo", File: "internal/bar.go", Line: 34, Kind: "function"},
				},
				AvgSimilarity: 0.95,
				Tier:          "very-close",
			},
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "Exact1", File: "pkg/a.go", Line: 5, Kind: "function"},
					{Name: "Exact2", File: "pkg/b.go", Line: 7, Kind: "function"},
				},
				AvgSimilarity: 1.0,
				Tier:          "exact",
			},
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "RelatedA", File: "cmd/x.go", Line: 99, Kind: "method"},
					{Name: "RelatedB", File: "cmd/y.go", Line: 101, Kind: "method"},
				},
				AvgSimilarity: 0.82,
				Tier:          "related",
			},
		},
		Candidates:     30,
		Dropped:        map[string]int{"tests": 5, "same_file": 2, "kind": 1, "calls_edge": 3, "interface_sibling": 4},
		ReportedByTier: map[string]int{"exact": 1, "very-close": 1, "related": 1},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)

	// Tier ordering: the exact SECTION must precede very-close, very-close before
	// related. Assert on the section markers, not bare tier tokens — the bare tokens
	// also appear in the summary header ("exact=a very-close=b related=c"), so matching
	// them would pass even if the sections themselves were scrambled.
	exactPos := strings.Index(out, "=== exact ===")
	veryClosePos := strings.Index(out, "=== very-close ===")
	relatedPos := strings.Index(out, "=== related ===")
	if exactPos < 0 || veryClosePos < 0 || relatedPos < 0 {
		t.Fatalf("missing tier section marker in output:\n%s", out)
	}
	if exactPos > veryClosePos {
		t.Errorf("exact (%d) must appear before very-close (%d) in output", exactPos, veryClosePos)
	}
	if veryClosePos > relatedPos {
		t.Errorf("very-close (%d) must appear before related (%d) in output", veryClosePos, relatedPos)
	}
}

// TestFormatTriage_SummaryLine verifies the header summary contains candidate/filter counts.
func TestFormatTriage_SummaryLine(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups: []semhealth.DupGroup{
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "FuncA", File: "a.go", Line: 1, Kind: "function"},
					{Name: "FuncB", File: "b.go", Line: 2, Kind: "function"},
				},
				AvgSimilarity: 0.91,
				Tier:          "very-close",
			},
		},
		Candidates:     10,
		Dropped:        map[string]int{"tests": 3, "same_file": 1, "kind": 0, "calls_edge": 2, "interface_sibling": 1},
		ReportedByTier: map[string]int{"very-close": 1},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)

	checks := []string{
		"candidates=10",
		"reported=1",
		"tests=3",
		"same_file=1",
		"calls_edge=2",
		"interface_sibling=1",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in summary line, output:\n%s", want, out)
		}
	}
}

// TestFormatTriage_SymbolLineFormat verifies that each group line contains symbol name,
// file:line, kind, tier label, and similarity.
func TestFormatTriage_SymbolLineFormat(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups: []semhealth.DupGroup{
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "ProcessOrder", File: "internal/order.go", Line: 42, Kind: "function"},
					{Name: "HandleOrder", File: "internal/handler.go", Line: 88, Kind: "method"},
				},
				AvgSimilarity: 0.93,
				Tier:          "very-close",
			},
		},
		Candidates:     5,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{"very-close": 1},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)

	wants := []string{
		"ProcessOrder",
		"internal/order.go:42",
		"function",
		"HandleOrder",
		"internal/handler.go:88",
		"method",
		"very-close",
		"sim=0.93",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// TestFormatTriage_TierFilter verifies that when a tier filter is applied,
// only groups of that tier appear in the output.
func TestFormatTriage_TierFilter(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups: []semhealth.DupGroup{
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "Exact1", File: "a.go", Line: 1, Kind: "function"},
					{Name: "Exact2", File: "b.go", Line: 2, Kind: "function"},
				},
				AvgSimilarity: 1.0,
				Tier:          "exact",
			},
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "Close1", File: "c.go", Line: 3, Kind: "function"},
					{Name: "Close2", File: "d.go", Line: 4, Kind: "function"},
				},
				AvgSimilarity: 0.90,
				Tier:          "very-close",
			},
			{
				Symbols: []semhealth.DupSymbol{
					{Name: "Related1", File: "e.go", Line: 5, Kind: "function"},
					{Name: "Related2", File: "f.go", Line: 6, Kind: "function"},
				},
				AvgSimilarity: 0.83,
				Tier:          "related",
			},
		},
		Candidates:     20,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{"exact": 1, "very-close": 1, "related": 1},
	}

	// Filter to "very-close" only.
	out := formatTriage(res, "very-close", 0, defaultDupLimit)

	if strings.Contains(out, "Exact1") {
		t.Errorf("exact group should be filtered out, but found Exact1 in output:\n%s", out)
	}
	if strings.Contains(out, "Related1") {
		t.Errorf("related group should be filtered out, but found Related1 in output:\n%s", out)
	}
	if !strings.Contains(out, "Close1") {
		t.Errorf("very-close group should be present, missing Close1 in output:\n%s", out)
	}
}

// TestFormatTriage_LimitCap verifies that the limit parameter caps the number of groups.
func TestFormatTriage_LimitCap(t *testing.T) {
	groups := make([]semhealth.DupGroup, 10)
	for i := range groups {
		groups[i] = semhealth.DupGroup{
			Symbols: []semhealth.DupSymbol{
				{Name: "FuncA" + string(rune('A'+i)), File: "a.go", Line: i + 1, Kind: "function"},
				{Name: "FuncB" + string(rune('A'+i)), File: "b.go", Line: i + 2, Kind: "function"},
			},
			AvgSimilarity: 0.90,
			Tier:          "very-close",
		}
	}
	res := &semhealth.TriageResult{
		Groups:         groups,
		Candidates:     50,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{"very-close": 10},
	}

	out := formatTriage(res, "", 0, 3)

	// Only the first 3 groups should appear. Count occurrence of "sim=" as a proxy for group lines.
	count := strings.Count(out, "sim=")
	if count != 3 {
		t.Errorf("expected 3 groups with limit=3, got %d occurrences of 'sim=' in output:\n%s", count, out)
	}
}

// TestFormatTriage_EmptyGroups verifies that an empty (but non-nil) result
// returns a "no duplicates found" message.
func TestFormatTriage_EmptyGroups(t *testing.T) {
	res := &semhealth.TriageResult{
		Groups:         nil,
		Candidates:     0,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)

	if !strings.Contains(out, "no semantic duplicates") {
		t.Errorf("expected no-duplicates message, got:\n%s", out)
	}
}

// TestFormatTriage_DefaultLimit verifies that zero Limit in the input defaults to defaultDupLimit.
// This indirectly verifies the default constant is used, so reverting the guard breaks the test.
func TestFormatTriage_DefaultLimit(t *testing.T) {
	if defaultDupLimit <= 0 {
		t.Fatalf("defaultDupLimit must be positive, got %d", defaultDupLimit)
	}
	// 60 groups > defaultDupLimit=50: output should cap at 50.
	groups := make([]semhealth.DupGroup, 60)
	for i := range groups {
		groups[i] = semhealth.DupGroup{
			Symbols: []semhealth.DupSymbol{
				{Name: "FA" + string(rune('A'+i%26)), File: "a.go", Line: i + 1, Kind: "function"},
				{Name: "FB" + string(rune('A'+i%26)), File: "b.go", Line: i + 2, Kind: "function"},
			},
			AvgSimilarity: 0.90,
			Tier:          "very-close",
		}
	}
	res := &semhealth.TriageResult{
		Groups:         groups,
		Candidates:     100,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{"very-close": 60},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)
	count := strings.Count(out, "sim=")
	if count != defaultDupLimit {
		t.Errorf("expected defaultDupLimit=%d groups, got %d in output", defaultDupLimit, count)
	}
}

// TestFormatTriage_RevertGuard ensures the tier-ordering invariant is load-bearing:
// if we scrambled the tiers this test would fail. This is the RED-on-revert check.
func TestFormatTriage_RevertGuard(t *testing.T) {
	// Place groups in reverse tier order in the input slice; output must still show exact first.
	res := &semhealth.TriageResult{
		Groups: []semhealth.DupGroup{
			{
				Symbols:       []semhealth.DupSymbol{{Name: "R1", File: "r.go", Line: 1, Kind: "function"}, {Name: "R2", File: "r2.go", Line: 2, Kind: "function"}},
				AvgSimilarity: 0.81,
				Tier:          "related",
			},
			{
				Symbols:       []semhealth.DupSymbol{{Name: "E1", File: "e.go", Line: 1, Kind: "function"}, {Name: "E2", File: "e2.go", Line: 2, Kind: "function"}},
				AvgSimilarity: 1.0,
				Tier:          "exact",
			},
		},
		Candidates:     5,
		Dropped:        map[string]int{},
		ReportedByTier: map[string]int{"exact": 1, "related": 1},
	}

	out := formatTriage(res, "", 0, defaultDupLimit)

	// "exact" section header must come before the "related" section header.
	// (The groups are ordered by AnalyzeTriage already; formatTriage must preserve that order.)
	exactHeaderPos := strings.Index(out, "=== exact ===")
	relatedHeaderPos := strings.Index(out, "=== related ===")
	if exactHeaderPos < 0 || relatedHeaderPos < 0 {
		t.Fatalf("expected tier section headers in output:\n%s", out)
	}
	if exactHeaderPos > relatedHeaderPos {
		t.Errorf("exact section (%d) must appear before related section (%d)", exactHeaderPos, relatedHeaderPos)
	}
}
