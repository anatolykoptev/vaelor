// cmd/go-code/tool_debug_investigate_alerts.go
package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/promclient"
)

// runAlertsPhase queries Prometheus /api/v1/alerts, filters firing alerts
// by service label match, and returns them as MetricSpike{Kind="invariant",
// Score=scoreCritical} entries alongside AlertViolation records.
//
// Steady-state invariant violations (constant ratio breaks) dont trip Phase 4s
// anomaly detection because there is no delta — the ratio never changes, it is
// just always wrong. Prometheus alerting rules ARE the signal for these cases.
//
// Service matching: alert must have label service=service OR job=service.
func runAlertsPhase(ctx context.Context, prom *promclient.Client, service string, diags *investigate.Diagnostics) ([]investigate.MetricSpike, []investigate.AlertViolation) {
	alerts, err := prom.Alerts(ctx)
	if err != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("alerts: %v", err))
		return nil, nil
	}
	diags.AlertsQueried += 1

	var spikes []investigate.MetricSpike
	var violations []investigate.AlertViolation

	for _, a := range alerts {
		if a.State != "firing" {
			continue
		}
		if a.Labels["service"] != service && a.Labels["job"] != service {
			continue
		}
		violations = append(violations, investigate.AlertViolation{
			AlertName:   a.Labels["alertname"],
			Severity:    a.Labels["severity"],
			Service:     service,
			Summary:     a.Annotations["summary"],
			Description: a.Annotations["description"],
			Runbook:     a.Annotations["runbook_url"],
			ActiveAt:    a.ActiveAt,
		})
		spikes = append(spikes, investigate.MetricSpike{
			Kind:       "invariant",
			MetricName: a.Labels["alertname"],
			// Use plain %s (no inner quotes) to avoid double-escaping when the
			// Labels string is later rendered via labels=%q in _format.go.
			// Matches the convention used by computeFailureSpikes for service-only labels.
			Labels: fmt.Sprintf("{service=%s,severity=%s}", service, a.Labels["severity"]),
			Score:  scoreCritical,
			Ratio:  0,
		})
	}
	return spikes, violations
}
