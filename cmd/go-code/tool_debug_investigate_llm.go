// cmd/go-code/tool_debug_investigate_llm.go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/llm"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// investigateLLM is the interface subset of *llm.Client used by runLLMPhase.
// Defined locally so tests can inject fakes without importing the llm package.
type investigateLLM interface {
	Complete(ctx context.Context, system, user string, opts ...llm.ChatOption) (string, error)
}

// validPromLabel matches Prometheus label names: [A-Za-z_][A-Za-z0-9_]*.
// Labels that deviate are rejected by listLabelValues to prevent path injection.
var validPromLabel = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// listLabelValues fetches the values of a Prometheus label (e.g. "__name__"
// to get all metric names). Returns up to 200 values; failures are
// non-fatal — empty slice is returned with the error.
//
// label must match Prometheus label naming rules ([A-Za-z_][A-Za-z0-9_]*);
// invalid labels are rejected immediately to prevent path construction issues.
//
// For the "__name__" label specifically, prefer promclient.Client.MetricNames
// which is fetched once per investigation in runInvestigation and passed through
// the call chain. listLabelValues remains for other label lookups (e.g. service=,
// job=) that may be needed in future phases.
func listLabelValues(ctx context.Context, prom *promclient.Client, label string) ([]string, error) {
	if !validPromLabel.MatchString(label) {
		return nil, fmt.Errorf("listLabelValues: invalid label name %q (must match [A-Za-z_][A-Za-z0-9_]*)", label)
	}
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

// runLLMPhase executes Phase 5: produce an LLM-generated one-paragraph summary
// and reasoning for the top hypothesis.
//
// metricNames is the pre-fetched list from promclient.Client.MetricNames (fetched
// once in runInvestigation) — avoids a redundant __name__ round-trip here.
//
// Skipped when deps.LLM is nil or res.Hypotheses is empty.
//
// Note: llmCtx (10s deadline) is deferred inside this function; it fires when
// runLLMPhase returns. Nothing after this call reads the LLM context.
// investigationCacheKey builds a stable cache key from the investigation input
// and the top-5 hypotheses (by subject and anomaly score). The key is a hex
// SHA-256 prefix (first 16 bytes) to keep it compact and storage-safe.
func investigationCacheKey(input DebugInvestigateInput, top []investigate.Hypothesis) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|", input.Service, input.StartUnix, input.EndUnix)
	for i, hyp := range top {
		if i >= 5 {
			break
		}
		fmt.Fprintf(h, "%s|%.3f|", hyp.Subject, hyp.AnomalyScore)
	}
	return "investigate:llm:" + hex.EncodeToString(h.Sum(nil)[:16])
}

func runLLMPhase(
	ctx context.Context,
	deps analyze.Deps,
	metricNames []string,
	input DebugInvestigateInput,
	services []string,
	ops map[string]int,
	start, end time.Time,
	res *investigate.InvestigationResult,
) {
	// Guard: passing a nil *llm.Client as investigateLLM interface would create
	// a non-nil interface wrapping a nil pointer — client == nil check inside
	// runLLMPhaseInner would incorrectly return false. Check here before passing.
	if deps.LLM == nil {
		return
	}
	runLLMPhaseInner(ctx, deps.LLM, deps.ToolCache, metricNames, input, services, ops, start, end, res)
}

func runLLMPhaseInner(
	ctx context.Context,
	client investigateLLM,
	toolCache *kitcache.Cache,
	metricNames []string,
	input DebugInvestigateInput,
	services []string,
	ops map[string]int,
	start, end time.Time,
	res *investigate.InvestigationResult,
) {
	if client == nil || len(res.Hypotheses) == 0 {
		return
	}

	// Cap metricNames to 200 for the system prompt (matches prior listLabelValues cap).
	availMetrics := metricNames
	if len(availMetrics) > 200 {
		availMetrics = availMetrics[:200]
	}

	operationsSeen := make([]string, 0, len(ops))
	for op := range ops {
		operationsSeen = append(operationsSeen, op)
	}

	// Collect firing alert names for the system prompt ground truth.
	firingAlerts := make([]string, 0, len(res.AlertViolations))
	for _, av := range res.AlertViolations {
		firingAlerts = append(firingAlerts, av.AlertName)
	}

	sysPrompt := investigate.BuildSystemPrompt(investigate.PromptContext{
		Service:           input.Service,
		AvailableMetrics:  availMetrics,
		AvailableServices: services,
		OperationsSeen:    operationsSeen,
		FiringAlerts:      firingAlerts,
	})

	// Compact user-side payload: top 5 hypotheses + diagnostics + hint + alerts.
	topN := res.Hypotheses
	if len(topN) > 5 {
		topN = topN[:5]
	}
	userPayload := map[string]any{
		"service":          input.Service,
		"window":           map[string]string{"start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339)},
		"hypotheses":       topN,
		"diagnostics":      res.Diagnostics,
		"user_hint":        input.Hint,
		"alert_violations": res.AlertViolations,
	}
	userJSON, marshalErr := json.Marshal(userPayload)
	if marshalErr != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("marshal llm payload: %v", marshalErr))
		// Skip LLM call — sending an empty payload produces meaningless output.
		return
	}

	// Compute cache key from top hypotheses (same topN slice sent to LLM).
	// Best-effort: skip cache if toolCache is nil.
	var cacheKey string
	if toolCache != nil {
		cacheKey = investigationCacheKey(input, topN)
		if cached, ok, _ := kitcache.GetJSON[string](toolCache, ctx, cacheKey); ok {
			res.LLMSummary = cached
			res.Diagnostics.LLMCacheHit = true
			return
		}
	}

	// Bounded LLM call (10s timeout — non-blocking on overall investigation).
	llmCtx, llmCancel := context.WithTimeout(ctx, 10*time.Second)
	defer llmCancel()
	summary, err := client.Complete(llmCtx, sysPrompt, string(userJSON))
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("llm: %v", err))
		return
	}
	res.LLMSummary = summary

	// Cache the result best-effort (5 min TTL).
	if toolCache != nil && cacheKey != "" {
		_ = kitcache.SetJSONWithTTL(toolCache, ctx, cacheKey, summary, 5*time.Minute)
	}
}
