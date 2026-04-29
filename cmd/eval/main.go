// Command eval — offline retrieval-quality harness for go-code.
//
// Replays a labeled (query, expected_top_3) golden dataset against a running
// go-code MCP server's REST bridge, computes nDCG@10, Recall@10/@20, and MRR,
// and writes a JSON report.
//
// Usage:
//
//	go-code-eval \
//	  --golden-dir eval/golden \
//	  --target-url http://127.0.0.1:8897 \
//	  --output     /tmp/eval-candidate.json
//
// The harness is read-only against the target server: every query is a
// semantic_search call. Use against a non-prod target for fair benchmarking.
//
// A/B comparison via paired t-test is added in a follow-up commit.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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

func main() {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	goldenDir := fs.String("golden-dir", "eval/golden", "directory of <repo>.jsonl golden files")
	targetURL := fs.String("target-url", "http://127.0.0.1:8897", "go-code MCP base URL (REST bridge at /api/tools)")
	output := fs.String("output", "", "JSON output path (default: stdout)")
	workers := fs.Int("workers", defaultWorkers, "concurrent HTTP workers")
	topK := fs.Int("top-k", minTopK, "top_k passed to semantic_search (≥10 for Recall@10/@20)")
	timeout := fs.Duration("timeout", defaultTimeout, "overall harness timeout")
	verFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *verFlag {
		fmt.Println("go-code-eval", version)
		return
	}

	if err := run(*goldenDir, *targetURL, *output, *workers, *topK, *timeout); err != nil {
		slog.Error("eval failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(goldenDir, targetURL, output string, workers, topK int, timeout time.Duration) error {
	if topK < minTopK {
		// Recall@20 requires the candidate pool to have at least 20 items.
		topK = minTopK
	}

	golden, err := LoadGolden(goldenDir)
	if err != nil {
		return fmt.Errorf("load golden: %w", err)
	}
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
	start := time.Now()
	results := runEval(ctx, client, golden, runnerCfg{Workers: workers, TopK: topK})
	elapsed := time.Since(start)
	slog.Info("eval complete",
		slog.Duration("elapsed", elapsed),
		slog.Int("queries", len(results)),
	)

	report := Report{
		Metadata: Metadata{
			Timestamp: time.Now().UTC(),
			TargetURL: targetURL,
			GitSHA:    detectGitSHA(),
			GoldenDir: goldenDir,
			TopK:      topK,
		},
		PerQuery:   results,
		PerRepo:    computePerRepo(results),
		Aggregates: computeAggregates(results),
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
