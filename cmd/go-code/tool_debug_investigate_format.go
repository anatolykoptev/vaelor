// cmd/go-code/tool_debug_investigate_format.go
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// ---- XML types ----
//
// The investigation response is modelled as typed structs marshalled via
// encoding/xml, so escaping and well-formedness are correct BY CONSTRUCTION.
// This replaces the prior hand-rolled fmt.Fprintf string concatenation, whose
// attribute sites used %q (Go quoting, NOT XML escaping) and silently produced
// malformed XML whenever a value carried <, & or " -- e.g. the <spike labels>
// attribute, which routinely holds Prometheus label sets like
// {service="x"} (see pr-review-council #260/#261).
//
// Float attributes are pre-formatted to strings (matching the original %.2f /
// %.3f precision) because xml.Marshal renders float64 via strconv and would
// drop trailing zeros (0.870 -> 0.87), changing the attribute value. CDATA
// payloads reuse the in-package wrapCDATA helper (]]> splitting) carried
// verbatim through an ,innerxml field.

type investigationRespXML struct {
	XMLName xml.Name         `xml:"response"`
	Tool    string           `xml:"tool,attr"`
	Inv     investigationXML `xml:"investigation"`
}

type investigationXML struct {
	Service             string                  `xml:"service,attr"`
	HintKind            string                  `xml:"hint_kind,attr,omitempty"`
	StartedAt           string                  `xml:"started_at,attr"`
	FinishedAt          string                  `xml:"finished_at,attr"`
	Summary             string                  `xml:"summary,omitempty"`
	Hypotheses          []hypothesisXML         `xml:"hypothesis"`
	MetricSpikes        *metricSpikesXML        `xml:"metric_spikes,omitempty"`
	AlertViolations     *alertViolationsXML     `xml:"alert_violations,omitempty"`
	LogExcerpts         *logExcerptsXML         `xml:"log_excerpts,omitempty"`
	HistoricalIncidents *historicalIncidentsXML `xml:"historical_incidents,omitempty"`
	Diagnostics         xmlCDATA                `xml:"diagnostics"`
}

type hypothesisXML struct {
	Rank         int              `xml:"rank,attr"`
	Confidence   string           `xml:"confidence,attr"`
	Source       string           `xml:"source,attr,omitempty"`
	Subject      string           `xml:"subject"`
	Location     *locationXML     `xml:"location,omitempty"`
	Signals      signalsXML       `xml:"signals"`
	Impact       *impactXML       `xml:"impact,omitempty"`
	SymbolBody   *symbolBodyXML   `xml:"symbol_body,omitempty"`
	FusedScore   *fusedScoreXML   `xml:"fused_score,omitempty"`
	RecentChange *recentChangeXML `xml:"recent_change,omitempty"`
	BodyExcerpt  *bodyExcerptXML  `xml:"body_excerpt,omitempty"`
	Evidence     []string         `xml:"evidence,omitempty"`
	NextChecks   []nextCheckXML   `xml:"next_check,omitempty"`
}

type locationXML struct {
	File string `xml:"file,attr"`
	Line int    `xml:"line,attr"`
}

type signalsXML struct {
	SpanCount    int    `xml:"span_count,attr"`
	AnomalyScore string `xml:"anomaly_score,attr"`
}

type impactXML struct {
	DirectCallers int    `xml:"direct_callers,attr"`
	TotalAffected int    `xml:"total_affected,attr"`
	BlastRadius   string `xml:"blast_radius,attr"`
	RiskScore     string `xml:"risk_score,attr"`
}

type symbolBodyXML struct {
	ErrorExits int  `xml:"error_exits,attr"`
	HasDefer   bool `xml:"has_defer,attr"`
	HasTODO    bool `xml:"has_todo,attr"`
}

type fusedScoreXML struct {
	Value   string      `xml:"value,attr"`
	Signals []signalXML `xml:"signal"`
}

type signalXML struct {
	Name  string `xml:"name,attr"`
	Score string `xml:"score,attr"`
}

// recentChangeXML and bodyExcerptXML carry a CDATA body (raw diff / source)
// alongside element attributes; the body is written verbatim via ,innerxml
// after being wrapped by the shared wrapCDATA helper.
type recentChangeXML struct {
	File  string `xml:"file,attr"`
	Since string `xml:"since,attr"`
	CDATA string `xml:",innerxml"`
}

type bodyExcerptXML struct {
	File  string `xml:"file,attr"`
	Lines string `xml:"lines,attr"`
	CDATA string `xml:",innerxml"`
}

type nextCheckXML struct {
	Tool string   `xml:"tool,attr"`
	Args []argXML `xml:"arg,omitempty"`
}

type argXML struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

