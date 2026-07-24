// Package main — eval harness for go-code retrieval quality.
//
// This file: SPLADE A/B go/no-go gate for flipping RRF_WEIGHT_SPARSE from 0.
//
// Gate rule (research-mandated, documented in the SPLADE plan Phase 6):
//
//	PASS  iff nDCG@10 improves with p < 0.05 (paired t-test) AND
//	       Recall@20 is non-inferior (delta >= -nonInferiorMargin OR p_recall20 >= 0.05).
//	FAIL  if nDCG@10 shows no statistically significant improvement (p >= 0.05)
//	       OR nDCG@10 delta is non-positive (no gain even when nominally significant).
//	FAIL  if Recall@20 significantly regresses (delta < -nonInferiorMargin AND p < 0.05).
//
// Future online validation: Team Draft Interleaving (TDI) provides higher
// sensitivity via live traffic interleaving. This offline harness is the
// prerequisite gate; TDI is the recommended follow-up to confirm with real
// query distribution before a permanent default change.
package main

import (
	"fmt"
	"math"
)

// nonInferiorMargin is the maximum allowed Recall@20 regression before the
// gate treats the candidate as inferior. 0.02 = 2 percentage-point tolerance,
// chosen to absorb measurement noise at corpus sizes 40–200 queries while
// still catching real regressions. Document if you change it: the number
// affects the go/no-go decision.
const nonInferiorMargin = 0.02

// pAlpha is the significance threshold for the primary metric (nDCG@10).
// p < pAlpha is required for a PASS verdict.
const pAlpha = 0.05

// GateVerdict is the go/no-go decision for flipping a dark-launched config.
type GateVerdict string

const (
	// GatePass: nDCG@10 improved significantly AND Recall@20 is non-inferior.
	// Operator may flip the tested config to the candidate value.
	GatePass GateVerdict = "PASS"

	// GateFail: primary metric did not improve significantly, or Recall@20
	// significantly regressed. Do NOT flip the tested config.
	GateFail GateVerdict = "FAIL"

	// GateInsufficient: fewer than 2 paired queries — t-test is undefined.
	GateInsufficient GateVerdict = "INSUFFICIENT_DATA"

	// GateNotExercised: the flag was provided but the harness does not call
	// the tool the config affects (e.g. --fusion-mode affects repo_analyze,
	// which the harness does not yet call). No measurement was made.
	GateNotExercised GateVerdict = "NOT_EXERCISED"
)

// GateResult captures the gate evaluation in a machine-readable + human-readable form.
type GateResult struct {
	// Verdict is the go/no-go decision.
	Verdict GateVerdict `json:"verdict"`

	// TestedWeight is the numeric config value the candidate was run with
	// (RRF_WEIGHT_SPARSE / RRF_WEIGHT_GRAPH). Set to math.NaN() when unknown
	// or when the gate is not weight-driven (keyword-arm, fusion-mode).
	TestedWeight float64 `json:"tested_weight,omitempty"`

	// TestedArm is the KEYWORD_ARM value the candidate was run with
	// (grep | bm25f). Only set for the keyword-arm gate.
	TestedArm string `json:"tested_arm,omitempty"`

	// TestedFusionMode is the ANALYZE_RANK_FUSION_MODE value the candidate
	// was run with (minmax | rrf). Only set for the fusion-mode gate.
	TestedFusionMode string `json:"tested_fusion_mode,omitempty"`

	// RecommendedAction is a human-readable next step.
	RecommendedAction string `json:"recommended_action"`

	// NDCG10Delta is the raw mean delta (candidate − baseline) for nDCG@10.
	NDCG10Delta float64 `json:"ndcg10_delta"`

	// NDCG10P is the two-tailed p-value for the nDCG@10 paired t-test.
	NDCG10P float64 `json:"ndcg10_p"`

	// Recall20Delta is the raw mean delta for Recall@20.
	Recall20Delta float64 `json:"recall20_delta"`

	// Recall20P is the two-tailed p-value for the Recall@20 paired t-test.
	Recall20P float64 `json:"recall20_p"`

	// PairedQueries is the number of (baseline, candidate) query pairs used.
	PairedQueries int `json:"paired_queries"`

	// NDCGSignificant is true when nDCG@10 improved with p < pAlpha.
	NDCGSignificant bool `json:"ndcg10_significant"`

	// Recall20NonInferior is true when Recall@20 did not significantly regress.
	Recall20NonInferior bool `json:"recall20_non_inferior"`

	// Explanation is a detailed human-readable rationale for the verdict.
	Explanation string `json:"explanation"`
}

