// Command eval — offline retrieval-quality harness for go-code.
//
// Replays a labeled (query, expected_top_3) golden dataset against a running
// go-code MCP server's REST bridge, computes nDCG@10, Recall@10/@20, and MRR,
// and writes a JSON report. Optional --baseline runs a paired t-test against
// a prior report and reports per-metric significance.
//
// SPLADE A/B gate mode (Phase P6):
//
// Run the harness twice — once with RRF_WEIGHT_SPARSE=0 (baseline) and once
// with the candidate weight — then use --baseline + --splade-weight to emit a
// go/no-go verdict for flipping the production env var:
//
//	# Step 1: baseline run (RRF_WEIGHT_SPARSE=0 on the server)
//	go-code-eval --golden-dir eval/golden \
//	             --target-url http://127.0.0.1:8897 \
//	             --output /tmp/eval-baseline.json
//
//	# Step 2: candidate run (RRF_WEIGHT_SPARSE=0.3 on the server)
//	go-code-eval --golden-dir eval/golden \
//	             --target-url http://127.0.0.1:8897 \
//	             --output /tmp/eval-cand.json \
//	             --baseline /tmp/eval-baseline.json \
//	             --splade-weight 0.3
//
//	# The report's "splade_gate" field contains the PASS/FAIL verdict.
//	jq .splade_gate /tmp/eval-cand.json
//
// Graph-arm A/B gate mode (Phase 1 graph-first retrieval):
//
// Same pattern as SPLADE but use --graph-weight and the report's "graph_gate":
//
//	# Step 1: baseline run (RRF_WEIGHT_GRAPH=0 on the server)
//	go-code-eval --golden-dir eval/golden \
//	             --target-url http://127.0.0.1:8897 \
//	             --output /tmp/eval-baseline.json
//
//	# Step 2: candidate run (RRF_WEIGHT_GRAPH=0.2 on the server)
//	go-code-eval --golden-dir eval/golden \
//	             --target-url http://127.0.0.1:8897 \
//	             --output /tmp/eval-graph-cand.json \
//	             --baseline /tmp/eval-baseline.json \
//	             --graph-weight 0.2
//
//	jq .graph_gate /tmp/eval-graph-cand.json
//
// Gate: PASS iff nDCG@10 improves at p<0.05 AND Recall@20 is non-inferior
// (delta >= -2% OR p >= 0.05). See abgate.go for the full rule.
//
// Keyword-arm A/B gate mode (--keyword-arm):
//
// Same pattern as SPLADE but use --keyword-arm grep|bm25f and the report's
// "keyword_arm_gate". The gate uses the same nDCG@10 + Recall@20 non-inferiority
// logic as the SPLADE/graph gates.
//
// Fusion-mode flag (--fusion-mode minmax|rrf):
//
// In semantic_search mode (default): reports NOT_EXERCISED — the harness calls
// semantic_search only, and ANALYZE_RANK_FUSION_MODE affects repo_analyze's
// file ranking, not semantic_search. The flag records the tested mode in
// metadata for traceability but does NOT fake a fusion measurement.
//
// In repo_analyze mode (--mode repo_analyze): emits a REAL fusion A/B gate
// (paired t-test over nDCG@10 + Recall@20 non-inferiority) comparing the
// baseline (minmax) and candidate (rrf) repo_analyze runs. Requires --baseline.
//
// --repo-map (or REPO_MAP env):
//
// Comma-separated repo_key=path mapping that resolves placeholder golden paths
// (e.g. /path/to/repo) to real absolute paths or forge slugs at run time, so
// the golden JSONL stays portable. See eval/golden/README.md.
//
// Future online step: Team Draft Interleaving (TDI) on live traffic provides
// higher sensitivity; this offline harness is the prerequisite gate.
//
// The harness is read-only against the target server: every query is a
// semantic_search call. Use against a non-prod target for fair benchmarking.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// defaultWorkers tracks "8 concurrent workers per ~5min on 200 queries"
	// from the harness spec — keeps p95 latency well under the SLA.
	defaultWorkers = 8

	// minTopK = 20 because Recall@20 needs at least 20 candidates.
	minTopK = 20

	// defaultTimeout is large enough to absorb embed-server warm-up + a
	// full 200-query corpus on cold cache.
	defaultTimeout = 30 * time.Minute
)

