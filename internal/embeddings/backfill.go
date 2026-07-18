package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/go-kit/sparse"

	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/strutil"
)

// Backfill metrics — pre-touched at 0 so /metrics always exposes them regardless
// of whether any backfill has run (project metrics-first rule: no silent absences).
//
// gocode_sparse_backfill_total{outcome} tracks per-symbol outcomes:
//   - "backfilled"       — sparse vector written successfully
//   - "skipped_drift"    — body_hash mismatch (disk drifted from index); row stays NULL
//   - "skipped_missing"  — repo or file not found on disk; row stays NULL
//   - "embed_failed"     — embed-server returned error; row stays NULL (retried next run)
//   - "write_failed"     — DB batch UPDATE timed out or errored (SQLSTATE 57014 or
//     other write error); embed succeeded but the row was not
//     persisted and stays NULL for retry on the next run.
//     Distinguishable from embed_failed on /metrics so alert
//     rules can identify DB write pressure vs embed-server issues.
//
// gocode_sparse_backfill_remaining is a per-call gauge set to the NULL row count
// BEFORE each page — useful for progress monitoring via /metrics.
var (
	sparseBackfillTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_sparse_backfill_total",
			Help: "Sparse-embedding backfill outcomes by result (backfilled, skipped_drift, skipped_missing, embed_failed).",
		},
		[]string{"outcome"},
	)
	sparseBackfillRemaining = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gocode_sparse_backfill_remaining",
			Help: "Number of code_embeddings rows with sparse_embedding IS NULL, sampled at the start of each backfill page.",
		},
	)
)

func init() {
	// Pre-touch all label values so the counter family is always visible on /metrics.
	for _, outcome := range []string{
		backfillOutcomeBackfilled,
		backfillOutcomeDrift,
		backfillOutcomeMissing,
		backfillOutcomeEmbedFailed,
		backfillOutcomeWriteFailed,
	} {
		sparseBackfillTotal.WithLabelValues(outcome).Add(0)
	}
}

const (
	backfillOutcomeBackfilled  = "backfilled"
	backfillOutcomeDrift       = "skipped_drift"
	backfillOutcomeMissing     = "skipped_missing"
	backfillOutcomeEmbedFailed = "embed_failed"
	backfillOutcomeWriteFailed = "write_failed"

	// backfillPageSize is the number of NULL rows fetched per SQL page.
	// Keeps peak memory bounded for large repos (103K rows ≈ 207 pages).
	backfillPageSize = 500
)

// BackfillRow is a single NULL-sparse row selected for backfill.
type BackfillRow struct {
	RepoKey    string
	FilePath   string // relative path (stored as-is in code_embeddings)
	SymbolName string
	SymbolKind string
	Language   string
	StartLine  int
	BodyHash   uint64 // stored hash (FNV-64a of buildEmbedText at index time)
}

// BackfillResult reports the outcome of a backfill run.
type BackfillResult struct {
	Backfilled   int
	SkippedDrift int
	SkippedMiss  int
	EmbedFailed  int
	WriteFailed  int // batch DB write timed out or errored; rows stay NULL for retry
	Total        int // rows examined
}

// BackfillOpts controls the behaviour of BackfillSparse.
type BackfillOpts struct {
	// RepoKey scopes the backfill to one repo (empty = all repos).
	RepoKey string
	// RepoRootLookup resolves a repoKey to its absolute disk root.
	// Returns ("", false) when the repo is not available on disk.
	// Callers build this from AUTO_INDEX_DIRS + codegraph.GraphNameFor.
	// Injected rather than computed internally to avoid an import cycle
	// between internal/embeddings and internal/codegraph.
	RepoRootLookup func(repoKey string) (root string, ok bool)
	// WriteSparsesBatch is the batch UPDATE writer. Defaults to
	// store.UpdateSparseEmbeddingsBatch. Injectable for testing.
	// A single batch covers all surviving candidates in one page (one round-trip
	// instead of one per row). Non-fatal contract: failure leaves those rows NULL
	// and they are retried on the next backfill run via the IS NULL cursor.
	WriteSparsesBatch func(ctx context.Context, rows []SparseUpdate) error
}

// resolveWriteSparsesBatch returns the effective batch writer from opts:
//   - opts.WriteSparsesBatch if set (production or test injection)
//   - store.UpdateSparseEmbeddingsBatch as the default
func (s *Store) resolveWriteSparsesBatch(opts BackfillOpts) func(context.Context, []SparseUpdate) error {
	if opts.WriteSparsesBatch != nil {
		return opts.WriteSparsesBatch
	}
	return s.UpdateSparseEmbeddingsBatch
}