// gateLabel parameterizes the gate's human-readable text for the config
// under test (RRF_WEIGHT_SPARSE, RRF_WEIGHT_GRAPH, KEYWORD_ARM, etc.) so the
// same numeric gate logic serves all config-driven features.
type gateLabel struct {
	envVar         string // e.g. "RRF_WEIGHT_SPARSE", "KEYWORD_ARM"
	passAction     string // action on PASS (fmt.Sprintf'd with testedValue)
	failAction     string // action on FAIL
	failRecall     string // action on FAIL due to Recall@20 regression
	passExtra      string // extra text appended to PASS explanation
	failRecallHint string // hint appended to Recall@20 regression explanation
}

// EvaluateGate computes the SPLADE A/B go/no-go verdict from raw query results.
//
// baseline and candidate are the per-query result slices from the two harness
// runs (weight=0 baseline, weight=W candidate). sparseWeight is the tested
// RRF_WEIGHT_SPARSE value and is recorded in GateResult for traceability;
// pass math.NaN() when unknown.
//
// The function performs its own paired t-tests over nDCG@10 and Recall@20 so
// the verdict is grounded in raw floats, not in parsed strings from DeltaBlock.
func EvaluateGate(baseline, candidate []QueryResult, sparseWeight float64) GateResult {
	return evaluateGate(baseline, candidate, spladeLabel, sparseWeight)
}

// EvaluateGraphGate computes the graph-arm A/B go/no-go verdict. Same numeric
// logic as EvaluateGate but labels the output for RRF_WEIGHT_GRAPH.
func EvaluateGraphGate(baseline, candidate []QueryResult, graphWeight float64) GateResult {
	return evaluateGate(baseline, candidate, graphLabel, graphWeight)
}

// EvaluateKeywordArmGate computes the KEYWORD_ARM A/B go/no-go verdict. Same
// numeric logic as EvaluateGate but labels the output for KEYWORD_ARM and
// records the tested arm string instead of a weight. The RecommendedAction is
// rewritten with the arm string since evaluateGate formats with a float weight.
func EvaluateKeywordArmGate(baseline, candidate []QueryResult, arm string) GateResult {
	g := evaluateGate(baseline, candidate, keywordArmLabel, math.NaN())
	g.TestedArm = arm
	g.TestedWeight = 0 // not weight-driven; clear the NaN
	switch g.Verdict {
	case GatePass:
		g.RecommendedAction = fmt.Sprintf(
			"Set KEYWORD_ARM=%s in production. Monitor keyword arm metrics and re-run harness after 2 weeks.",
			arm,
		)
	case GateFail:
		if !g.Recall20NonInferior {
			g.RecommendedAction = "Do not flip KEYWORD_ARM. Recall@20 regressed; investigate keyword arm coverage gaps."
		} else {
			g.RecommendedAction = "Do not flip KEYWORD_ARM. Consider tuning the arm or query-set composition."
		}
	}
	return g
}

