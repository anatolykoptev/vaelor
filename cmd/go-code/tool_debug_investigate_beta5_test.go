// cmd/go-code/tool_debug_investigate_beta5_test.go
//
// Tests for Phase β.5 additions:
//   - β5.3: orchestrator wires runAlertsPhase → AlertViolations + invariant spikes
//   - β5.4: formatInvestigationResult renders <alert_violations> block
//   - β5.5: BuildSystemPrompt includes FiringAlerts ground truth
//
// These tests are written BEFORE the implementation (RED phase).
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// newPromFakeWithAlerts returns a Prometheus fake that additionally handles
// /api/v1/alerts with the given alertsBody JSON.
func newPromFakeWithAlerts(pivot time.Time, windowVal, baselineVal float64, alertsBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/alerts":
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, alertsBody)
		case "/api/v1/label/__name__/values":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"status":"success","data":[]}`)
		case "/api/v1/query_range":
			var startF float64
			fmt.Sscanf(r.URL.Query().Get("start"), "%f", &startF)
			val := baselineVal
			if startF >= float64(pivot.Unix()) {
				val = windowVal
			}
			ts := float64(time.Now().Unix())
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%g,%q]]}]}}`, ts, fmt.Sprintf("%g", val))
		default:
			http.NotFound(w, r)
		}
	}))
}

// β5.3 — orchestrator wiring -----------------------------------------------

// TestIntegration_AlertViolations verifies that when Prometheus returns a
// firing alert for the service under investigation, runInvestigation populates
// res.AlertViolations and appends a MetricSpike with Kind="invariant".
func TestIntegration_AlertViolations(t *testing.T) {
	t.Cleanup(func() { debugInvestigateStore = investigate.NewInvestigationStore() })

	svc := "test-svc-alert-violations"
	jaegerSrv := newJaegerFake([]string{svc}, nil)
	defer jaegerSrv.Close()

	alertsBody := `{"status":"success","data":{"alerts":[
		{"labels":{"alertname":"WireWriteMissing","service":"` + svc + `","severity":"critical"},
		 "annotations":{"summary":"wire_written stayed at 0","runbook_url":"https://runbooks/wire"},
		 "state":"firing","activeAt":"2026-05-08T10:00:00Z","value":"0"}
	]}}`

	start, end := fixedWindow()
	promSrv := newPromFakeWithAlerts(start, 1.0, 1.0, alertsBody)
	defer promSrv.Close()

	prom := promclient.NewClient(promSrv.URL, 5*time.Second)
	jaeger := jaegerclient.NewClient(jaegerSrv.URL, 5*time.Second)

	_, callErr := handleDebugInvestigate(
		context.Background(),
		DebugInvestigateInput{Service: svc, StartUnix: start.Unix(), EndUnix: end.Unix()},
		analyze.Deps{},
		prom,
		jaeger,
	)
	if callErr != nil {
		t.Fatalf("handleDebugInvestigate: %v", callErr)
	}

	st := pollStore(svc, start, end, 10*time.Second)
	if st == nil {
		t.Fatal("investigation did not complete within 10s")
	}
	if st.Status() != investigate.StatusDone {
		t.Fatalf("expected StatusDone, got %v (err: %s)", st.Status(), st.Error())
	}
	r := st.Result()

	if len(r.AlertViolations) == 0 {
		t.Fatal("expected non-empty AlertViolations, got none")
	}
	if r.AlertViolations[0].AlertName != "WireWriteMissing" {
		t.Errorf("alert name: got %q, want WireWriteMissing", r.AlertViolations[0].AlertName)
	}
	if r.AlertViolations[0].Severity != "critical" {
		t.Errorf("severity: got %q, want critical", r.AlertViolations[0].Severity)
	}

	foundInvariant := false
	for _, s := range r.MetricSpikes {
		if s.Kind == "invariant" {
			foundInvariant = true
			if s.Score != scoreCritical {
				t.Errorf("invariant spike score: got %v, want %v", s.Score, scoreCritical)
			}
			break
		}
	}
	if !foundInvariant {
		t.Errorf("expected Kind=invariant spike in MetricSpikes, got: %v", r.MetricSpikes)
	}

	if r.Diagnostics.AlertsQueried != 1 {
		t.Errorf("AlertsQueried: got %d, want 1", r.Diagnostics.AlertsQueried)
	}
}

// β5.4 — format rendering ---------------------------------------------------

// TestFormat_AlertViolationsBlock verifies that formatInvestigationResult
// renders a <alert_violations> block when AlertViolations is non-empty.
func TestFormat_AlertViolationsBlock(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service:    "test-svc",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
		AlertViolations: []investigate.AlertViolation{
			{
				AlertName: "WireWriteMissing",
				Severity:  "critical",
				Service:   "test-svc",
				Summary:   "wire_written stayed at 0",
				Runbook:   "https://runbooks/wire",
				ActiveAt:  "2026-05-08T10:00:00Z",
			},
		},
	}
	out := formatInvestigationResult(r)

	if !strings.Contains(out, "<alert_violations>") {
		t.Errorf("expected <alert_violations> block, got:\n%s", out)
	}
	if !strings.Contains(out, `alertname="WireWriteMissing"`) {
		t.Errorf("expected alertname attribute, got:\n%s", out)
	}
	if !strings.Contains(out, `severity="critical"`) {
		t.Errorf("expected severity attribute, got:\n%s", out)
	}
	if !strings.Contains(out, "wire_written stayed at 0") {
		t.Errorf("expected summary text content, got:\n%s", out)
	}
	if !strings.Contains(out, "</alert_violations>") {
		t.Errorf("expected closing </alert_violations> tag, got:\n%s", out)
	}
}

// TestFormat_NoAlertViolationsBlock verifies that formatInvestigationResult
// does NOT render <alert_violations> when AlertViolations is empty.
func TestFormat_NoAlertViolationsBlock(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service:    "test-svc",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	out := formatInvestigationResult(r)
	if strings.Contains(out, "<alert_violations>") {
		t.Errorf("unexpected <alert_violations> for empty AlertViolations:\n%s", out)
	}
}

// β5.5 — LLM prompt context -------------------------------------------------

// TestBuildSystemPrompt_IncludesFiringAlerts verifies that when FiringAlerts
// is non-empty in PromptContext, BuildSystemPrompt includes the alert names.
func TestBuildSystemPrompt_IncludesFiringAlerts(t *testing.T) {
	c := investigate.PromptContext{
		Service:      "test-svc",
		FiringAlerts: []string{"WireWriteMissing", "HighLatency"},
	}
	out := investigate.BuildSystemPrompt(c)
	if !strings.Contains(out, "WireWriteMissing") {
		t.Errorf("expected WireWriteMissing in system prompt, got:\n%s", out)
	}
	if !strings.Contains(out, "HighLatency") {
		t.Errorf("expected HighLatency in system prompt, got:\n%s", out)
	}
}
