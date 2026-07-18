// cmd/go-code/tool_debug_investigate_alerts_test.go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/promclient"
)

func makeAlertsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestRunAlertsPhase_HappyPath(t *testing.T) {
	srv := makeAlertsServer(t, `{
		"status": "success",
		"data": {
			"alerts": [
				{
					"labels": {"alertname":"WireWriteMissing","service":"acme-sfu","severity":"critical"},
					"annotations": {"summary":"wire_written stuck","description":"ratio < 0.9","runbook_url":"https://runbooks/wire"},
					"state": "firing",
					"activeAt": "2026-05-08T10:00:00Z",
					"value": "0"
				},
				{
					"labels": {"alertname":"OtherServiceAlert","service":"other-svc","severity":"warning"},
					"annotations": {"summary":"unrelated"},
					"state": "firing",
					"activeAt": "2026-05-08T10:01:00Z",
					"value": "1"
				}
			]
		}
	}`)
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	diags := &investigate.Diagnostics{}
	spikes, violations := runAlertsPhase(context.Background(), prom, "acme-sfu", diags)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].AlertName != "WireWriteMissing" {
		t.Errorf("alert name: got %q", violations[0].AlertName)
	}
	if violations[0].Severity != "critical" {
		t.Errorf("severity: got %q", violations[0].Severity)
	}
	if violations[0].Summary != "wire_written stuck" {
		t.Errorf("summary: got %q", violations[0].Summary)
	}
	if violations[0].Runbook != "https://runbooks/wire" {
		t.Errorf("runbook: got %q", violations[0].Runbook)
	}
	if violations[0].ActiveAt != "2026-05-08T10:00:00Z" {
		t.Errorf("activeAt: got %q", violations[0].ActiveAt)
	}

	if len(spikes) != 1 {
		t.Fatalf("expected 1 spike, got %d", len(spikes))
	}
	if spikes[0].Kind != "invariant" {
		t.Errorf("spike kind: got %q", spikes[0].Kind)
	}
	if spikes[0].Score != scoreCritical {
		t.Errorf("spike score: got %v", spikes[0].Score)
	}
	if diags.AlertsQueried != 1 {
		t.Errorf("AlertsQueried: got %d", diags.AlertsQueried)
	}
}

func TestRunAlertsPhase_Empty(t *testing.T) {
	srv := makeAlertsServer(t, `{"status":"success","data":{"alerts":[]}}`)
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	diags := &investigate.Diagnostics{}
	spikes, violations := runAlertsPhase(context.Background(), prom, "my-svc", diags)

	if len(spikes) != 0 {
		t.Errorf("expected 0 spikes, got %d", len(spikes))
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
	if diags.AlertsQueried != 1 {
		t.Errorf("AlertsQueried: got %d", diags.AlertsQueried)
	}
}

func TestRunAlertsPhase_PendingFiltered(t *testing.T) {
	srv := makeAlertsServer(t, `{
		"status": "success",
		"data": {
			"alerts": [
				{
					"labels": {"alertname":"PendingAlert","service":"my-svc","severity":"warning"},
					"annotations": {"summary":"pending"},
					"state": "pending",
					"activeAt": "2026-05-08T10:00:00Z",
					"value": "0.5"
				}
			]
		}
	}`)
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	diags := &investigate.Diagnostics{}
	spikes, violations := runAlertsPhase(context.Background(), prom, "my-svc", diags)

	if len(spikes) != 0 {
		t.Errorf("expected 0 spikes for pending alert, got %d", len(spikes))
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for pending alert, got %d", len(violations))
	}
}

func TestRunAlertsPhase_JobLabelFallback(t *testing.T) {
	srv := makeAlertsServer(t, `{
		"status": "success",
		"data": {
			"alerts": [
				{
					"labels": {"alertname":"HighLatency","job":"my-svc","severity":"warning"},
					"annotations": {"summary":"latency high"},
					"state": "firing",
					"activeAt": "2026-05-08T10:00:00Z",
					"value": "1.5"
				}
			]
		}
	}`)
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	diags := &investigate.Diagnostics{}
	spikes, violations := runAlertsPhase(context.Background(), prom, "my-svc", diags)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation via job= label, got %d", len(violations))
	}
	if violations[0].AlertName != "HighLatency" {
		t.Errorf("alert name: got %q", violations[0].AlertName)
	}
	if len(spikes) != 1 {
		t.Fatalf("expected 1 spike via job= label, got %d", len(spikes))
	}
}