// FusionSkipResult returns a NOT_EXERCISED gate result for --fusion-mode. The
// harness calls semantic_search only; fusion mode (ANALYZE_RANK_FUSION_MODE)
// affects repo_analyze, which is a separate eval mode not yet implemented.
// This makes the skip VISIBLE in the report rather than silently omitting the
// gate or faking a meaningless measurement.
func FusionSkipResult(mode string) GateResult {
	return GateResult{
		Verdict:           GateNotExercised,
		TestedFusionMode:  mode,
		RecommendedAction: "No action: fusion mode was not measured by this harness run.",
		Explanation: fmt.Sprintf(
			"fusion mode not exercised: harness calls semantic_search only "+
				"(see repo_analyze eval mode, separate task). "+
				"ANALYZE_RANK_FUSION_MODE=%q affects repo_analyze ranking, not semantic_search. "+
				"Set the env on the server for completeness; a real fusion gate requires a "+
				"repo_analyze eval mode that is not yet implemented.",
			mode,
		),
	}
}

var spladeLabel = gateLabel{
	envVar:         "RRF_WEIGHT_SPARSE",
	passAction:     "Set RRF_WEIGHT_SPARSE=%.2f in production. Monitor gocode_rrf_weights{retriever='sparse'} and re-run harness after 2 weeks of backfill.",
	failAction:     "Do not flip RRF_WEIGHT_SPARSE. Consider tuning weight or query-set composition.",
	failRecall:     "Do not flip RRF_WEIGHT_SPARSE. Recall@20 regressed; investigate sparse arm coverage gaps.",
	passExtra:      "(TDI online interleaving is the recommended follow-up to confirm with live traffic.)",
	failRecallHint: "Possible cause: sparse arm biases toward rare high-weight tokens, crowding out relevant-but-broad symbols. Try lower weight or adjust SPLADE model.",
}

var graphLabel = gateLabel{
	envVar:         "RRF_WEIGHT_GRAPH",
	passAction:     "Set RRF_WEIGHT_GRAPH=%.2f in production. Monitor gocode_rrf_weights{retriever='graph'} and re-run harness after 2 weeks.",
	failAction:     "Do not flip RRF_WEIGHT_GRAPH. Consider tuning weight or query-set composition.",
	failRecall:     "Do not flip RRF_WEIGHT_GRAPH. Recall@20 regressed; investigate graph arm coverage gaps.",
	passExtra:      "(Graph arm is fused below dense; monitor PageRank candidate quality.)",
	failRecallHint: "Possible cause: graph candidates crowd out relevant-but-low-PageRank symbols. Try lower weight.",
}

var keywordArmLabel = gateLabel{
	envVar:         "KEYWORD_ARM",
	passAction:     "Set KEYWORD_ARM=%s in production. Monitor keyword arm metrics and re-run harness after 2 weeks.",
	failAction:     "Do not flip KEYWORD_ARM. Consider tuning the arm or query-set composition.",
	failRecall:     "Do not flip KEYWORD_ARM. Recall@20 regressed; investigate keyword arm coverage gaps.",
	passExtra:      "(BM25F arm is dark-launched; monitor keyword retrieval quality.)",
	failRecallHint: "Possible cause: BM25F term weighting crowds out relevant-but-broad symbols. Try grep arm or adjust BM25F params.",
}

