// cmd/go-code/tool_debug_investigate.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/promclient"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// debugInvestigateTraceLimit caps the number of traces fetched per investigation.
const debugInvestigateTraceLimit = 20

// DebugInvestigateInput is the user-facing tool input.
type DebugInvestigateInput struct {
	Service   string `json:"service" jsonschema_description:"Service name as known to Jaeger (e.g. 'go-code', 'oxpulse-chat')."`
	StartUnix int64  `json:"start_unix" jsonschema_description:"Investigation window start, unix seconds. If 0, defaults to now-15m."`
	EndUnix   int64  `json:"end_unix" jsonschema_description:"Investigation window end, unix seconds. If 0, defaults to now."`
	Hint      string `json:"hint,omitempty" jsonschema_description:"Optional free-text hint about the suspected behaviour."`
	Repo      string `json:"repo,omitempty" jsonschema_description:"Repo path for symbol lookup. Defaults to the service's resolved repo when known."`
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
		}
	}

	// Fresh — kick off background goroutine.
	go runInvestigation(input, deps, jaeger, start, end)

	return textResult(fmt.Sprintf("Investigation started for service=%q range=[%s, %s]. Re-run this call in 30s to fetch the result.",
		input.Service, start.Format(time.RFC3339), end.Format(time.RFC3339))), nil
}

func runInvestigation(input DebugInvestigateInput, deps analyze.Deps, jaeger *jaegerclient.Client, start, end time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res := &investigate.InvestigationResult{
		Service:   input.Service,
		Range:     investigate.TimeRange{Start: start, End: end},
		StartedAt: time.Now(),
	}

	// Phase 1: list services to confirm Jaeger has data for this service.
	services, err := jaeger.ListServices(ctx)
	if err != nil {
		debugInvestigateStore.Fail(input.Service, start, end, fmt.Sprintf("jaeger list services: %v", err))
		return
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
	traces, err := jaeger.FindTraces(ctx, jaegerclient.FindTracesParams{
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

	// Phase 3: count unique operations across all failed spans.
	ops := map[string]int{}
	for _, tr := range traces {
		for _, sp := range tr.Spans {
			ops[sp.OperationName]++
			res.Diagnostics.SpansAnalyzed++
		}
	}

	// Phase 3: span → operation → symbol correlation.
	//
	// For each unique operation we attempt to extract a Go function name and
	// resolve it against the repo's symbol table. Successful resolutions
	// produce a Hypothesis with file:line; unresolved operations remain
	// Hypotheses with empty File (still useful — caller sees "operation X
	// failed N times even though no symbol matched").
	repo := input.Repo
	if repo != "" {
		resolvedRoot, cleanup, resolveErr := resolveRoot(ctx, repo, "", deps)
		if resolveErr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				fmt.Sprintf("resolve root %q: %v", repo, resolveErr))
		} else {
			defer cleanup()
			cg, cgErr := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
				Root:     resolvedRoot,
				Language: "go",
			})
			if cgErr != nil {
				res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
					fmt.Sprintf("build callgraph: %v", cgErr))
			}
			for op, count := range ops {
				funcName := investigate.OperationToFuncName(op)
				h := investigate.Hypothesis{
					Subject:       fmt.Sprintf("operation %q", op),
					SpanCount:     count,
					AnomalyScore:  0.5,
					EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
				}
				if cg != nil && funcName != "" {
					matches := compound.FindSymbol(cg.Symbols, funcName)
					if len(matches) > 0 {
						sym := matches[0]
						h.File = reverseToHost(sym.File, deps.PathMappings)
						h.Line = int(sym.StartLine)
						h.Subject = fmt.Sprintf("%s in %s", funcName, h.File)
						h.NextChecks = append(h.NextChecks,
							fmt.Sprintf("understand symbol=%q repo=%q", funcName, repo))
						res.Diagnostics.SymbolsTouched++
					}
				}
				res.Hypotheses = append(res.Hypotheses, h)
			}
		}
	}

	if len(res.Hypotheses) == 0 {
		// No symbol resolution (empty repo or no callgraph) — fall back to
		// frequency-only hypotheses so callers always get something useful.
		for op, count := range ops {
			res.Hypotheses = append(res.Hypotheses, investigate.Hypothesis{
				Subject:       fmt.Sprintf("operation %q", op),
				SpanCount:     count,
				AnomalyScore:  0.5,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
			})
		}
	}

	res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	res.FinishedAt = time.Now()

	debugInvestigateStore.Finish(input.Service, start, end, res)
}

// formatInvestigationResult renders the result as XML for the MCP caller.
func formatInvestigationResult(r *investigate.InvestigationResult) string {
	var b strings.Builder
	b.WriteString(`<response tool="debug_investigate">`)
	b.WriteString("\n  ")
	b.WriteString(fmt.Sprintf(`<investigation service=%q started_at=%q finished_at=%q>`,
		r.Service, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339)))

	if r.LLMSummary != "" {
		b.WriteString("\n    <summary>")
		b.WriteString(escapeXML(r.LLMSummary))
		b.WriteString("</summary>")
	}

	for i, h := range r.Hypotheses {
		b.WriteString(fmt.Sprintf("\n    <hypothesis rank=\"%d\" confidence=%q>", i+1, h.Confidence))
		b.WriteString("\n      <subject>")
		b.WriteString(escapeXML(h.Subject))
		b.WriteString("</subject>")
		if h.File != "" {
			b.WriteString(fmt.Sprintf("\n      <location file=%q line=\"%d\"/>", h.File, h.Line))
		}
		b.WriteString(fmt.Sprintf("\n      <signals span_count=\"%d\" anomaly_score=\"%.3f\"/>",
			h.SpanCount, h.AnomalyScore))
		for _, link := range h.EvidenceLinks {
			b.WriteString("\n      <evidence>")
			b.WriteString(escapeXML(link))
			b.WriteString("</evidence>")
		}
		for _, nc := range h.NextChecks {
			b.WriteString("\n      <next_check>")
			b.WriteString(escapeXML(nc))
			b.WriteString("</next_check>")
		}
		b.WriteString("\n    </hypothesis>")
	}

	d, _ := json.Marshal(r.Diagnostics)
	b.WriteString("\n    <diagnostics>")
	b.WriteString(string(d))
	b.WriteString("</diagnostics>")

	b.WriteString("\n  </investigation>")
	b.WriteString("\n</response>")
	return b.String()
}