// BackfillSparse populates sparse_embedding for all rows where it is NULL.
//
// Design invariants (plan P5):
//   - IS NULL cursor: re-running picks up exactly the rows still NULL (idempotent).
//   - Hash drift guard: if the freshly-computed textHash diverges from body_hash
//     the row is skipped (counted skipped_drift); we never embed stale text against
//     the stored dense vector — that would produce a semantically inconsistent pair.
//     These rows self-heal on the next incremental index, which re-embeds both.
//   - Per-item error isolation (lesson_per_item_error_freezes_batch_gate):
//     a skipped_drift or skipped_missing row NEVER aborts the page. The IS NULL
//     cursor advances because at least some rows are written; purely-bad pages
//     (all drift/missing) terminate safely via the "no candidates after filtering"
//     early-exit, which returns a zero-progress result that the caller can detect.
//   - Batch-by-32: sparse embed calls respect sparseServerMaxDocs (P2 plan §P2).
//   - Metrics-first: 4 outcome counters + 1 gauge ship with the feature so a
//     stalled or partial backfill is always observable.
func (s *Store) BackfillSparse(
	ctx context.Context,
	sparseClient sparse.SparseEmbedder,
	opts BackfillOpts,
) (*BackfillResult, error) {
	if sparseClient == nil {
		return nil, errors.New("sparse backfill: sparseClient is nil (SPARSE_EMBED_URL not configured)")
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, fmt.Errorf("sparse backfill: ensure schema: %w", err)
	}

	writeSparsesBatch := s.resolveWriteSparsesBatch(opts)
	rootLookup := opts.RepoRootLookup
	if rootLookup == nil {
		rootLookup = func(_ string) (string, bool) { return "", false }
	}

	result := &BackfillResult{}

	// Page loop: IS NULL cursor — deterministic ORDER BY ensures no row is skipped.
	// A crashed/re-run backfill picks up exactly the rows still NULL.
	// Termination: loop exits when no NULL rows remain OR when a page produces
	// zero backfilled/embed_failed rows (all-permanent-skip page — see below).
	for {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		rows, err := s.fetchNullSparseRows(ctx, opts.RepoKey, backfillPageSize)
		if err != nil {
			return result, fmt.Errorf("sparse backfill: page query: %w", err)
		}
		if len(rows) == 0 {
			break // no NULL rows left — done
		}

		// Publish remaining count before processing this page (progress gauge).
		remaining, _ := s.countNullSparse(ctx, opts.RepoKey)
		sparseBackfillRemaining.Set(float64(remaining))

		result.Total += len(rows)
		slog.Info("sparse backfill: processing page",
			slog.Int("page_size", len(rows)),
			slog.Int64("remaining", remaining),
			slog.String("repo", opts.RepoKey),
		)

		pageBackfilled, pageEmbedFailed := s.backfillPage(
			ctx, sparseClient, rows, rootLookup, writeSparsesBatch, result,
		)

		// All-permanent-skip termination: if this page produced zero DB writes
		// (neither backfilled nor embed_failed), every row was drift/missing and
		// the IS NULL cursor will return the same page indefinitely. Break to
		// avoid an infinite loop. These rows stay NULL and self-heal via the next
		// incremental index.
		if pageBackfilled == 0 && pageEmbedFailed == 0 {
			slog.Warn("sparse backfill: page produced no DB writes (all drift/missing); stopping to avoid loop",
				slog.Int("page_size", len(rows)),
				slog.String("repo", opts.RepoKey),
			)
			break
		}
	}

	sparseBackfillRemaining.Set(0)
	slog.Info("sparse backfill: complete",
		slog.String("repo", opts.RepoKey),
		slog.Int("backfilled", result.Backfilled),
		slog.Int("skipped_drift", result.SkippedDrift),
		slog.Int("skipped_missing", result.SkippedMiss),
		slog.Int("embed_failed", result.EmbedFailed),
		slog.Int("write_failed", result.WriteFailed),
		slog.Int("total", result.Total),
	)
	return result, nil
}

// backfillCandidate is a single row that passed the hash check and is ready to embed.
type backfillCandidate struct {
	row       BackfillRow
	embedText string
}

// backfillPage processes one page of NULL-sparse rows.
// Returns (backfilled, embedFailed) counts for this page — used by the caller
// to detect the all-permanent-skip termination condition.
//
// Groups rows by file to amortize disk reads, then embeds surviving candidates
// in batches of sparseServerMaxDocs (32) via embedSparseBatched. All resulting
// vectors are written in one batch UPDATE (one round-trip per page).
func (s *Store) backfillPage(
	ctx context.Context,
	sparseClient sparse.SparseEmbedder,
	rows []BackfillRow,
	rootLookup func(string) (string, bool),
	writeSparsesBatch func(ctx context.Context, rows []SparseUpdate) error,
	result *BackfillResult,
) (backfilled, embedFailed int) {
	candidates := backfillPageCandidates(rows, rootLookup, result)
	if len(candidates) == 0 {
		return 0, 0
	}
	return backfillWriteVecs(ctx, sparseClient, candidates, writeSparsesBatch, result)
}

