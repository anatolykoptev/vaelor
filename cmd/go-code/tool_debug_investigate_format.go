// cmd/go-code/tool_debug_investigate_format.go
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// escapeCDATA splits any literal "]]>" sequences so they don't terminate
// the enclosing CDATA section. Standard XML technique: end the section,
// emit "]" or ">" as a separate CDATA, resume.
func escapeCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

// formatInvestigationResult renders the result as XML for the MCP caller.
// It is a thin sequencer: each section is delegated to a focused writer.
func formatInvestigationResult(r *investigate.InvestigationResult) string {
	var b strings.Builder
	b.WriteString(`<response tool="debug_investigate">`)
	writeInvestigationHeader(&b, r)

	if r.LLMSummary != "" {
		b.WriteString("<summary>")
		b.WriteString(escapeXML(r.LLMSummary))
		b.WriteString("</summary>")
	}

	for i, h := range r.Hypotheses {
		writeHypothesis(&b, i+1, h)
	}

	writeMetricSpikes(&b, r.MetricSpikes)
	writeAlertViolations(&b, r.AlertViolations)
	writeLogExcerpts(&b, r.LogExcerpts)
	writeHistoricalIncidents(&b, r.HistoricalIncidents)

	// Diagnostics is a plain struct — Marshal cannot fail in practice.
	d, _ := json.Marshal(r.Diagnostics)
	b.WriteString("<diagnostics>")
	b.WriteString(string(d))
	b.WriteString("</diagnostics>")

	b.WriteString("</investigation>")
	b.WriteString("</response>")
	return b.String()
}

// writeInvestigationHeader writes the opening <investigation …> tag.
// Two forms: with hint_kind attribute (when r.HintKind is non-empty) and without.
func writeInvestigationHeader(b *strings.Builder, r *investigate.InvestigationResult) {
	if r.HintKind != "" {
		fmt.Fprintf(b, `<investigation service=%q hint_kind=%q started_at=%q finished_at=%q>`,
			r.Service, r.HintKind, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(b, `<investigation service=%q started_at=%q finished_at=%q>`,
			r.Service, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339))
	}
}

// writeHypothesis writes the complete <hypothesis> block for a single hypothesis.
// rank is the 1-based position (i+1 in the caller loop).
func writeHypothesis(b *strings.Builder, rank int, h investigate.Hypothesis) {
	if h.Source != "" {
		fmt.Fprintf(b, "<hypothesis rank=\"%d\" confidence=%q source=%q>", rank, h.Confidence, h.Source)
	} else {
		fmt.Fprintf(b, "<hypothesis rank=\"%d\" confidence=%q>", rank, h.Confidence)
	}
	b.WriteString("<subject>")
	b.WriteString(escapeXML(h.Subject))
	b.WriteString("</subject>")
	if h.File != "" {
		fmt.Fprintf(b, "<location file=%q line=\"%d\"/>", h.File, h.Line)
	}
	fmt.Fprintf(b, "<signals span_count=\"%d\" anomaly_score=\"%.3f\"/>",
		h.SpanCount, h.AnomalyScore)

	writeHypothesisImpact(b, h.Impact)
	writeHypothesisSymbolBody(b, h.SymbolBody)
	writeHypothesisFusedScore(b, h.FusedScore, h.SignalBreakdown)
	writeHypothesisRecentChange(b, h.RecentChange)
	writeHypothesisBodyExcerpt(b, h.BodySource, h.File, h.Line, h.EndLine)
	writeHypothesisNextChecks(b, h.EvidenceLinks, h.NextChecks)

	b.WriteString("</hypothesis>")
}

// writeHypothesisImpact writes the <impact> block when imp is non-nil and meaningful.
// γ.B.2: rendered for top-3 hypotheses when Impact is set.
func writeHypothesisImpact(b *strings.Builder, imp *investigate.ImpactInfo) {
	if imp == nil || (imp.DirectCallers == 0 && imp.TotalAffected == 0 && imp.BlastRadius == "") {
		return
	}
	fmt.Fprintf(b,
		"<impact direct_callers=\"%d\" total_affected=\"%d\" blast_radius=%q risk_score=\"%.2f\"/>",
		imp.DirectCallers, imp.TotalAffected, imp.BlastRadius, imp.RiskScore)
}

// writeHypothesisSymbolBody writes the <symbol_body> block when sb is non-nil.
// γ.B.3: rendered for top-1 hypothesis when SymbolBody is set.
func writeHypothesisSymbolBody(b *strings.Builder, sb *investigate.SymbolBodyInfo) {
	if sb == nil {
		return
	}
	hasDeferStr := "false"
	if sb.HasDeferCleanup {
		hasDeferStr = "true"
	}
	hasTODOStr := "false"
	if sb.HasTODO {
		hasTODOStr = "true"
	}
	fmt.Fprintf(b,
		"<symbol_body error_exits=\"%d\" has_defer=%q has_todo=%q/>",
		sb.ErrorExits, hasDeferStr, hasTODOStr)
}

