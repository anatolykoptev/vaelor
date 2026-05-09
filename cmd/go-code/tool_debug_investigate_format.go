// cmd/go-code/tool_debug_investigate_format.go
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// formatInvestigationResult renders the result as XML for the MCP caller.
func formatInvestigationResult(r *investigate.InvestigationResult) string {
	var b strings.Builder
	b.WriteString(`<response tool="debug_investigate">`)
	b.WriteString("\n  ")
	if r.HintKind != "" {
		b.WriteString(fmt.Sprintf(`<investigation service=%q hint_kind=%q started_at=%q finished_at=%q>`,
			r.Service, r.HintKind, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339)))
	} else {
		b.WriteString(fmt.Sprintf(`<investigation service=%q started_at=%q finished_at=%q>`,
			r.Service, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339)))
	}

	if r.LLMSummary != "" {
		b.WriteString("\n    <summary>")
		b.WriteString(escapeXML(r.LLMSummary))
		b.WriteString("</summary>")
	}

	for i, h := range r.Hypotheses {
		if h.Source != "" {
			b.WriteString(fmt.Sprintf("\n    <hypothesis rank=\"%d\" confidence=%q source=%q>", i+1, h.Confidence, h.Source))
		} else {
			b.WriteString(fmt.Sprintf("\n    <hypothesis rank=\"%d\" confidence=%q>", i+1, h.Confidence))
		}
		b.WriteString("\n      <subject>")
		b.WriteString(escapeXML(h.Subject))
		b.WriteString("</subject>")
		if h.File != "" {
			b.WriteString(fmt.Sprintf("\n      <location file=%q line=\"%d\"/>", h.File, h.Line))
		}
		b.WriteString(fmt.Sprintf("\n      <signals span_count=\"%d\" anomaly_score=\"%.3f\"/>",
			h.SpanCount, h.AnomalyScore))

		// γ.B.2: blast radius block — rendered for top-3 when Impact is set.
		if imp := h.Impact; imp != nil && (imp.DirectCallers > 0 || imp.TotalAffected > 0 || imp.BlastRadius != "") {
			b.WriteString(fmt.Sprintf(
				"\n      <impact direct_callers=\"%d\" total_affected=\"%d\" blast_radius=%q risk_score=\"%.2f\"/>",
				imp.DirectCallers, imp.TotalAffected, imp.BlastRadius, imp.RiskScore))
		}

		// γ.B.3: symbol body block — rendered for top-1 when SymbolBody is set.
		if sb := h.SymbolBody; sb != nil {
			hasDeferStr := "false"
			if sb.HasDeferCleanup {
				hasDeferStr = "true"
			}
			hasTODOStr := "false"
			if sb.HasTODO {
				hasTODOStr = "true"
			}
			b.WriteString(fmt.Sprintf(
				"\n      <symbol_body error_exits=\"%d\" has_defer=%q has_todo=%q/>",
				sb.ErrorExits, hasDeferStr, hasTODOStr))
		}

		// γ.D.1: fused score block — rendered when FusedScore > 0.
		if h.FusedScore > 0 {
			b.WriteString(fmt.Sprintf("\n      <fused_score value=\"%.3f\">", h.FusedScore))
			// Emit signals in a stable order for consistent output.
			for _, sig := range []string{
				fusionSigMetricAnomaly,
				fusionSigRecency,
				fusionSigComplexity,
				fusionSigImpact,
				fusionSigHistorical,
			} {
				if v, ok := h.SignalBreakdown[sig]; ok {
					b.WriteString(fmt.Sprintf("\n        <signal name=%q score=\"%.3f\"/>", sig, v))
				}
			}
			b.WriteString("\n      </fused_score>")
		}

		// γ.D.2: recent change block — rendered when RecentChange is set and Diff non-empty.
		if rc := h.RecentChange; rc != nil && rc.Diff != "" {
			b.WriteString(fmt.Sprintf("\n      <recent_change file=%q since=%q>", rc.File, rc.Since))
			b.WriteString("\n        <![CDATA[\n")
			b.WriteString(rc.Diff)
			b.WriteString("\n        ]]>")
			b.WriteString("\n      </recent_change>")
		}

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

	if len(r.MetricSpikes) > 0 {
		b.WriteString("\n    <metric_spikes>")
		for _, s := range r.MetricSpikes {
			b.WriteString(fmt.Sprintf(
				"\n      <spike kind=%q metric=%q labels=%q ratio=\"%.2f\" score=\"%.3f\"/>",
				s.Kind, s.MetricName, s.Labels, s.Ratio, s.Score))
		}
		b.WriteString("\n    </metric_spikes>")
	}

	if len(r.AlertViolations) > 0 {
		b.WriteString("\n    <alert_violations>")
		for _, av := range r.AlertViolations {
			b.WriteString(fmt.Sprintf(
				"\n      <alert_violation alertname=%q severity=%q service=%q active_at=%q>",
				av.AlertName, av.Severity, av.Service, av.ActiveAt))
			b.WriteString(escapeXML(av.Summary))
			b.WriteString("</alert_violation>")
		}
		b.WriteString("\n    </alert_violations>")
	}

	if len(r.LogExcerpts) > 0 {
		b.WriteString("\n    <log_excerpts>")
		for _, l := range r.LogExcerpts {
			b.WriteString(fmt.Sprintf("\n      <line ts=%q level=%q>", l.Ts, escapeXML(l.Level)))
			b.WriteString(escapeXML(l.Msg))
			b.WriteString("</line>")
		}
		b.WriteString("\n    </log_excerpts>")
	}

	if len(r.HistoricalIncidents) > 0 {
		b.WriteString("\n    <historical_incidents>")
		for _, inc := range r.HistoricalIncidents {
			b.WriteString(fmt.Sprintf("\n      <incident repo=%q symbol=%q risk_level=%q flag=%q>",
				inc.Repo, inc.Symbol, inc.RiskLevel, inc.Flag))
			b.WriteString(escapeXML(inc.Note))
			b.WriteString("</incident>")
		}
		b.WriteString("\n    </historical_incidents>")
	}

	// Diagnostics is a plain struct — Marshal cannot fail in practice.
	d, _ := json.Marshal(r.Diagnostics)
	b.WriteString("\n    <diagnostics>")
	b.WriteString(string(d))
	b.WriteString("</diagnostics>")

	b.WriteString("\n  </investigation>")
	b.WriteString("\n</response>")
	return b.String()
}
