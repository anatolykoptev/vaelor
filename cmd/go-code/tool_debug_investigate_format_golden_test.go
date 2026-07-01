// cmd/go-code/tool_debug_investigate_format_golden_test.go
// Characterization (golden) test for formatInvestigationResult.
// Exercises EVERY branch of the function; output must be byte-identical
// before and after the SRP refactor.
package main

import (
	"os"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// goldenFixture builds the richest possible InvestigationResult that exercises
// every branch in formatInvestigationResult:
//   - HintKind set                       → investigation tag includes hint_kind attribute
//   - LLMSummary                         → <summary> block rendered
//   - Hypothesis with Source             → includes source= attribute
//   - Impact non-nil, DirectCallers>0   → <impact> block rendered
//   - SymbolBody with HasDeferCleanup+HasTODO → <symbol_body> block rendered
//   - FusedScore>0, all 5 SignalBreakdown entries → <fused_score> block rendered
//   - RecentChange with non-empty Diff   → <recent_change> CDATA block rendered
//   - BodySource with EndLine>Line       → <body_excerpt> with range lines rendered
//   - EvidenceLinks                      → <evidence> elements rendered
//   - NextChecks: one with ≥2 Args (sorted order verified), one without Args
//   - Hypothesis without Source          → no source= attribute in tag
//   - MetricSpikes non-empty             → <metric_spikes> block rendered
//   - AlertViolations non-empty          → <alert_violations> block rendered
//   - LogExcerpts non-empty              → <log_excerpts> block rendered
//   - HistoricalIncidents non-empty      → <historical_incidents> block rendered
//   - Diagnostics non-zero              → JSON diagnostics block rendered
func goldenFixture() *investigate.InvestigationResult {
	t0 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 15, 10, 0, 5, 0, time.UTC)
	return &investigate.InvestigationResult{
		Service:    "payments-svc",
		StartedAt:  t0,
		FinishedAt: t1,
		HintKind:   "latency",
		LLMSummary: "Payment handler shows elevated p99 latency due to DB pool exhaustion.",
		Hypotheses: []investigate.Hypothesis{
			{
				// hypothesis 1: exercises Source, File/Line, Impact, SymbolBody,
				// FusedScore+SignalBreakdown, RecentChange, BodySource+EndLine,
				// EvidenceLinks, NextChecks with 2-key Args map.
				Subject:      "HandlePayment in payments/handler.go",
				File:         "payments/handler.go",
				Line:         42,
				EndLine:      85,
				SpanCount:    120,
				AnomalyScore: 0.87,
				Confidence:   "high",
				Source:       "span",
				Impact: &investigate.ImpactInfo{
					DirectCallers: 5,
					TotalAffected: 42,
					BlastRadius:   "medium",
					RiskScore:     8.75,
				},
				SymbolBody: &investigate.SymbolBodyInfo{
					ErrorExits:      3,
					HasDeferCleanup: true,
					HasTODO:         true,
				},
				FusedScore: 0.712,
				SignalBreakdown: map[string]float64{
					fusionSigMetricAnomaly: 0.85,
					fusionSigRecency:       0.60,
					fusionSigComplexity:    0.40,
					fusionSigImpact:        0.55,
					fusionSigHistorical:    0.20,
				},
				RecentChange: &investigate.RecentChange{
					File:  "payments/handler.go",
					Since: "2026-01-01",
					Diff:  "--- a/payments/handler.go\n+++ b/payments/handler.go\n@@ -42,6 +42,7 @@\n+\tlog.Info(\"payment processed\")\n",
				},
				BodySource: "func HandlePayment(ctx context.Context, req *PaymentRequest) error {\n\tif req == nil {\n\t\treturn ErrNilRequest\n\t}\n\treturn db.Process(ctx, req)\n}",
				EvidenceLinks: []string{
					"https://traces.example.com/trace/abc123",
					"https://logs.example.com/query?q=payment+error",
				},
				NextChecks: []investigate.NextCheck{
					{
						Tool: "understand",
						Args: map[string]string{
							// Two keys — sorted order: "repo" before "symbol"
							"repo":   "/src/payments",
							"symbol": "HandlePayment",
						},
					},
					{
						// No Args — renders as self-closing <next_check tool="code_health"/>
						Tool: "code_health",
					},
				},
			},
			{
				// hypothesis 2: no Source → tag without source= attribute
				// no Impact, SymbolBody, FusedScore, RecentChange, BodySource
				Subject:      "ProcessRefund in payments/refund.go",
				File:         "payments/refund.go",
				Line:         10,
				SpanCount:    30,
				AnomalyScore: 0.45,
				Confidence:   "medium",
			},
		},
		MetricSpikes: []investigate.MetricSpike{
			{
				Kind:       "latency",
				MetricName: "http_request_duration_seconds",
				// Labels contains quotes → %q in Sprintf produces \" in output.
				Labels: `{service="payments",handler="pay"}`,
				Ratio:  4.2,
				Score:  0.800,
			},
		},
		AlertViolations: []investigate.AlertViolation{
			{
				AlertName: "HighLatency",
				Severity:  "critical",
				Service:   "payments-svc",
				Summary:   "P99 latency exceeds 2s threshold for payments handler.",
				ActiveAt:  "2026-01-15T09:55:00Z",
			},
		},
		LogExcerpts: []investigate.LogExcerpt{
			{
				Ts:    "2026-01-15T10:00:01Z",
				Level: "error",
				Msg:   "db pool exhausted: max connections reached",
			},
		},
		HistoricalIncidents: []investigate.HistoricalIncident{
			{
				Repo:      "anatolykoptev/payments",
				Symbol:    "HandlePayment",
				RiskLevel: "high",
				Flag:      "repeated_failure",
				Note:      "This function caused P0 incident on 2025-12-01.",
			},
		},
		Diagnostics: investigate.Diagnostics{
			MetricsQueried: 12,
			TracesFetched:  8,
			SpansAnalyzed:  150,
			SymbolsTouched: 5,
			AlertsQueried:  3,
			LogsFetched:    100,
		},
	}
}

