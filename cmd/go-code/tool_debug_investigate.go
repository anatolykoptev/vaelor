// cmd/go-code/tool_debug_investigate.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/promclient"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// debugInvestigateTraceLimit caps the number of traces fetched per investigation.
const debugInvestigateTraceLimit = 20

// Anomaly score buckets — highest to lowest.
const (
	scoreCritical      = 1.0 // ratio > 5x baseline
	scoreElevated      = 0.8 // ratio > 2x baseline
	scoreBaselineEmpty = 0.7 // baseline empty, window has errors
	scoreMild          = 0.6 // ratio > 1.2x baseline
	scoreDefault       = 0.5 // metric data missing or both queries failed
	scoreNominal       = 0.3 // ratio close to baseline (default healthy)
)

// Anomaly ratio thresholds — Prometheus window/baseline comparisons.
const (
	ratioCritical = 5.0
	ratioElevated = 2.0
	ratioMild     = 1.2
)

// HintKind disambiguates investigation focus when caller knows the bug class.
// Empty value preserves current "auto-detect everything" behavior.
type HintKind string

const (
	HintKindAuto                  HintKind = ""
	HintKindFrontendReactiveCycle HintKind = "frontend_reactive_cycle"
	HintKindPanicAtHandler        HintKind = "panic_at_handler"
	HintKindMetricSpikeUnknown    HintKind = "metric_spike_unknown_source"
	HintKindLatencySpike          HintKind = "latency_spike"
)

// IsValid reports whether k is a known HintKind value.
func (k HintKind) IsValid() bool {
	switch k {
	case HintKindAuto, HintKindFrontendReactiveCycle, HintKindPanicAtHandler, HintKindMetricSpikeUnknown, HintKindLatencySpike:
		return true
	}
	return false
}

// DebugInvestigateInput is the user-facing tool input.
type DebugInvestigateInput struct {
	Service   string   `json:"service" jsonschema_description:"Service name as known to Jaeger (e.g. 'go-code', 'oxpulse-chat')."`
	StartUnix int64    `json:"start_unix" jsonschema_description:"Investigation window start, unix seconds. If 0, defaults to now-15m."`
	EndUnix   int64    `json:"end_unix" jsonschema_description:"Investigation window end, unix seconds. If 0, defaults to now."`
	Hint      string   `json:"hint,omitempty" jsonschema_description:"Optional free-text hint about the suspected behaviour."`
	HintKind  HintKind `json:"hint_kind,omitempty" jsonschema_description:"Optional structured hint kind: frontend_reactive_cycle | panic_at_handler | metric_spike_unknown_source | latency_spike. Empty = auto-detect (default)."`
	Repo      string   `json:"repo,omitempty" jsonschema_description:"Repo path for symbol lookup. Defaults to the service's resolved repo when known."`
}

// debugInvestigateStore is module-scoped — survives across calls in the same process.
var debugInvestigateStore = investigate.NewInvestigationStore()

func registerDebugInvestigate(server *mcp.Server, cfg Config, deps analyze.Deps) {
	if cfg.PrometheusURL == "" || cfg.JaegerURL == "" {
		slog.Warn("debug_investigate: not registering — PROMETHEUS_URL or JAEGER_URL empty")
		return
	}

	prom := promclient.NewClient(cfg.PrometheusURL, 30*time.Second)
	jaeger := jaegerclient.NewClient(cfg.JaegerURL, 30*time.Second)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "debug_investigate",
		Description: "Correlate Prometheus metrics + Jaeger failed traces + code symbols to suggest the likely buggy file:function for the given service+window. Long-running (5min budget); poll same input to fetch result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DebugInvestigateInput) (*mcp.CallToolResult, error) {
		return handleDebugInvestigate(ctx, input, deps, prom, jaeger)
	})
}

func handleDebugInvestigate(ctx context.Context, input DebugInvestigateInput, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client) (*mcp.CallToolResult, error) {

	if !input.HintKind.IsValid() {
		return errResult(fmt.Sprintf("invalid hint_kind %q; expected one of: frontend_reactive_cycle, panic_at_handler, metric_spike_unknown_source, latency_spike", input.HintKind)), nil
	}

	if input.Service == "" {
		return errResult("service is required"), nil
	}

	now := time.Now()
	start := time.Unix(input.StartUnix, 0)
	end := time.Unix(input.EndUnix, 0)
	if input.StartUnix == 0 {
		start = now.Add(-15 * time.Minute)
	}
	if input.EndUnix == 0 {
		end = now
	}
	if !end.After(start) {
		return errResult("end must be after start"), nil
	}

	// Lifecycle dedup.
	st, fresh := debugInvestigateStore.Start(input.Service, start, end)
	if !fresh {
		switch st.Status() {
		case investigate.StatusRunning:
			return textResult(fmt.Sprintf("Investigation in progress for %q (started %s). Re-run this call in 30s to fetch the result.",
				input.Service, st.StartedAt().Format(time.RFC3339))), nil
		case investigate.StatusDone:
			return textResult(formatInvestigationResult(st.Result())), nil
		case investigate.StatusFailed:
			return errResult(fmt.Sprintf("Previous investigation failed: %s", st.Error())), nil
		default:
			return errResult(fmt.Sprintf("unknown status: %v", st.Status())), nil
		}
	}

	// Fresh — kick off background goroutine.
	go runInvestigation(input, deps, prom, jaeger, start, end)

	return textResult(fmt.Sprintf("Investigation started for service=%q range=[%s, %s]. Re-run this call in 30s to fetch the result.",
		input.Service, start.Format(time.RFC3339), end.Format(time.RFC3339))), nil
}

func runInvestigation(input DebugInvestigateInput, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client, start, end time.Time) {
	finished := false
	defer func() {
		if r := recover(); r != nil {
			slog.Error("debug_investigate panic", "service", input.Service, "recover", r)
			debugInvestigateStore.Fail(input.Service, start, end, fmt.Sprintf("panic: %v", r))
			return
		}
		if !finished {
			// Covers any future early-return that misses Finish/Fail.
			debugInvestigateStore.Fail(input.Service, start, end, "investigation did not complete")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// TODO(phase-beta): route to specialized data sources per hint_kind
	res := &investigate.InvestigationResult{
		Service:   input.Service,
		Range:     investigate.TimeRange{Start: start, End: end},
		StartedAt: time.Now(),
		HintKind:  string(input.HintKind),
	}

	// Phase 1 + 2: list Jaeger services and fetch failed traces.
	services, traces, err := runTracesPhase(ctx, jaeger, input, start, end, res)
	if err != nil {
		debugInvestigateStore.Fail(input.Service, start, end, err.Error())
		finished = true
		return
	}

	// Phase 4: query Prometheus for the error-rate ratio between the
	// investigation window and a baseline (same duration, 1h earlier).
	// The composite anomaly score weights metric-confirmed operations higher.
	anomalyScore, spikes := computeAnomalyScore(ctx, prom, input.Service, start, end, &res.Diagnostics)
	res.MetricSpikes = spikes

	// Phase 3: span → symbol correlation.
	ops := runSymbolsPhase(ctx, deps, input, traces, anomalyScore, res)

	// Phase 5: LLM correlate — produce one-paragraph summary + reasoning.
	runLLMPhase(ctx, deps, prom, input, services, ops, start, end, res)

	res.FinishedAt = time.Now()

	debugInvestigateStore.Finish(input.Service, start, end, res)
	finished = true
}
