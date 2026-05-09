// cmd/go-code/tool_debug_investigate_traces.go
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
)

// runTracesPhase executes Phase 1 (Jaeger ListServices) and Phase 2 (FindTraces).
//
// Phase 1 is a hard dependency: if ListServices fails the investigation cannot
// proceed, so hardErr is returned and the caller must call
// debugInvestigateStore.Fail before returning.
//
// Phase 2 errors are soft: appended to res.Diagnostics.Warnings; execution continues.
func runTracesPhase(
	ctx context.Context,
	jaeger *jaegerclient.Client,
	input DebugInvestigateInput,
	start, end time.Time,
	res *investigate.InvestigationResult,
) (services []string, traces []jaegerclient.Trace, hardErr error) {
	// Phase 1: list services to confirm Jaeger has data for this service.
	services, err := jaeger.ListServices(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("jaeger list services: %w", err)
	}
	knownService := false
	for _, s := range services {
		if s == input.Service {
			knownService = true
			break
		}
	}
	if !knownService {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("service %q not seen by Jaeger; available: %s", input.Service, strings.Join(services, ", ")))
	}

	// Phase 2a: fetch error traces (primary signal for fault investigation).
	errorTraces, err := jaeger.FindTraces(ctx, jaegerclient.FindTracesParams{
		Service:   input.Service,
		Tags:      map[string]string{"error": "true"},
		StartTime: start,
		EndTime:   end,
		Limit:     debugInvestigateTraceLimit,
	})
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("find error traces: %v", err))
	}

	// Phase 2b: fetch recent traces unconditionally for symbol correlation.
	// Healthy services have zero error-tagged spans → without this, Phase 3
	// starves (symbols_touched=0). Error traces remain first (highest signal).
	allTraces, err := jaeger.FindTraces(ctx, jaegerclient.FindTracesParams{
		Service:   input.Service,
		StartTime: start,
		EndTime:   end,
		Limit:     debugInvestigateTraceLimit,
	})
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("find baseline traces: %v", err))
	}

	// Merge: error traces first (highest signal), then dedup-fill with non-error traces.
	// TracesFetched now reflects total traces analyzed (was: error traces only).
	seen := make(map[string]struct{}, len(errorTraces))
	traces = errorTraces
	for _, t := range errorTraces {
		seen[t.TraceID] = struct{}{}
	}
	for _, t := range allTraces {
		if _, ok := seen[t.TraceID]; !ok {
			traces = append(traces, t)
		}
	}
	res.Diagnostics.TracesFetched = len(traces)

	return services, traces, nil
}