type metricSpikesXML struct {
	Spikes []spikeXML `xml:"spike"`
}

type spikeXML struct {
	Kind   string `xml:"kind,attr"`
	Metric string `xml:"metric,attr"`
	Labels string `xml:"labels,attr"`
	Ratio  string `xml:"ratio,attr"`
	Score  string `xml:"score,attr"`
}

type alertViolationsXML struct {
	Items []alertViolationXML `xml:"alert_violation"`
}

type alertViolationXML struct {
	AlertName string `xml:"alertname,attr"`
	Severity  string `xml:"severity,attr"`
	Service   string `xml:"service,attr"`
	ActiveAt  string `xml:"active_at,attr"`
	Summary   string `xml:",chardata"`
}

type logExcerptsXML struct {
	Lines []logLineXML `xml:"line"`
}

type logLineXML struct {
	Ts    string `xml:"ts,attr"`
	Level string `xml:"level,attr"`
	Msg   string `xml:",chardata"`
}

type historicalIncidentsXML struct {
	Items []incidentXML `xml:"incident"`
}

type incidentXML struct {
	Repo      string `xml:"repo,attr"`
	Symbol    string `xml:"symbol,attr"`
	RiskLevel string `xml:"risk_level,attr"`
	Flag      string `xml:"flag,attr"`
	Note      string `xml:",chardata"`
}

// ---- Builders ----

// formatInvestigationResult renders the result as XML for the MCP caller.
func formatInvestigationResult(r *investigate.InvestigationResult) string {
	resp := investigationRespXML{
		Tool: "debug_investigate",
		Inv:  buildInvestigationXML(r),
	}
	b, err := xml.Marshal(resp)
	if err != nil {
		return fmt.Sprintf("<error>%s</error>", escapeXML(err.Error()))
	}
	return string(b)
}

func buildInvestigationXML(r *investigate.InvestigationResult) investigationXML {
	inv := investigationXML{
		Service:    r.Service,
		HintKind:   r.HintKind,
		StartedAt:  r.StartedAt.Format(time.RFC3339),
		FinishedAt: r.FinishedAt.Format(time.RFC3339),
		Summary:    r.LLMSummary,
	}
	for i, h := range r.Hypotheses {
		inv.Hypotheses = append(inv.Hypotheses, buildHypothesisXML(i+1, h))
	}
	inv.MetricSpikes = buildMetricSpikesXML(r.MetricSpikes)
	inv.AlertViolations = buildAlertViolationsXML(r.AlertViolations)
	inv.LogExcerpts = buildLogExcerptsXML(r.LogExcerpts)
	inv.HistoricalIncidents = buildHistoricalIncidentsXML(r.HistoricalIncidents)

	// Diagnostics is a plain struct -- Marshal cannot fail in practice.
	// json.Marshal HTML-escapes <, > and &, so the payload is XML-text-safe;
	// it is carried verbatim (no re-escaping) via the ,innerxml carrier.
	d, _ := json.Marshal(r.Diagnostics)
	inv.Diagnostics = xmlCDATA{Inner: string(d)}
	return inv
}

func buildHypothesisXML(rank int, h investigate.Hypothesis) hypothesisXML {
	hx := hypothesisXML{
		Rank:       rank,
		Confidence: string(h.Confidence),
		Source:     h.Source,
		Subject:    h.Subject,
		Signals: signalsXML{
			SpanCount:    h.SpanCount,
			AnomalyScore: fmt.Sprintf("%.3f", h.AnomalyScore),
		},
		Location:     buildLocationXML(h.File, h.Line),
		Impact:       buildImpactXML(h.Impact),
		SymbolBody:   buildSymbolBodyXML(h.SymbolBody),
		FusedScore:   buildFusedScoreXML(h.FusedScore, h.SignalBreakdown),
		RecentChange: buildRecentChangeXML(h.RecentChange),
		BodyExcerpt:  buildBodyExcerptXML(h.BodySource, h.File, h.Line, h.EndLine),
		Evidence:     h.EvidenceLinks,
		NextChecks:   buildNextChecksXML(h.NextChecks),
	}
	return hx
}

func buildLocationXML(file string, line int) *locationXML {
	if file == "" {
		return nil
	}
	return &locationXML{File: file, Line: line}
}

// buildImpactXML mirrors the prior guard: rendered only when Impact is set and
// carries a non-zero signal.
func buildImpactXML(imp *investigate.ImpactInfo) *impactXML {
	if imp == nil || (imp.DirectCallers == 0 && imp.TotalAffected == 0 && imp.BlastRadius == "") {
		return nil
	}
	return &impactXML{
		DirectCallers: imp.DirectCallers,
		TotalAffected: imp.TotalAffected,
		BlastRadius:   imp.BlastRadius,
		RiskScore:     fmt.Sprintf("%.2f", imp.RiskScore),
	}
}