// writeHypothesisFusedScore writes the <fused_score> block with signal breakdown.
// γ.D.1: rendered when fusedScore > 0. Signals are emitted in a stable order
// using the fusionSig* consts to ensure consistent output.
func writeHypothesisFusedScore(b *strings.Builder, fusedScore float64, breakdown map[string]float64) {
	if fusedScore <= 0 {
		return
	}
	fmt.Fprintf(b, "<fused_score value=\"%.3f\">", fusedScore)
	for _, sig := range []string{
		fusionSigMetricAnomaly,
		fusionSigRecency,
		fusionSigComplexity,
		fusionSigImpact,
		fusionSigHistorical,
	} {
		if v, ok := breakdown[sig]; ok {
			fmt.Fprintf(b, "<signal name=%q score=\"%.3f\"/>", sig, v)
		}
	}
	b.WriteString("</fused_score>")
}

// writeHypothesisRecentChange writes the <recent_change> CDATA block.
// γ.D.2: rendered when rc is non-nil and rc.Diff is non-empty.
func writeHypothesisRecentChange(b *strings.Builder, rc *investigate.RecentChange) {
	if rc == nil || rc.Diff == "" {
		return
	}
	fmt.Fprintf(b, "<recent_change file=%q since=%q>", rc.File, rc.Since)
	b.WriteString("<![CDATA[")
	b.WriteString(escapeCDATA(rc.Diff))
	b.WriteString("]]>")
	b.WriteString("</recent_change>")
}

// writeHypothesisBodyExcerpt writes the <body_excerpt> CDATA block.
// Sprint B1: rendered when bodySource is non-empty.
// The lines attribute is "start-end" when endLine > line, else just "start".
func writeHypothesisBodyExcerpt(b *strings.Builder, bodySource, file string, line, endLine int) {
	if bodySource == "" {
		return
	}
	var lines string
	if endLine == 0 || endLine == line {
		lines = strconv.Itoa(line)
	} else {
		lines = strconv.Itoa(line) + "-" + strconv.Itoa(endLine)
	}
	fmt.Fprintf(b, "<body_excerpt file=%q lines=%q>", file, lines)
	b.WriteString("<![CDATA[")
	b.WriteString(escapeCDATA(bodySource))
	b.WriteString("]]>")
	b.WriteString("</body_excerpt>")
}

// writeHypothesisNextChecks writes <evidence> and <next_check> elements.
// Args keys are sorted for stable output.
func writeHypothesisNextChecks(b *strings.Builder, evidenceLinks []string, nextChecks []investigate.NextCheck) {
	for _, link := range evidenceLinks {
		b.WriteString("<evidence>")
		b.WriteString(escapeXML(link))
		b.WriteString("</evidence>")
	}
	for _, nc := range nextChecks {
		if len(nc.Args) == 0 {
			fmt.Fprintf(b, "<next_check tool=%q/>", nc.Tool)
		} else {
			fmt.Fprintf(b, "<next_check tool=%q>", nc.Tool)
			keys := make([]string, 0, len(nc.Args))
			for k := range nc.Args {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(b, "<arg name=%q>", k)
				b.WriteString(escapeXML(nc.Args[k]))
				b.WriteString("</arg>")
			}
			b.WriteString("</next_check>")
		}
	}
}

// writeMetricSpikes writes the <metric_spikes> block when spikes is non-empty.
func writeMetricSpikes(b *strings.Builder, spikes []investigate.MetricSpike) {
	if len(spikes) == 0 {
		return
	}
	b.WriteString("<metric_spikes>")
	for _, s := range spikes {
		fmt.Fprintf(b,
			"<spike kind=%q metric=%q labels=%q ratio=\"%.2f\" score=\"%.3f\"/>",
			s.Kind, s.MetricName, s.Labels, s.Ratio, s.Score)
	}
	b.WriteString("</metric_spikes>")
}

// writeAlertViolations writes the <alert_violations> block when avs is non-empty.
func writeAlertViolations(b *strings.Builder, avs []investigate.AlertViolation) {
	if len(avs) == 0 {
		return
	}
	b.WriteString("<alert_violations>")
	for _, av := range avs {
		fmt.Fprintf(b,
			"<alert_violation alertname=%q severity=%q service=%q active_at=%q>",
			av.AlertName, av.Severity, av.Service, av.ActiveAt)
		b.WriteString(escapeXML(av.Summary))
		b.WriteString("</alert_violation>")
	}
	b.WriteString("</alert_violations>")
}

// writeLogExcerpts writes the <log_excerpts> block when logs is non-empty.
func writeLogExcerpts(b *strings.Builder, logs []investigate.LogExcerpt) {
	if len(logs) == 0 {
		return
	}
	b.WriteString("<log_excerpts>")
	for _, l := range logs {
		fmt.Fprintf(b, "<line ts=%q level=%q>", l.Ts, escapeXML(l.Level))
		b.WriteString(escapeXML(l.Msg))
		b.WriteString("</line>")
	}
	b.WriteString("</log_excerpts>")
}

// writeHistoricalIncidents writes the <historical_incidents> block when incs is non-empty.
func writeHistoricalIncidents(b *strings.Builder, incs []investigate.HistoricalIncident) {
	if len(incs) == 0 {
		return
	}
	b.WriteString("<historical_incidents>")
	for _, inc := range incs {
		fmt.Fprintf(b, "<incident repo=%q symbol=%q risk_level=%q flag=%q>",
			inc.Repo, inc.Symbol, inc.RiskLevel, inc.Flag)
		b.WriteString(escapeXML(inc.Note))
		b.WriteString("</incident>")
	}
	b.WriteString("</historical_incidents>")
}