// evaluateGate is the shared numeric gate logic. testedWeight is recorded in
// GateResult.TestedWeight; pass math.NaN() for non-weight-driven gates
// (keyword-arm sets TestedArm separately after this returns).
func evaluateGate(baseline, candidate []QueryResult, lbl gateLabel, testedWeight float64) GateResult {
	// Build paired slices: match on (repo, query).
	type pair struct{ blNDCG, cnNDCG, blR20, cnR20 float64 }
	idx := make(map[string]QueryResult, len(baseline))
	for _, r := range baseline {
		if r.Error == "" {
			idx[r.Repo+"|"+r.Query] = r
		}
	}

	var pairs []pair
	for _, r := range candidate {
		if r.Error != "" {
			continue
		}
		bl, ok := idx[r.Repo+"|"+r.Query]
		if !ok {
			continue
		}
		pairs = append(pairs, pair{
			blNDCG: bl.NDCG10, cnNDCG: r.NDCG10,
			blR20: bl.Recall20, cnR20: r.Recall20,
		})
	}

	if len(pairs) < 2 {
		return GateResult{
			Verdict:           GateInsufficient,
			TestedWeight:      testedWeight,
			PairedQueries:     len(pairs),
			RecommendedAction: "Add more golden queries; paired t-test requires ≥ 2 matched pairs.",
			Explanation: fmt.Sprintf(
				"Only %d paired query matches found (baseline %d, candidate %d non-error queries). "+
					"Paired t-test is undefined. Extend eval/golden/<repo>.jsonl with ≥40 queries "+
					"and re-run both baseline and candidate arms.",
				len(pairs), len(baseline), len(candidate),
			),
		}
	}

	// Extract per-metric slices.
	cnNDCG := make([]float64, len(pairs))
	blNDCG := make([]float64, len(pairs))
	cnR20 := make([]float64, len(pairs))
	blR20 := make([]float64, len(pairs))
	for i, p := range pairs {
		cnNDCG[i] = p.cnNDCG
		blNDCG[i] = p.blNDCG
		cnR20[i] = p.cnR20
		blR20[i] = p.blR20
	}

	ndcgDelta, ndcgP := pairedTTest(cnNDCG, blNDCG)
	r20Delta, r20P := pairedTTest(cnR20, blR20)

	// Gate conditions.
	ndcgSig := !math.IsNaN(ndcgDelta) && ndcgDelta > 0 && ndcgP < pAlpha
	// Non-inferior: delta above the tolerance floor OR not a significant drop.
	r20NonInf := r20Delta >= -nonInferiorMargin || r20P >= pAlpha

	g := GateResult{
		TestedWeight:        testedWeight,
		PairedQueries:       len(pairs),
		NDCG10Delta:         ndcgDelta,
		NDCG10P:             ndcgP,
		Recall20Delta:       r20Delta,
		Recall20P:           r20P,
		NDCGSignificant:     ndcgSig,
		Recall20NonInferior: r20NonInf,
	}

	switch {
	case ndcgSig && r20NonInf:
		g.Verdict = GatePass
		g.RecommendedAction = fmt.Sprintf(lbl.passAction, testedWeight)
		g.Explanation = fmt.Sprintf(
			"nDCG@10 improved by %+.4f (p=%.4f < %.2f) — statistically significant. "+
				"Recall@20 delta %+.4f (p=%.4f) — non-inferior (margin %.2f). "+
				"Both gate conditions met. PASS. %s",
			ndcgDelta, ndcgP, pAlpha, r20Delta, r20P, nonInferiorMargin, lbl.passExtra,
		)

	case !ndcgSig && r20NonInf:
		g.Verdict = GateFail
		g.RecommendedAction = lbl.failAction
		g.Explanation = fmt.Sprintf(
			"nDCG@10 delta %+.4f (p=%.4f >= %.2f) — no significant improvement. "+
				"Recall@20 delta %+.4f (p=%.4f). "+
				"Primary metric gate not met. FAIL.",
			ndcgDelta, ndcgP, pAlpha, r20Delta, r20P,
		)

	default:
		// ndcgSig is true but Recall@20 regressed significantly.
		g.Verdict = GateFail
		g.RecommendedAction = lbl.failRecall
		g.Explanation = fmt.Sprintf(
			"nDCG@10 improved %+.4f (p=%.4f) but Recall@20 regressed %+.4f (p=%.4f) beyond "+
				"non-inferior margin %.2f. %s arm hurts recall at 20. FAIL. %s",
			ndcgDelta, ndcgP, r20Delta, r20P, nonInferiorMargin, lbl.envVar, lbl.failRecallHint,
		)
	}

	return g
}
