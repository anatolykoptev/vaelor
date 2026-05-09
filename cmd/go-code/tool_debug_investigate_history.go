// cmd/go-code/tool_debug_investigate_history.go
// Phase γ.C — historical incidents persistence + retrieval.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/learnings"
)

// riskLevelFromScore maps a 0..1 anomaly score to a learnings risk level.
// Boundaries: >=0.8 → "high", >=0.5 → "medium", else "low".
func riskLevelFromScore(score float64) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

// primarySpikeKind returns the Kind of the first MetricSpike, or "" if empty.
func primarySpikeKind(spikes []investigate.MetricSpike) string {
	if len(spikes) == 0 {
		return ""
	}
	return spikes[0].Kind
}

// truncate returns s[:n] if len(s) > n, otherwise s unchanged.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// buildInvestigateRecord constructs a learnings.Record from investigation results.
// The Flag is prefixed with "investigate:" to distinguish from review_pr records.
func buildInvestigateRecord(service string, anomalyScore float64, res *investigate.InvestigationResult) learnings.Record {
	top := res.Hypotheses[0]
	flag := "investigate:" + primarySpikeKind(res.MetricSpikes)
	return learnings.Record{
		Repo:      service,
		Symbol:    top.Subject,
		RiskLevel: riskLevelFromScore(anomalyScore),
		Flag:      flag,
		Note:      truncate(res.LLMSummary, 1000),
	}
}

// runHistoryPersist writes the investigation outcome to the learnings store.
// It is best-effort: nil store, store error, and missing hypotheses all degrade gracefully.
func runHistoryPersist(ctx context.Context, store *learnings.Store, service string, anomalyScore float64, res *investigate.InvestigationResult) {
	if store == nil || len(res.Hypotheses) == 0 {
		return
	}
	rec := buildInvestigateRecord(service, anomalyScore, res)
	if err := store.Upsert(ctx, rec); err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("learnings persist: %v", err))
		return
	}
	res.Diagnostics.LearningsPersisted = true
}

// filterInvestigateIncidents keeps only incidents whose Flag starts with "investigate:".
// This drops review_pr records that share the same table.
func filterInvestigateIncidents(incidents []investigate.HistoricalIncident) []investigate.HistoricalIncident {
	if len(incidents) == 0 {
		return nil
	}
	var out []investigate.HistoricalIncident
	for _, inc := range incidents {
		if strings.HasPrefix(inc.Flag, "investigate:") {
			out = append(out, inc)
		}
	}
	return out
}

// retrieveHistoricalIncidents queries the learnings store for past investigation
// records for the same service. Returns nil on any error (best-effort).
// When input.Hint is non-empty and the store has an embedder, also runs a vector
// similarity query and merges results (deduped by Repo+Symbol).
func retrieveHistoricalIncidents(ctx context.Context, store *learnings.Store, service, hint string, res *investigate.InvestigationResult) {
	if store == nil {
		return
	}

	const topK = 3

	// Exact-by-repo lookup.
	records, err := store.NearestByRepo(ctx, service, topK)
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("historical incidents lookup: %v", err))
		// Continue — vector path may still succeed.
		records = nil
	}

	// Vector similarity lookup when hint is set and embedder configured.
	if hint != "" && store.HasEmbedder() {
		vecs, verr := store.NearestVector(ctx, hint, topK)
		if verr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				fmt.Sprintf("historical incidents vector lookup: %v", verr))
		} else {
			records = mergeDedup(records, vecs)
		}
	}

	// Convert to HistoricalIncident and filter to investigate: namespace.
	raw := make([]investigate.HistoricalIncident, 0, len(records))
	for _, r := range records {
		raw = append(raw, investigate.HistoricalIncident{
			Repo:      r.Repo,
			Symbol:    r.Symbol,
			RiskLevel: r.RiskLevel,
			Flag:      r.Flag,
			Note:      r.Note,
		})
	}
	res.HistoricalIncidents = filterInvestigateIncidents(raw)
}

// mergeDedup merges two slices of learnings.Record, deduplicating by (Repo, Symbol).
// Base records come first; additions are appended only if not already present.
func mergeDedup(base, additions []learnings.Record) []learnings.Record {
	seen := make(map[string]bool, len(base))
	for _, r := range base {
		seen[r.Repo+"\x00"+r.Symbol] = true
	}
	out := append([]learnings.Record(nil), base...)
	for _, r := range additions {
		key := r.Repo + "\x00" + r.Symbol
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out
}

// hintSearchMatch is a minimal struct capturing a codesearch result
// for the hint-driven hypothesis generation path.
type hintSearchMatch struct {
	File string
	Line int
	Text string
}

// applyHintMatches converts codesearch matches to Hypothesis entries with
// Source="hint_match" and merges them into the existing hypotheses slice.
func applyHintMatches(existing []investigate.Hypothesis, matches []hintSearchMatch) []investigate.Hypothesis {
	out := append([]investigate.Hypothesis(nil), existing...)
	for _, m := range matches {
		out = append(out, investigate.Hypothesis{
			Subject:       fmt.Sprintf("%s:%d (hint match)", m.File, m.Line),
			File:          m.File,
			Line:          m.Line,
			Source:        investigate.HypothesisSourceHintMatch,
			AnomalyScore:  0.5,
			EvidenceLinks: []string{m.Text},
		})
	}
	return out
}

// runHintSearch runs a codesearch for hint in root, returning at most 5 matches.
// It respects the caller's context (which should have a short timeout).
// On any error, it logs nothing and returns nil — hint-search failures are non-fatal.
func runHintSearch(ctx context.Context, hint, root string) []hintSearchMatch {
	if hint == "" || root == "" {
		return nil
	}
	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       hint,
		IsRegex:       false,
		CaseSensitive: false,
		MaxResults:    5,
		ContextLines:  1,
	})
	if err != nil {
		return nil
	}
	out := make([]hintSearchMatch, 0, len(matches))
	for _, m := range matches {
		out = append(out, hintSearchMatch{
			File: m.File,
			Line: m.Line,
			Text: m.Text,
		})
	}
	return out
}

// topAnomalyScore extracts the highest anomaly score from the result's MetricSpikes,
// falling back to the top hypothesis AnomalyScore, then 0.5 as default.
func topAnomalyScore(res *investigate.InvestigationResult) float64 {
	if len(res.MetricSpikes) > 0 {
		return res.MetricSpikes[0].Score
	}
	if len(res.Hypotheses) > 0 {
		return res.Hypotheses[0].AnomalyScore
	}
	return 0.5
}
