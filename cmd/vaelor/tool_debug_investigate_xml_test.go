package main

import (
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/investigate"
)

// benignInvestigationFixture exercises most branches of the formatter with
// values that contain NO XML-hostile characters (<, &, "), so the
// pre-migration hand-rolled output is itself well-formed and decodable -- the
// prerequisite for a decode-both structural-equivalence comparison.
func benignInvestigationFixture() *investigate.InvestigationResult {
	t0 := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 12, 0, 3, 0, time.UTC)
	return &investigate.InvestigationResult{
		Service:    "cache-svc",
		StartedAt:  t0,
		FinishedAt: t1,
		HintKind:   "saturation",
		LLMSummary: "Cache eviction rate elevated after deploy.",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject:      "EvictLoop in cache/evict.go",
				File:         "cache/evict.go",
				Line:         30,
				EndLine:      60,
				SpanCount:    42,
				AnomalyScore: 0.5,
				Confidence:   "medium",
				Source:       "span",
				Impact: &investigate.ImpactInfo{
					DirectCallers: 2,
					TotalAffected: 9,
					BlastRadius:   "low",
					RiskScore:     3.5,
				},
				SymbolBody: &investigate.SymbolBodyInfo{
					ErrorExits:      1,
					HasDeferCleanup: true,
					HasTODO:         false,
				},
				FusedScore: 0.4,
				SignalBreakdown: map[string]float64{
					fusionSigMetricAnomaly: 0.5,
					fusionSigRecency:       0.3,
				},
				RecentChange: &investigate.RecentChange{
					File:  "cache/evict.go",
					Since: "2026-01-20",
					Diff:  "@@ -1 +1 @@\n-old\n+new\n",
				},
				BodySource: "func EvictLoop() {\n\treturn\n}",
				EvidenceLinks: []string{
					"https://traces.example.com/abc",
				},
				NextChecks: []investigate.NextCheck{
					{
						Tool: "understand",
						Args: map[string]string{
							"repo":   "/src/cache",
							"symbol": "EvictLoop",
						},
					},
					{Tool: "code_health"},
				},
			},
			{
				Subject:      "Warmup in cache/warm.go",
				File:         "cache/warm.go",
				Line:         5,
				SpanCount:    10,
				AnomalyScore: 0.2,
				Confidence:   "low",
			},
		},
		MetricSpikes: []investigate.MetricSpike{
			{Kind: "saturation", MetricName: "cache_evictions_total", Labels: "{service=cache}", Ratio: 3.0, Score: 0.7},
		},
		AlertViolations: []investigate.AlertViolation{
			{AlertName: "HighEvict", Severity: "warning", Service: "cache-svc", Summary: "evict rate high", ActiveAt: "2026-02-01T11:55:00Z"},
		},
		LogExcerpts: []investigate.LogExcerpt{
			{Ts: "2026-02-01T12:00:01Z", Level: "warn", Msg: "eviction spike"},
		},
		HistoricalIncidents: []investigate.HistoricalIncident{
			{Repo: "anatolykoptev/cache", Symbol: "EvictLoop", RiskLevel: "medium", Flag: "repeat", Note: "prior evict incident"},
		},
		Diagnostics: investigate.Diagnostics{
			MetricsQueried: 3, TracesFetched: 2, SpansAnalyzed: 20, SymbolsTouched: 2, AlertsQueried: 1, LogsFetched: 10,
		},
	}
}

// hostileInvestigationFixture carries XML-hostile characters where the prior
// formatter used %q on an attribute -- notably the <spike labels> attribute,
// which holds a Prometheus label set full of double-quotes. The pre-migration
// output for this fixture is MALFORMED XML.
func hostileInvestigationFixture() *investigate.InvestigationResult {
	t0 := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 12, 0, 3, 0, time.UTC)
	return &investigate.InvestigationResult{
		Service:    "pay-svc",
		StartedAt:  t0,
		FinishedAt: t1,
		Hypotheses: []investigate.Hypothesis{
			{Subject: "Handle<x> & retry", File: "a.go", Line: 1, SpanCount: 1, AnomalyScore: 0.1, Confidence: "high"},
		},
		MetricSpikes: []investigate.MetricSpike{
			{Kind: "latency", MetricName: "http_req_duration", Labels: `{service="payments",code="500"}`, Ratio: 2.0, Score: 0.5},
		},
		Diagnostics: investigate.Diagnostics{},
	}
}

// TestDebugInvestigate_StructurallyEquivalentToBaseline proves the migrated
// (xml.Marshal) output is structurally identical to the recorded pre-migration
// output for a benign fixture: same elements, nesting, attributes and
// text/CDATA content. Serialization-only differences (self-closing <x/> vs
// long-form <x></x>) are normalized away by the decoder.
func TestDebugInvestigate_StructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "debug_benign_current.xml")
	migrated := formatInvestigationResult(benignInvestigationFixture())
	assertXMLEquivalent(t, current, migrated)
}

// TestDebugInvestigate_BaselineHostileIsMalformed documents the BUG: the
// hand-rolled formatter emitted malformed XML for the hostile fixture (the
// <spike labels> attribute used %q, so its embedded double-quotes terminated
// the attribute value). The recorded baseline does not decode.
func TestDebugInvestigate_BaselineHostileIsMalformed(t *testing.T) {
	assertNotWellFormed(t, readGolden(t, "debug_hostile_current.xml"))
}

// TestDebugInvestigate_HostileAttrsEscaped proves the FIX: the migrated output
// for the same hostile fixture is well-formed, and the <spike labels>
// attribute round-trips to its exact original Prometheus label set.
func TestDebugInvestigate_HostileAttrsEscaped(t *testing.T) {
	migrated := formatInvestigationResult(hostileInvestigationFixture())
	assertAttrRoundTrips(t, migrated,
		"response/investigation/metric_spikes/spike", "labels",
		`{service="payments",code="500"}`)
}
