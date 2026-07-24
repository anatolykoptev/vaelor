// Package main — eval harness for go-code retrieval quality.
//
// This file: repo_analyze eval mode — file-level relevance scoring + the real
// ANALYZE_RANK_FUSION_MODE A/B gate. The semantic_search mode (default) is
// byte-identical to the pre-mode harness; repo_analyze mode calls the
// repo_analyze tool (deep) and scores the ranked FILE list against the set of
// files containing the golden's expected_top_3 symbols.
package main

import (
	"fmt"
	"math"
	"strings"
)

// Eval mode constants. The default (modeSemanticSearch) preserves byte-identical
// behavior; modeRepoAnalyze exercises the repo_analyze file ranking whose
// fusion strategy is controlled by ANALYZE_RANK_FUSION_MODE.
const (
	modeSemanticSearch = "semantic_search"
	modeRepoAnalyze    = "repo_analyze"
)

// fileLevelExpected derives the file-relevance target from a golden record's
// expected_top_3 labels. Only labels of the form "<file>:<symbol>" contribute
// a file portion; symbol-only labels carry no file information and are
// dropped (they cannot match a file-level ranking). Each contributing file
// portion is rendered as "<file>:" (empty symbol) so the existing
// matchExpected case-3 suffix logic fires against synthetic file-only hits
// (File=path, Symbol=""). Dedup preserves first-occurrence order.
func fileLevelExpected(expectedTop3 []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, raw := range expectedTop3 {
		exp := strings.TrimSpace(raw)
		if exp == "" || !strings.Contains(exp, ":") {
			continue
		}
		fp, _, ok := strings.Cut(exp, ":")
		if !ok || strings.TrimSpace(fp) == "" {
			continue
		}
		if seen[fp] {
			continue
		}
		seen[fp] = true
		out = append(out, fp+":")
	}
	return out
}

// fileHitsFromPaths builds synthetic SearchHits (File=path, Symbol="") from
// the repo_analyze ranked file list so the existing rank-based metric
// functions (NDCG10/RecallAtK/MRR) compute file-level relevance against
// fileLevelExpected output. Position is 1-based to mirror semantic_search.
func fileHitsFromPaths(rankedFiles []string) []SearchHit {
	out := make([]SearchHit, len(rankedFiles))
	for i, p := range rankedFiles {
		out[i] = SearchHit{Position: i + 1, File: p, Symbol: ""}
	}
	return out
}

// retrievedFileKeys caps the ranked file list at 20 for the per_query JSON
// (retrieved_top_20). Mirrors retrievedKeys for the semantic_search mode.
func retrievedFileKeys(files []string) []string {
	const cap = 20
	n := len(files)
	if n > cap {
		n = cap
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, files[i])
	}
	return out
}

// EvaluateFusionGate computes the ANALYZE_RANK_FUSION_MODE A/B go/no-go
// verdict from raw query results. Same numeric logic as EvaluateGate (paired
// t-test over nDCG@10 + Recall@20 non-inferiority) but labels the output for
// fusion mode and records the tested mode string instead of a weight.
//
// Used only in repo_analyze mode, where the harness actually exercises the
// fusion-mode-controlled file ranking. In semantic_search mode the fusion
// gate stays NOT_EXERCISED (FusionSkipResult) since fusion mode does not
// affect the semantic_search path.
func EvaluateFusionGate(baseline, candidate []QueryResult, mode string) GateResult {
	g := evaluateGate(baseline, candidate, fusionLabel, math.NaN())
	g.TestedFusionMode = mode
	g.TestedWeight = 0 // not weight-driven; clear the NaN
	switch g.Verdict {
	case GatePass:
		g.RecommendedAction = fmt.Sprintf(
			"Set ANALYZE_RANK_FUSION_MODE=%s in production. Monitor gocode_analyze_fusion_mode and re-run the repo_analyze harness after 2 weeks.",
			mode,
		)
	case GateFail:
		if !g.Recall20NonInferior {
			g.RecommendedAction = "Do not flip ANALYZE_RANK_FUSION_MODE. Recall@20 regressed; investigate rrf weight tuning."
		} else {
			g.RecommendedAction = "Do not flip ANALYZE_RANK_FUSION_MODE. Consider tuning rrf weights or query-set composition."
		}
	}
	return g
}

// fusionLabel parameterizes the shared gate logic for the fusion-mode config.
// passAction uses %s but is overwritten by EvaluateFusionGate after
// evaluateGate returns (mirroring the EvaluateKeywordArmGate pattern).
var fusionLabel = gateLabel{
	envVar:         "ANALYZE_RANK_FUSION_MODE",
	passAction:     "Set ANALYZE_RANK_FUSION_MODE=%s in production. Monitor gocode_analyze_fusion_mode and re-run the repo_analyze harness after 2 weeks.",
	failAction:     "Do not flip ANALYZE_RANK_FUSION_MODE. Consider tuning rrf weights or query-set composition.",
	failRecall:     "Do not flip ANALYZE_RANK_FUSION_MODE. Recall@20 regressed; investigate rrf weight tuning.",
	passExtra:      "(RRF is rank-based fusion; monitor file-ranking quality vs minmax baseline.)",
	failRecallHint: "Possible cause: rrf weights over-emphasize a single signal, crowding out relevant-but-balanced files. Tune ANALYZE_RANK_WEIGHT_*.",
}
