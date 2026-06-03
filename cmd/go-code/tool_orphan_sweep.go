package main

import (
	"context"
	"fmt"
	"log/slog"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// OrphanSweepInput is the input schema for the orphan_sweep tool.
// The tool takes no parameters — it sweeps all orphan repo_keys.
type OrphanSweepInput struct{}

// registerOrphanSweep registers the orphan_sweep MCP tool.
//
// The tool is only registered when a DB pool is available (DATABASE_URL configured).
// It is a no-op (disabled) otherwise, matching the gating pattern used by
// registerSparseBackfill and registerCodeGraph.
//
// The sweep is intentionally operator-gated (not run automatically at startup)
// because it issues a bulk DELETE across potentially tens of thousands of rows.
// The intra-key orphan reconciliation in indexRepo is safe to run automatically
// (it has the complete parsed set); this sweep deletes entire repo_keys and
// requires an explicit operator trigger. Run after removing worktrees, deregistering
// repos, or after a mass migration of checkout paths.
func registerOrphanSweep(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("orphan_sweep: DATABASE_URL not set — tool disabled")
		return
	}

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "orphan_sweep",
		Description: "Operator-initiated: delete code_embeddings rows whose repo_key has no matching code_repo_state row. " +
			"These orphans accumulate when worktrees are removed without cleanup, or when a repo's checkout path " +
			"changes (GraphNameFor hashes the root path, so a new path mints a new repo_key and the old snapshot " +
			"is never cleaned up). " +
			"SAFETY: deletes embeddings-keys-not-in-state only; never deletes code_repo_state rows. " +
			"The intra-key orphan reconciliation that runs automatically in indexRepo handles per-symbol cleanup; " +
			"this tool handles entire-repo_key cleanup. " +
			"Idempotent: re-running when clean returns 0 deleted. " +
			"Progress is observable via gocode_orphan_repo_keys on /metrics (port 9897).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ OrphanSweepInput) (*mcp.CallToolResult, error) {
		return handleOrphanSweep(ctx, deps)
	})
}

// handleOrphanSweep is the extracted handler, callable from tests.
func handleOrphanSweep(ctx context.Context, deps SemanticDeps) (*mcp.CallToolResult, error) {
	slog.Info("orphan_sweep: starting")

	// Snapshot the orphan count before the delete so the result is informative.
	before, countErr := deps.Store.CountOrphanRepoKeys(ctx)
	if countErr != nil {
		slog.Warn("orphan_sweep: pre-sweep count failed (continuing)", slog.Any("error", countErr))
		before = -1 // unknown
	}

	deleted, err := deps.Store.DeleteOrphanRepoKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan_sweep: %w", err)
	}

	// Update the gauge: after the sweep, orphan count should be 0.
	// Re-count to confirm (accounts for concurrent indexRepo adding new state rows).
	after, afterErr := deps.Store.CountOrphanRepoKeys(ctx)
	if afterErr != nil {
		slog.Warn("orphan_sweep: post-sweep count failed", slog.Any("error", afterErr))
		after = 0 // assume clean
	}
	embeddings.SetOrphanRepoKeysGauge(float64(after))

	slog.Info("orphan_sweep: complete",
		slog.Int64("orphan_keys_before", before),
		slog.Int64("rows_deleted", deleted),
		slog.Int64("orphan_keys_after", after),
	)

	return textResult(fmt.Sprintf(
		"orphan_sweep complete: orphan_repo_keys_before=%d rows_deleted=%d orphan_repo_keys_after=%d",
		before, deleted, after,
	)), nil
}