// backfillPageCandidates groups rows by file, reads + parses each file, checks
// hash drift, and returns candidates that are ready to embed.
// Permanent-skip rows (missing repo/file/symbol, drift) are counted directly
// into result; no error is returned — these are per-item decisions, not fatals.
func backfillPageCandidates(
	rows []BackfillRow,
	rootLookup func(string) (string, bool),
	result *BackfillResult,
) []backfillCandidate {
	type fileKey struct{ repoKey, filePath string }
	byFile := make(map[fileKey][]BackfillRow, len(rows))
	for _, r := range rows {
		k := fileKey{r.RepoKey, r.FilePath}
		byFile[k] = append(byFile[k], r)
	}

	var candidates []backfillCandidate
	for fk, fileRows := range byFile {
		cs := backfillFileGroup(fk.repoKey, fk.filePath, fileRows, rootLookup, result)
		candidates = append(candidates, cs...)
	}
	return candidates
}

// backfillFileGroup processes rows for a single (repoKey, filePath) pair.
// Returns candidates that passed the hash check. Permanent-skip rows are
// counted into result.
func backfillFileGroup(
	repoKey, filePath string,
	fileRows []BackfillRow,
	rootLookup func(string) (string, bool),
	result *BackfillResult,
) []backfillCandidate {
	root, ok := rootLookup(repoKey)
	if !ok {
		for range fileRows {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeMissing).Inc()
			result.SkippedMiss++
		}
		slog.Debug("sparse backfill: repo not on disk", slog.String("repo", repoKey))
		return nil
	}

	absPath := filepath.Join(root, filePath)
	src, readErr := os.ReadFile(absPath)
	if readErr != nil {
		for range fileRows {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeMissing).Inc()
			result.SkippedMiss++
		}
		slog.Debug("sparse backfill: file not found", slog.String("file", absPath), slog.Any("error", readErr))
		return nil
	}

	pr, parseErr := parser.ParseFile(absPath, src, parser.ParseOpts{
		Language:    fileRows[0].Language,
		IncludeBody: true,
	})
	if parseErr != nil {
		for range fileRows {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeMissing).Inc()
			result.SkippedMiss++
		}
		slog.Debug("sparse backfill: parse failed", slog.String("file", absPath), slog.Any("error", parseErr))
		return nil
	}

	// Build symbol lookup: name → *parser.Symbol (first match wins, matching indexer).
	symMap := make(map[string]*parser.Symbol, len(pr.Symbols))
	for _, sym := range pr.Symbols {
		if _, exists := symMap[sym.Name]; !exists {
			symMap[sym.Name] = sym
		}
	}

	var candidates []backfillCandidate
	for _, row := range fileRows {
		sym, found := symMap[row.SymbolName]
		if !found {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeMissing).Inc()
			result.SkippedMiss++
			slog.Debug("sparse backfill: symbol not found in file",
				slog.String("symbol", row.SymbolName),
				slog.String("file", absPath),
			)
			continue
		}

		// Re-derive embed text exactly as the original indexer did.
		embedText := buildEmbedText(sym, row.FilePath)
		freshHash := strutil.TextHash(embedText)

		if freshHash != row.BodyHash {
			// Disk drifted from indexed content. Embedding stale text would
			// pair a new sparse vector with an old dense one — quality skew.
			// Leave NULL; the next incremental index self-heals both vectors.
			sparseBackfillTotal.WithLabelValues(backfillOutcomeDrift).Inc()
			result.SkippedDrift++
			slog.Debug("sparse backfill: hash drift — skip",
				slog.String("symbol", row.SymbolName),
				slog.Uint64("stored", row.BodyHash),
				slog.Uint64("fresh", freshHash),
			)
			continue
		}

		candidates = append(candidates, backfillCandidate{row: row, embedText: embedText})
	}
	return candidates
}