// version is set at build time via -ldflags; "dev" for local builds.
var version = "dev"

// noSPLADEWeight is the sentinel that signals "splade-weight not provided".
const noSPLADEWeight = -1.0

// noGraphWeight is the sentinel that signals "graph-weight not provided".
const noGraphWeight = -1.0

// keywordArmUnset and fusionModeUnset are sentinels for flags not provided.
const (
	keywordArmUnset = ""
	fusionModeUnset = ""
)

func main() {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	goldenDir := fs.String("golden-dir", "eval/golden", "directory of <repo>.jsonl golden files")
	targetURL := fs.String("target-url", "http://127.0.0.1:8897", "go-code MCP base URL (REST bridge at /api/tools)")
	output := fs.String("output", "", "JSON output path (default: stdout)")
	baseline := fs.String("baseline", "", "optional baseline report path for A/B comparison")
	splaDeWeight := fs.Float64("splade-weight", noSPLADEWeight,
		"RRF_WEIGHT_SPARSE used in the candidate run; enables SPLADE go/no-go gate in output (requires --baseline)")
	graphWeight := fs.Float64("graph-weight", noGraphWeight,
		"RRF_WEIGHT_GRAPH used in the candidate run; enables graph-arm go/no-go gate in output (requires --baseline)")
	keywordArm := fs.String("keyword-arm", keywordArmUnset,
		"KEYWORD_ARM used in the candidate run (grep|bm25f); enables keyword-arm go/no-go gate in output (requires --baseline)")
	fusionMode := fs.String("fusion-mode", fusionModeUnset,
		"ANALYZE_RANK_FUSION_MODE used in the candidate run (minmax|rrf); in semantic_search mode reports NOT_EXERCISED, in repo_analyze mode emits a real A/B gate (requires --baseline)")
	mode := fs.String("mode", modeSemanticSearch,
		"eval mode: semantic_search (default, current behavior) | repo_analyze (calls repo_analyze, scores the ranked file list, enables the real fusion gate)")
	repoMapFlag := fs.String("repo-map", "",
		"comma-separated repo_key=path mapping (e.g. go-code=/host/src/go-code,MemDB=/host/src/MemDB); resolves placeholder golden paths at run time. Falls back to REPO_MAP env.")
	workers := fs.Int("workers", defaultWorkers, "concurrent HTTP workers")
	topK := fs.Int("top-k", minTopK, "top_k passed to semantic_search (≥10 for Recall@10/@20)")
	timeout := fs.Duration("timeout", defaultTimeout, "overall harness timeout")
	queryRetry := fs.Int("query-retry", 6, "max attempts per query on transient tool signals (1=no retry)")
	warmup := fs.Bool("warmup", true, "warm up indexes before the measured run")
	warmupTimeout := fs.Duration("warmup-timeout", 10*time.Minute, "per-repo warmup timeout (0 disables warmup)")
	verFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *verFlag {
		fmt.Println("go-code-eval", version)
		return
	}

	sw := *splaDeWeight
	if sw == noSPLADEWeight {
		sw = math.NaN()
	}
	gw := *graphWeight
	if gw == noGraphWeight {
		gw = math.NaN()
	}

	// --repo-map flag takes precedence; fall back to REPO_MAP env.
	repoMapRaw := *repoMapFlag
	if repoMapRaw == "" {
		repoMapRaw = os.Getenv("REPO_MAP")
	}

	if err := run(*goldenDir, *targetURL, *output, *baseline, sw, gw, *keywordArm, *fusionMode, repoMapRaw, *mode, *workers, *topK, *timeout, *queryRetry, *warmup, *warmupTimeout); err != nil {
		slog.Error("eval failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(goldenDir, targetURL, output, baseline string, splaDeWeight, graphWeight float64, keywordArm, fusionMode, repoMapRaw, mode string, workers, topK int, timeout time.Duration, queryRetry int, warmup bool, warmupTimeout time.Duration) error {
	if mode != modeSemanticSearch && mode != modeRepoAnalyze {
		return fmt.Errorf("invalid mode %q: use %q or %q", mode, modeSemanticSearch, modeRepoAnalyze)
	}
	if topK < minTopK {
		// Recall@20 requires the candidate pool to have at least 20 items.
		topK = minTopK
	}

	golden, err := LoadGolden(goldenDir)
	if err != nil {
		return fmt.Errorf("load golden: %w", err)
	}

	// Apply repo-map override: resolve placeholder paths to real paths/slug.
	repoMap, err := ParseRepoMap(repoMapRaw)
	if err != nil {
		return fmt.Errorf("repo-map: %w", err)
	}
	golden.ApplyRepoMap(repoMap)

	totalQ := 0
	for _, r := range golden.PerRepo {
		totalQ += len(r)
	}
	slog.Info("golden loaded",
		slog.Int("repos", len(golden.PerRepo)),
		slog.Int("queries", totalQ),
	)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := NewMCPClient(targetURL)

	// Warmup phase: probe each distinct resolved repo until its index is ready
	// so the measured pass runs against warm indexes and measured latency
	// isn't polluted by first-hit indexing. Best-effort — a timeout or error
	// on one repo does not abort the run.
	if warmup && warmupTimeout > 0 {
		warmupRepos(ctx, client, golden, runnerCfg{
			Mode:          mode,
			TopK:          topK,
			RetryAttempts: queryRetry,
			RetryBase:     defaultRetryBase,
			RetryCap:      defaultRetryCap,
		}, warmupTimeout)
	}

	start := time.Now()
	results := runEval(ctx, client, golden, runnerCfg{
		Workers:       workers,
		TopK:          topK,
		Mode:          mode,
		RetryAttempts: queryRetry,
		RetryBase:     defaultRetryBase,
		RetryCap:      defaultRetryCap,
	})
	elapsed := time.Since(start)
	slog.Info("eval complete",
		slog.Duration("elapsed", elapsed),
		slog.Int("queries", len(results)),
	)

	report := Report{
		Metadata: Metadata{
			Timestamp:  time.Now().UTC(),
			TargetURL:  targetURL,
			GitSHA:     detectGitSHA(),
			GoldenDir:  goldenDir,
			TopK:       topK,
			KeywordArm: keywordArm,
			FusionMode: fusionMode,
		},
		PerQuery:    results,
		PerRepo:     computePerRepo(results),
		PerLanguage: computePerLanguage(results),
		Aggregates:  computeAggregates(results),
	}
	if repoMapRaw != "" {
		report.Metadata.RepoMapPath = repoMapRaw
	}
	// Record the mode only when non-default so default (semantic_search) runs
	// stay byte-identical to the pre-mode harness (no new metadata field).
	if mode != modeSemanticSearch {
		report.Metadata.Mode = mode
	}

	if baseline != "" {
		base, err := readReport(baseline)
		if err != nil {
			return fmt.Errorf("baseline: %w", err)
		}
		report.Delta = computeDelta(base.PerQuery, results)

		// Emit the SPLADE go/no-go gate when --splade-weight is provided.
		// math.NaN() is the sentinel meaning "not provided" (set in main).
		if !math.IsNaN(splaDeWeight) {
			gate := EvaluateGate(base.PerQuery, results, splaDeWeight)
			report.Gate = &gate
			slog.Info("SPLADE gate",
				slog.String("verdict", string(gate.Verdict)),
				slog.Float64("ndcg10_delta", gate.NDCG10Delta),
				slog.Float64("ndcg10_p", gate.NDCG10P),
				slog.Float64("recall20_delta", gate.Recall20Delta),
				slog.Float64("recall20_p", gate.Recall20P),
				slog.Int("paired_queries", gate.PairedQueries),
			)
		}

		// Emit the graph-arm go/no-go gate when --graph-weight is provided.
		// Same gate logic as SPLADE; GateResult.TestedWeight records the graph weight.
		if !math.IsNaN(graphWeight) {
			gate := EvaluateGraphGate(base.PerQuery, results, graphWeight)
			report.GraphGate = &gate
			slog.Info("graph arm gate",
				slog.String("verdict", string(gate.Verdict)),
				slog.Float64("ndcg10_delta", gate.NDCG10Delta),
				slog.Float64("ndcg10_p", gate.NDCG10P),
				slog.Float64("recall20_delta", gate.Recall20Delta),
				slog.Float64("recall20_p", gate.Recall20P),
				slog.Int("paired_queries", gate.PairedQueries),
			)
		}

		// Emit the keyword-arm go/no-go gate when --keyword-arm is provided.
		if keywordArm != keywordArmUnset {
			gate := EvaluateKeywordArmGate(base.PerQuery, results, keywordArm)
			report.KeywordArmGate = &gate
			slog.Info("keyword arm gate",
				slog.String("verdict", string(gate.Verdict)),
				slog.String("tested_arm", gate.TestedArm),
				slog.Float64("ndcg10_delta", gate.NDCG10Delta),
				slog.Float64("ndcg10_p", gate.NDCG10P),
				slog.Int("paired_queries", gate.PairedQueries),
			)
		}

		// Emit the fusion-mode A/B gate in repo_analyze mode (requires the
		// paired baseline). Fusion mode controls repo_analyze's file ranking;
		// only in this mode does the harness actually exercise it.
		if fusionMode != fusionModeUnset && mode == modeRepoAnalyze {
			gate := EvaluateFusionGate(base.PerQuery, results, fusionMode)
			report.FusionGate = &gate
			slog.Info("fusion mode gate",
				slog.String("verdict", string(gate.Verdict)),
				slog.String("tested_fusion_mode", gate.TestedFusionMode),
				slog.Float64("ndcg10_delta", gate.NDCG10Delta),
				slog.Float64("ndcg10_p", gate.NDCG10P),
				slog.Int("paired_queries", gate.PairedQueries),
			)
		}
	}

	// Fusion gate for the two non-baseline paths:
	//   - semantic_search mode: fusion mode does not affect this path →
	//     NOT_EXERCISED (byte-identical to the pre-mode harness).
	//   - repo_analyze mode without --baseline: the paired t-test needs a
	//     baseline → INSUFFICIENT_DATA.
	if fusionMode != fusionModeUnset && report.FusionGate == nil {
		var gate GateResult
		if mode == modeRepoAnalyze {
			gate = GateResult{
				Verdict:           GateInsufficient,
				TestedFusionMode:  fusionMode,
				RecommendedAction: "Provide --baseline (ANALYZE_RANK_FUSION_MODE=minmax run) to compute the fusion A/B gate.",
				Explanation: fmt.Sprintf(
					"fusion gate requires a paired baseline (minmax) and candidate (=%q) "+
						"repo_analyze run; --baseline not provided.",
					fusionMode,
				),
			}
		} else {
			gate = FusionSkipResult(fusionMode)
		}
		report.FusionGate = &gate
		slog.Info("fusion mode gate",
			slog.String("verdict", string(gate.Verdict)),
			slog.String("tested_fusion_mode", gate.TestedFusionMode),
		)
	}

	if err := writeReport(output, report); err != nil {
		return err
	}
	if output != "" && output != "-" {
		slog.Info("report written", slog.String("path", output))
	}
	return nil
}

// gitSHATimeout caps the `git rev-parse` invocation — metadata isn't worth
// hanging the harness on a stuck git filter.
const gitSHATimeout = 5 * time.Second

// detectGitSHA shells out to `git rev-parse HEAD`; returns "" on failure so
// the report still writes when run outside a git checkout (e.g. from /tmp).
// The harness does not require git — git SHA is metadata-only.
func detectGitSHA() string {
	ctx, cancel := context.WithTimeout(context.Background(), gitSHATimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