// TestFormatInvestigationResult_GenerateGolden is a helper (skipped normally)
// that writes the current output to /tmp/golden_out.txt for inspection.
// Run with: GENERATE_GOLDEN=1 go test -run TestFormatInvestigationResult_GenerateGolden -v
func TestFormatInvestigationResult_GenerateGolden(t *testing.T) {
	if os.Getenv("GENERATE_GOLDEN") == "" {
		t.Skip("set GENERATE_GOLDEN=1 to regenerate")
	}
	got := formatInvestigationResult(goldenFixture())
	if err := os.WriteFile("/tmp/golden_out.txt", []byte(got), 0644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	t.Logf("golden written to /tmp/golden_out.txt (%d bytes)", len(got))
}

// TestFormatInvestigationResult_GoldenByteIdentical is the characterization test.
// It asserts that formatInvestigationResult produces byte-identical output to the
// recorded golden in testdata/debug_golden.xml.
//
// RED guarantee: reverting the production function changes the output → test fails.
// The golden was regenerated (GENERATE_GOLDEN=1) after migrating the formatter
// onto typed structs + encoding/xml.Marshal.
//
// Encoding note: the <spike labels> attribute now renders XML entity escapes
// (e.g. &#34;) instead of the prior fmt.Sprintf %q Go-style \" quoting, which
// produced MALFORMED XML for label sets containing double-quotes. Empty elements
// serialize as long-form <x></x> (xml.Marshal never self-closes) — decoder-
// equivalent to the prior <x/> and consistent with the repo_analyze/code_compare
// seam. See tool_debug_investigate_xml_test.go for the structural-equivalence and
// escaping round-trip proofs.
func TestFormatInvestigationResult_GoldenByteIdentical(t *testing.T) {
	want := readGolden(t, "debug_golden.xml")

	got := formatInvestigationResult(goldenFixture())
	if got != want {
		// Show byte-level diff aid.
		t.Errorf("formatInvestigationResult output is not byte-identical to golden (first diff at byte %d).\n\nGOT  (%d bytes):\n%s\n\nWANT (%d bytes):\n%s",
			firstDiffByte(got, want), len(got), got, len(want), want)
	}
}

// firstDiffByte returns the index of the first byte that differs between a and b.
func firstDiffByte(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