// backfillWriteVecs embeds candidates in batches and writes all resulting sparse
// vectors in a single batch UPDATE (one round-trip per page).
// Returns (backfilled, embedFailed) counts.
//
// Batch-write non-fatal contract: if the batch UPDATE fails, ALL rows in the
// page are counted as embed_failed and left NULL. They are retried on the next
// backfill run via the IS NULL cursor. Dense rows are never touched here.
func backfillWriteVecs(
	ctx context.Context,
	sparseClient sparse.SparseEmbedder,
	candidates []backfillCandidate,
	writeSparsesBatch func(ctx context.Context, rows []SparseUpdate) error,
	result *BackfillResult,
) (backfilled, embedFailed int) {
	texts := make([]string, len(candidates))
	for i, c := range candidates {
		texts[i] = c.embedText
	}

	// embedSparseBatched respects sparseServerMaxDocs (32) sub-batch cap.
	// On error: bump embed_failed for each candidate, return counts.
	vecs, err := embedSparseBatched(ctx, sparseClient, texts, sparseServerMaxDocs)
	if err != nil {
		// embedSparseBatched already bumped sparseEmbedFailTotal{stage="index"}.
		n := len(candidates)
		for range n {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeEmbedFailed).Inc()
		}
		result.EmbedFailed += n
		embedFailed += n
		slog.Warn("sparse backfill: embed batch failed; page candidates marked embed_failed",
			slog.Int("count", n),
			slog.Any("error", err),
		)
		return 0, embedFailed
	}

	// Build the batch: sanitize each vector; skip degenerate ones (counted as drift).
	var batch []SparseUpdate
	for i, c := range candidates {
		lit := SanitizeAndFormatSparseVector(vecs[i], sparseDim)
		if lit == "" {
			// Sanitized to empty (all-zero / all-OOB after expansion) — treat as drift.
			sparseBackfillTotal.WithLabelValues(backfillOutcomeDrift).Inc()
			result.SkippedDrift++
			continue
		}
		batch = append(batch, SparseUpdate{
			RepoKey:    c.row.RepoKey,
			FilePath:   c.row.FilePath,
			SymbolName: c.row.SymbolName,
			Literal:    lit,
		})
	}

	if len(batch) == 0 {
		return 0, 0
	}

	// One batch UPDATE for all surviving rows in this page.
	if werr := writeSparsesBatch(ctx, batch); werr != nil {
		n := len(batch)
		// Embed succeeded; the WRITE failed (e.g. SQLSTATE 57014 statement_timeout).
		// Count as write_failed — distinct from embed_failed — so dashboards can
		// separate DB write pressure from embed-server issues.
		// sparseEmbedFailTotal{stage="write"} is the pipeline-level counter for
		// all write failures (shared with the inline pipeline path); the backfill-
		// specific write_failed outcome gives per-run granularity on /metrics.
		sparseEmbedFailTotal.WithLabelValues("write").Add(float64(n))
		for range n {
			sparseBackfillTotal.WithLabelValues(backfillOutcomeWriteFailed).Inc()
		}
		result.WriteFailed += n
		embedFailed += n // embedFailed drives the all-permanent-skip termination check
		slog.Warn("sparse backfill: batch write failed; rows stay NULL (IS NULL cursor retries next run)",
			slog.Int("rows", n),
			slog.Any("error", werr),
		)
		return 0, embedFailed
	}

	n := len(batch)
	for range n {
		sparseBackfillTotal.WithLabelValues(backfillOutcomeBackfilled).Inc()
	}
	result.Backfilled += n
	backfilled += n
	return backfilled, embedFailed
}

// fetchNullSparseRows returns up to limit rows from code_embeddings where
// sparse_embedding IS NULL. ORDER BY (repo_key, file_path, symbol_name) ensures
// deterministic paging: the same set of NULL rows always comes back in the same
// order, so a partial run never silently skips rows.
func (s *Store) fetchNullSparseRows(ctx context.Context, repoKey string, limit int) ([]BackfillRow, error) {
	q := `SELECT repo_key, file_path, symbol_name, symbol_kind, language, start_line, body_hash
		FROM public.code_embeddings
		WHERE sparse_embedding IS NULL`
	args := []any{limit}
	if repoKey != "" {
		q += " AND repo_key = $2"
		args = append(args, repoKey)
	}
	q += " ORDER BY repo_key, file_path, symbol_name LIMIT $1"

	dbRows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fetchNullSparseRows: %w", err)
	}
	defer dbRows.Close()

	var rows []BackfillRow
	for dbRows.Next() {
		var r BackfillRow
		var bodyHash int64
		if err := dbRows.Scan(
			&r.RepoKey, &r.FilePath, &r.SymbolName,
			&r.SymbolKind, &r.Language, &r.StartLine, &bodyHash,
		); err != nil {
			return nil, fmt.Errorf("fetchNullSparseRows scan: %w", err)
		}
		r.BodyHash = uint64(bodyHash) //nolint:gosec // G115: body_hash is always a non-negative FNV-64a
		rows = append(rows, r)
	}
	return rows, dbRows.Err()
}

// countNullSparse returns the count of rows where sparse_embedding IS NULL,
// optionally scoped to a repo. Used for the gocode_sparse_backfill_remaining gauge.
func (s *Store) countNullSparse(ctx context.Context, repoKey string) (int64, error) {
	q := "SELECT COUNT(*) FROM public.code_embeddings WHERE sparse_embedding IS NULL"
	args := []any{}
	if repoKey != "" {
		q += " AND repo_key = $1"
		args = append(args, repoKey)
	}
	var n int64
	err := s.pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}