func buildSymbolBodyXML(sb *investigate.SymbolBodyInfo) *symbolBodyXML {
	if sb == nil {
		return nil
	}
	return &symbolBodyXML{
		ErrorExits: sb.ErrorExits,
		HasDefer:   sb.HasDeferCleanup,
		HasTODO:    sb.HasTODO,
	}
}

// buildFusedScoreXML emits signals in a stable order using the fusionSig*
// consts, matching the prior formatter.
func buildFusedScoreXML(fusedScore float64, breakdown map[string]float64) *fusedScoreXML {
	if fusedScore <= 0 {
		return nil
	}
	fs := &fusedScoreXML{Value: fmt.Sprintf("%.3f", fusedScore)}
	for _, sig := range []string{
		fusionSigMetricAnomaly,
		fusionSigRecency,
		fusionSigComplexity,
		fusionSigImpact,
		fusionSigHistorical,
	} {
		if v, ok := breakdown[sig]; ok {
			fs.Signals = append(fs.Signals, signalXML{Name: sig, Score: fmt.Sprintf("%.3f", v)})
		}
	}
	return fs
}

func buildRecentChangeXML(rc *investigate.RecentChange) *recentChangeXML {
	if rc == nil || rc.Diff == "" {
		return nil
	}
	return &recentChangeXML{File: rc.File, Since: rc.Since, CDATA: wrapCDATA(rc.Diff)}
}

func buildBodyExcerptXML(bodySource, file string, line, endLine int) *bodyExcerptXML {
	if bodySource == "" {
		return nil
	}
	var lines string
	if endLine == 0 || endLine == line {
		lines = strconv.Itoa(line)
	} else {
		lines = strconv.Itoa(line) + "-" + strconv.Itoa(endLine)
	}
	return &bodyExcerptXML{File: file, Lines: lines, CDATA: wrapCDATA(bodySource)}
}

// buildNextChecksXML sorts each check's Args keys for stable output.
func buildNextChecksXML(nextChecks []investigate.NextCheck) []nextCheckXML {
	if len(nextChecks) == 0 {
		return nil
	}
	out := make([]nextCheckXML, 0, len(nextChecks))
	for _, nc := range nextChecks {
		nx := nextCheckXML{Tool: nc.Tool}
		if len(nc.Args) > 0 {
			keys := make([]string, 0, len(nc.Args))
			for k := range nc.Args {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				nx.Args = append(nx.Args, argXML{Name: k, Value: nc.Args[k]})
			}
		}
		out = append(out, nx)
	}
	return out
}

func buildMetricSpikesXML(spikes []investigate.MetricSpike) *metricSpikesXML {
	if len(spikes) == 0 {
		return nil
	}
	out := &metricSpikesXML{Spikes: make([]spikeXML, 0, len(spikes))}
	for _, s := range spikes {
		out.Spikes = append(out.Spikes, spikeXML{
			Kind:   s.Kind,
			Metric: s.MetricName,
			Labels: s.Labels,
			Ratio:  fmt.Sprintf("%.2f", s.Ratio),
			Score:  fmt.Sprintf("%.3f", s.Score),
		})
	}
	return out
}

func buildAlertViolationsXML(avs []investigate.AlertViolation) *alertViolationsXML {
	if len(avs) == 0 {
		return nil
	}
	out := &alertViolationsXML{Items: make([]alertViolationXML, 0, len(avs))}
	for _, av := range avs {
		out.Items = append(out.Items, alertViolationXML{
			AlertName: av.AlertName,
			Severity:  av.Severity,
			Service:   av.Service,
			ActiveAt:  av.ActiveAt,
			Summary:   av.Summary,
		})
	}
	return out
}

func buildLogExcerptsXML(logs []investigate.LogExcerpt) *logExcerptsXML {
	if len(logs) == 0 {
		return nil
	}
	out := &logExcerptsXML{Lines: make([]logLineXML, 0, len(logs))}
	for _, l := range logs {
		out.Lines = append(out.Lines, logLineXML{Ts: l.Ts, Level: l.Level, Msg: l.Msg})
	}
	return out
}

func buildHistoricalIncidentsXML(incs []investigate.HistoricalIncident) *historicalIncidentsXML {
	if len(incs) == 0 {
		return nil
	}
	out := &historicalIncidentsXML{Items: make([]incidentXML, 0, len(incs))}
	for _, inc := range incs {
		out.Items = append(out.Items, incidentXML{
			Repo:      inc.Repo,
			Symbol:    inc.Symbol,
			RiskLevel: inc.RiskLevel,
			Flag:      inc.Flag,
			Note:      inc.Note,
		})
	}
	return out
}
