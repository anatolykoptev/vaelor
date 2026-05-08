// cmd/go-code/tool_debug_investigate.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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
	Service   string `json:"service" jsonschema_description:"Service name as known to Jaeger (e.g. 'go-code', 'acme-web')."`
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

	res := &investigate.InvestigationResult{
		Service:   input.Service,
		Range:     investigate.TimeRange{Start: start, End: end},
		StartedAt: time.Now(),
	}

	// Phase 1: list services to confirm Jaeger has data for this service.
	services, err := jaeger.ListServices(ctx)
	if err != nil {
		debugInvestigateStore.Fail(input.Service, start, end, fmt.Sprintf("jaeger list services: %v", err))
		finished = true
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

	// Phase 4: query Prometheus for the error-rate ratio between the
	// investigation window and a baseline (same duration, 1h earlier).
	// The composite anomaly score weights metric-confirmed operations higher.
	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	errMetricQuery := fmt.Sprintf(
		`sum(rate(http_requests_total{service=%q,code=~"5..|4.."}[1m]))`,
		input.Service)

	windowSeries, werr := prom.QueryRange(ctx, errMetricQuery, start, end, 60*time.Second)
	baseSeries, berr := prom.QueryRange(ctx, errMetricQuery, baselineStart, baselineEnd, 60*time.Second)
	res.Diagnostics.MetricsQueried = 2

	anomalyScore := 0.5 // default if metric data missing
	if werr == nil && berr == nil {
		wMax := maxSampleValue(windowSeries)
		bMax := maxSampleValue(baseSeries)
		if bMax > 0 {
			ratio := wMax / bMax
			switch {
			case ratio > 5:
				anomalyScore = 1.0
			case ratio > 2:
				anomalyScore = 0.8
			case ratio > 1.2:
				anomalyScore = 0.6
			default:
				anomalyScore = 0.3
			}
		} else if wMax > 0 {
			// Baseline empty but window has errors — modest anomaly.
			anomalyScore = 0.7
		}
	} else {
		if werr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("prom window: %v", werr))
		}
		if berr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("prom baseline: %v", berr))
		}
	}

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
					AnomalyScore:  anomalyScore,
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
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
			})
		}
	}

	res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	// Phase 5: LLM correlate — produce one-paragraph summary + reasoning for top hypothesis.
	if deps.LLM != nil && len(res.Hypotheses) > 0 {
		// Gather ground-truth context.
		availMetrics, _ := listLabelValues(ctx, prom, "__name__")
		operationsSeen := make([]string, 0, len(ops))
		for op := range ops {
			operationsSeen = append(operationsSeen, op)
		}

		sysPrompt := investigate.BuildSystemPrompt(investigate.PromptContext{
			Service:           input.Service,
			AvailableMetrics:  availMetrics,
			AvailableServices: services,
			OperationsSeen:    operationsSeen,
		})

		// Compact user-side payload: top 5 hypotheses + diagnostics + hint.
		topN := res.Hypotheses
		if len(topN) > 5 {
			topN = topN[:5]
		}
		userPayload := map[string]any{
			"service":     input.Service,
			"window":      map[string]string{"start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339)},
			"hypotheses":  topN,
			"diagnostics": res.Diagnostics,
			"user_hint":   input.Hint,
		}
		userJSON, _ := json.Marshal(userPayload)

		// Bounded LLM call (10s timeout — non-blocking on overall investigation).
		llmCtx, llmCancel := context.WithTimeout(ctx, 10*time.Second)
		defer llmCancel()
		summary, err := deps.LLM.Complete(llmCtx, sysPrompt, string(userJSON))
		if err != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("llm: %v", err))
		} else {
			res.LLMSummary = summary
		}
	}
	res.FinishedAt = time.Now()

	debugInvestigateStore.Finish(input.Service, start, end, res)
	finished = true
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

// maxSampleValue returns the maximum sample value across all series in a
// Prometheus matrix response. Returns 0 if the response is empty or all
// values fail to parse.
func maxSampleValue(resp *promclient.QueryRangeResponse) float64 {
	if resp == nil {
		return 0
	}
	var max float64
	for _, series := range resp.Data.Result {
		for _, v := range series.Values {
			if len(v) < 2 {
				continue
			}
			s, ok := v[1].(string)
			if !ok {
				continue
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				continue
			}
			if f > max {
				max = f
			}
		}
	}
	return max
}

// listLabelValues fetches the values of a Prometheus label (e.g. "__name__"
// to get all metric names). Returns up to 200 values; failures are
// non-fatal — empty slice is returned with the error.
func listLabelValues(ctx context.Context, prom *promclient.Client, label string) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	path := "/api/v1/label/" + label + "/values"
	if err := prom.GetJSON(ctx, path, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("label values status %q", r.Status)
	}
	if len(r.Data) > 200 {
		return r.Data[:200], nil
	}
	return r.Data, nil
}
