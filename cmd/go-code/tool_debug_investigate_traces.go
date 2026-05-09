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

	// Phase 2: fetch failed traces.
	traces, err = jaeger.FindTraces(ctx, jaegerclient.FindTracesParams{
		Service:   input.Service,
		Tags:      map[string]string{"error": "true"},
		StartTime: start,
		EndTime:   end,
		Limit:     debugInvestigateTraceLimit,
	})
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("find traces: %v", err))
	}
	res.Diagnostics.TracesFetched = len(traces)

	return services, traces, nil
}
