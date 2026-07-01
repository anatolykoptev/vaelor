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
// It asserts that formatInvestigationResult produces byte-identical output
// before and after the SRP refactor.
//
// RED guarantee: reverting the production function changes the output → test fails.
// The want literal was captured by running GENERATE_GOLDEN=1 against the
// unmodified function and then hard-coded here.
//
// Key encoding note: the spike labels attribute value uses fmt.Sprintf %q which
// wraps the string in Go-style quoting. The `\"` sequences inside are literal
// backslash+quote bytes in the output — not XML entity escapes. This is preserved
// exactly by the refactor (no change to encoding logic).
func TestFormatInvestigationResult_GoldenByteIdentical(t *testing.T) {
	// Raw string literal: no escaping needed. The \"  sequences inside (on the
	// <spike labels=...> line) are the literal two-byte sequences backslash+quote
	// that the current function emits via fmt.Sprintf(..., s.Labels, ...) with %q.
	//
	// pr-review-council #261: the <recent_change>/<body_excerpt> CDATA payloads
	// below are the raw diff/body-source content — an intentional fidelity fix.
	// The previous version wrapped each CDATA in a formatter-injected leading
	// newline and trailing newline+indent (part of the CDATA payload per XML,
	// not structural whitespace); this golden now encodes the raw source
	// content deterministically, with no formatter whitespace inside the
	// <![CDATA[ ... ]]> markers.
	const want = "<response tool=\"debug_investigate\"><investigation service=\"payments-svc\" hint_kind=\"latency\" started_at=\"2026-01-15T10:00:00Z\" finished_at=\"2026-01-15T10:00:05Z\"><summary>Payment handler shows elevated p99 latency due to DB pool exhaustion.</summary><hypothesis rank=\"1\" confidence=\"high\" source=\"span\"><subject>HandlePayment in payments/handler.go</subject><location file=\"payments/handler.go\" line=\"42\"/><signals span_count=\"120\" anomaly_score=\"0.870\"/><impact direct_callers=\"5\" total_affected=\"42\" blast_radius=\"medium\" risk_score=\"8.75\"/><symbol_body error_exits=\"3\" has_defer=\"true\" has_todo=\"true\"/><fused_score value=\"0.712\"><signal name=\"metric_anomaly\" score=\"0.850\"/><signal name=\"recency\" score=\"0.600\"/><signal name=\"complexity\" score=\"0.400\"/><signal name=\"impact\" score=\"0.550\"/><signal name=\"historical\" score=\"0.200\"/></fused_score><recent_change file=\"payments/handler.go\" since=\"2026-01-01\"><![CDATA[--- a/payments/handler.go\n+++ b/payments/handler.go\n@@ -42,6 +42,7 @@\n+\tlog.Info(\"payment processed\")\n]]></recent_change><body_excerpt file=\"payments/handler.go\" lines=\"42-85\"><![CDATA[func HandlePayment(ctx context.Context, req *PaymentRequest) error {\n\tif req == nil {\n\t\treturn ErrNilRequest\n\t}\n\treturn db.Process(ctx, req)\n}]]></body_excerpt><evidence>https://traces.example.com/trace/abc123</evidence><evidence>https://logs.example.com/query?q=payment+error</evidence><next_check tool=\"understand\"><arg name=\"repo\">/src/payments</arg><arg name=\"symbol\">HandlePayment</arg></next_check><next_check tool=\"code_health\"/></hypothesis><hypothesis rank=\"2\" confidence=\"medium\"><subject>ProcessRefund in payments/refund.go</subject><location file=\"payments/refund.go\" line=\"10\"/><signals span_count=\"30\" anomaly_score=\"0.450\"/></hypothesis><metric_spikes><spike kind=\"latency\" metric=\"http_request_duration_seconds\" labels=\"{service=\\\"payments\\\",handler=\\\"pay\\\"}\" ratio=\"4.20\" score=\"0.800\"/></metric_spikes><alert_violations><alert_violation alertname=\"HighLatency\" severity=\"critical\" service=\"payments-svc\" active_at=\"2026-01-15T09:55:00Z\">P99 latency exceeds 2s threshold for payments handler.</alert_violation></alert_violations><log_excerpts><line ts=\"2026-01-15T10:00:01Z\" level=\"error\">db pool exhausted: max connections reached</line></log_excerpts><historical_incidents><incident repo=\"anatolykoptev/payments\" symbol=\"HandlePayment\" risk_level=\"high\" flag=\"repeated_failure\">This function caused P0 incident on 2025-12-01.</incident></historical_incidents><diagnostics>{\"metrics_queried\":12,\"traces_fetched\":8,\"spans_analyzed\":150,\"symbols_touched\":5,\"alerts_queried\":3,\"logs_fetched\":100}</diagnostics></investigation></response>"

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
