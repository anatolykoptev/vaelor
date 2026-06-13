package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/sparse"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	// maxEmbedText: cap at 1500 chars (~450 tokens) so the full pre-truncation
	// text reaches the model and the line-boundary cut in buildEmbedText is
	// the truncation policy instead of a hidden token-cap chop.
	// CodeRankEmbed (code-rank-embed) supports up to 8192 tokens so 450 tokens
	// is well within the model's context window. Verified 2026-04-29:
	// embed_batch_tokens p99 stuck at 256 (= cap) under prior 256 setting,
	// indicating saturation; 1500 chars maps to ~450 tokens for typical code.
	maxEmbedText      = 1500
	maxIndexFileBytes = 512 * 1024
	indexChunkSize    = 100
)

// indexProgress tracks the progress of a background indexing run.
// All fields are accessed from concurrent goroutines (the spawned indexer
// goroutine writes; callers of IsIndexing/IndexProgress read), so they use
// atomic operations: running via atomic.Bool, done/total via atomic int64.
type indexProgress struct {
	total   int64
	done    int64
	running atomic.Bool
}

// defaultIndexBudget is the fallback timeout for the background index goroutine
// when no WithIndexBudget option is provided. 30 minutes gives ample headroom for
// the largest repos (go-code itself is ~5k symbols × multiple chunks) while still
// bounding a goroutine that is stuck waiting on a permanently-unreachable embed server.
const defaultIndexBudget = 30 * time.Minute

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client            *embed.Client
	store             *Store
	embedModel        string                                               // active embedding model name; stored alongside head_sha for cross-model reindex detection
	writeRepoState    func(ctx context.Context, repoKey, sha string) error // defaults to a closure over store.SetRepoState + embedModel; injectable for testing
	writeSparsesBatch func(ctx context.Context, rows []SparseUpdate) error // defaults to store.UpdateSparseEmbeddingsBatch; injectable for testing
	progress          sync.Map                                             // repoKey -> *indexProgress
	fileCache         *kitcache.Cache                                      // optional per-file symbol-entry cache; nil disables.
	sparseClient      sparse.SparseEmbedder                                // optional SPLADE embedder; nil disables sparse indexing (cold-path: byte-identical to dense-only)
	sparseMaxBatch    int                                                  // per-request cap for sparse server (EMBED_MAX_INPUT_ARRAY); defaults to sparseServerMaxDocs
	indexBudget       time.Duration                                        // per-goroutine timeout for IndexRepoAsyncWithTool; 0 uses defaultIndexBudget
}

// NewPipeline creates a Pipeline backed by the given client and store.
//
// Pass a non-nil fileCache via WithFileCache to enable per-file symbol-entry
// caching keyed on (repoKey, file.RelPath) and validated by file modTime+size.
// When fileCache is nil, behavior is byte-identical to the v0.32.0 baseline.
//
// model is the active embedding model name (e.g. "code-rank-embed"). It is
// stored alongside head_sha so that a model switch on next startup triggers a
// full reindex. Pass "" to retain legacy behaviour (no model tracking).
func NewPipeline(client *embed.Client, store *Store, model string, opts ...PipelineOpt) *Pipeline {
	p := &Pipeline{
		client:            client,
		store:             store,
		embedModel:        model,
		writeSparsesBatch: store.UpdateSparseEmbeddingsBatch,
	}
	// writeRepoState closes over model so the injectable fn keeps a (ctx, repoKey, sha)
	// signature (no model param) — avoids a breaking change in test injectors.
	m := model
	p.writeRepoState = func(ctx context.Context, repoKey, sha string) error {
		return store.SetRepoState(ctx, repoKey, sha, m)
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// PipelineOpt configures a Pipeline at construction time.
type PipelineOpt func(*Pipeline)

// WithFileCache wires a *kitcache.Cache to memoize per-file symbol entries.
// Validator (modTime+size) ensures stale entries are evicted on the next
// indexRepo pass after a file is touched. Pass nil to disable explicitly.
func WithFileCache(c *kitcache.Cache) PipelineOpt {
	return func(p *Pipeline) { p.fileCache = c }
}

// WithSparseEmbedder wires a SparseEmbedder into the indexing pipeline. When
// set, each batch of symbols also gets a SPLADE sparse vector, written to the
// sparse_embedding column (Phase P2). When nil (default), the pipeline is
// byte-identical to the dense-only path — no sparse calls, no sparse writes.
//
// Sparse embedding is strictly additive: the dense vector is always persisted
// regardless of sparse outcome. Failures in the sparse leg (embed call or DB
// write) log at WARN, bump gocode_sparse_embed_failures_total, and leave that
// row's sparse_embedding NULL — never blocking dense persistence or SHA advance.
//
// VocabSize guard: if e.VocabSize() != 30522 (the sparsevec column dimension),
// sparse indexing is refused (logs WARN, pipeline stays dense-only). This
// prevents out-of-range index corruption when a non-standard SPLADE head is wired.
func WithSparseEmbedder(e sparse.SparseEmbedder) PipelineOpt {
	return func(p *Pipeline) { p.sparseClient = newSparseEmbedder(e) }
}

// WithSparseMaxBatch overrides the per-request input cap for the sparse server
// (EMBED_MAX_INPUT_ARRAY on the embed-server side). The default is
// sparseServerMaxDocs (32). Override when the server cap changes without a
// go-code redeploy (via SPARSE_EMBED_MAX_ARRAY → Config.SparseEmbedMaxArray).
func WithSparseMaxBatch(n int) PipelineOpt {
	return func(p *Pipeline) {
		if n > 0 {
			p.sparseMaxBatch = n
		}
	}
}

// WithIndexBudget sets the per-goroutine deadline for IndexRepoAsyncWithTool.
// When the budget expires the background goroutine's context is cancelled,
// which propagates to embedAndUpsert and terminates the stuck goroutine.
// d=0 uses the defaultIndexBudget (30m). Set via INDEX_BUDGET env var in the
// cmd layer (e.g. "INDEX_BUDGET=45m"); test code passes short durations directly.
func WithIndexBudget(d time.Duration) PipelineOpt {
	return func(p *Pipeline) {
		if d > 0 {
			p.indexBudget = d
		}
	}
}

// withWriteRepoStateFn overrides the SetRepoState implementation used by all
// pipeline code paths. For testing only — allows injection of a failing writer
// without touching the real Postgres store.
func withWriteRepoStateFn(fn func(ctx context.Context, repoKey, sha string) error) PipelineOpt {
	return func(p *Pipeline) { p.writeRepoState = fn }
}

// withWriteSparsesBatchFn overrides the UpdateSparseEmbeddingsBatch implementation.
// For testing only — allows a spy that records rows, injects errors, or verifies
// the batch shape without a real Postgres store.
func withWriteSparsesBatchFn(fn func(ctx context.Context, rows []SparseUpdate) error) PipelineOpt {
	return func(p *Pipeline) { p.writeSparsesBatch = fn }
}

// InvalidateIfModelChanged purges code_embeddings for repoKey when the stored
// embed_model differs from the active model (p.embedModel). This ensures stale
// vectors from a previous model are not mixed with new query vectors after a
// model upgrade (they live in different embedding spaces even when dimension
// is equal). When a purge occurs, the next IncrementalSync call treats the
// repo as unindexed and runs a full re-embed.
//
// Called by autoindex.go per-repo before triggering IncrementalSync. No-op
// when embedModel is "" (legacy / test pipelines without model tracking).
func (p *Pipeline) InvalidateIfModelChanged(ctx context.Context, repoKey string) bool {
	if p.embedModel == "" {
		return false
	}
	purged, err := p.store.InvalidateRepoIfModelChanged(ctx, repoKey, p.embedModel)
	if err != nil {
		slog.Warn("embeddings: model-mismatch invalidate failed",
			slog.String("repo_key", repoKey),
			slog.String("active_model", p.embedModel),
			slog.Any("error", err))
		return false
	}
	if purged {
		slog.Info("embeddings: model changed — purged stale vectors, full reindex queued",
			slog.String("repo_key", repoKey),
			slog.String("active_model", p.embedModel))
	}
	return purged
}

// EmbedModel returns the active embedding model name configured for this pipeline.
// Returns "" for legacy pipelines created without model tracking.
// Used by semantic_search to detect stale-space hits: a stored model that does
// not match EmbedModel() means the indexed vectors are in the wrong space.
func (p *Pipeline) EmbedModel() string { return p.embedModel }

// IsIndexing returns true if background indexing is running for the given repo.
func (p *Pipeline) IsIndexing(repoKey string) bool {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return false
	}
	return v.(*indexProgress).running.Load()
}

// IndexProgress returns (done, total, running) for the given repo.
func (p *Pipeline) IndexProgress(repoKey string) (done, total int, running bool) {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return 0, 0, false
	}
	prog := v.(*indexProgress)
	return int(atomic.LoadInt64(&prog.done)), int(atomic.LoadInt64(&prog.total)), prog.running.Load()
}

// IndexRepoAsync starts background indexing if not already running.
// Returns true if indexing was started, false if already in progress.
// The background goroutine uses context.Background() (not the caller's context)
// so that client disconnects do not abort the indexing — Bug #2 fix.
//
// This is a backward-compatible wrapper around IndexRepoAsyncWithTool that passes
// tool="autoindex". Callers that know their triggering MCP tool should use
// IndexRepoAsyncWithTool directly so the cancel counter is properly attributed.
func (p *Pipeline) IndexRepoAsync(repoKey, root string) bool {
	return p.IndexRepoAsyncWithTool("autoindex", repoKey, root)
}

// IndexRepoAsyncWithTool starts background indexing if not already running, with
// tool attribution for observability. Returns true if indexing was started, false
// if already in progress.
//
// The background goroutine runs under a bounded context (p.indexBudget, default 30m)
// derived from context.Background() — decoupled from any request context (Bug #2 fix)
// but bounded so a goroutine waiting on a permanently-unreachable embed server does
// not leak indefinitely.
//
// tool is the MCP tool name that triggered this index (e.g. "semantic_search",
// "code_research"). It labels the gocode_index_cancelled_total counter so
// cancellations are attributable. Callers that do not know the tool should pass
// "autoindex".
//
// Concurrency: the check-and-claim is atomic (LoadOrStore); only one goroutine per
// repoKey runs at a time.
func (p *Pipeline) IndexRepoAsyncWithTool(tool, repoKey, root string) bool {
	// Model-fingerprint guard: purge stale vectors before indexing if the active
	// model changed since the last index. This covers the lazy per-query path
	// (semantic_search triggers indexing on first query). The AutoIndex path has
	// its own pre-loop invalidation in autoindex.go.
	p.InvalidateIfModelChanged(context.Background(), repoKey)

	prog := &indexProgress{}
	prog.running.Store(true) // claim before LoadOrStore so the winner's slot is always running=true
	if _, loaded := p.progress.LoadOrStore(repoKey, prog); loaded {
		// Slot already taken by a concurrent or still-running goroutine.
		// The loser discards prog — no goroutine is spawned.
		return false
	}
	budget := p.indexBudget
	if budget <= 0 {
		budget = defaultIndexBudget
	}
	// We won the slot. The stored prog already has running=true.
	go func() {
		defer func() {
			prog.running.Store(false)
			p.progress.Delete(repoKey)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		result, err := p.indexRepoWithTool(ctx, tool, repoKey, root, prog)
		if err != nil {
			slog.Error("background index failed",
				slog.String("repo", repoKey),
				slog.String("tool", tool),
				slog.Any("error", err))
			return
		}
		slog.Info("background index complete",
			slog.String("repo", repoKey),
			slog.String("tool", tool),
			slog.Int("indexed", result.Indexed),
			slog.Int("skipped", result.Skipped),
			slog.Int("total", result.Total))
	}()
	return true
}

// IndexResult summarizes the outcome of an indexing run.
type IndexResult struct {
	Indexed int
	Skipped int
	Total   int
}

// IndexRepo indexes all functions and methods in a repository for semantic search.
func (p *Pipeline) IndexRepo(ctx context.Context, repoKey, root string) (*IndexResult, error) {
	// Register the (repo key → path) mapping so dashboards can resolve the
	// opaque hash. IncrementalSync also calls this; the double-set is harmless.
	SetRepoInfoGauge(repoKey, root)
	return p.indexRepoWithTool(ctx, "unknown", repoKey, root, nil)
}

// shortSHA returns the first shortSHALen chars of a SHA for log messages.
const shortSHALen = 8

func shortSHA(sha string) string { return sha[:min(shortSHALen, len(sha))] }

// checkSameSHAFastPath returns (result, true) when it is safe to skip indexing
// because the repo's main branch is unchanged AND the store has ≥1 embedding.
// Returns (nil, false) when the caller must proceed to full re-index (empty store
// or DB error — the Bug #1 frozen-empty recovery path).
//
// root is the absolute filesystem path to the repo; it is used to distinguish a
// real desync (code repo with 0 stored rows) from a docs-only repo that
// legitimately has 0 embeddable symbols — the counter must only fire for the
// former.
//
// Side-effects on skip: bumps indexed_at via writeRepoState (liveness), sets
// gocode_repo_embeddings_present gauge.
// Side-effects on desync (0 rows, code repo): bumps gocode_repo_state_advanced_with_zero_embeddings_total.
func (p *Pipeline) checkSameSHAFastPath(ctx context.Context, repoKey, root, currentSHA string) (*IndexResult, bool) {
	prevSHA, err := p.store.GetRepoState(ctx, repoKey)
	if err != nil || prevSHA != currentSHA {
		return nil, false // not same-SHA — fall through
	}
	embCount, countErr := p.store.CountEmbeddings(ctx, repoKey)
	switch {
	case countErr != nil:
		slog.Warn("indexRepo: CountEmbeddings failed; falling through to re-index",
			slog.String("repo", repoKey), slog.Any("error", countErr))
		return nil, false
	case embCount > 0:
		slog.Debug("indexRepo: skip — main branch unchanged",
			slog.String("repo", repoKey), slog.String("sha", shortSHA(currentSHA)))
		if setErr := p.writeRepoState(ctx, repoKey, currentSHA); setErr != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:same-sha", setErr)
		}
		SetEmbeddingsPresentGauge(repoKey, embCount)
		return &IndexResult{}, true
	default:
		// 0 rows despite same SHA — frozen-empty desync (Bug #1).
		// Only bump the "operator-investigation-required" counter when the repo root
		// has embeddable source files. Docs-only repos (e.g. /host/src/wiki with .md only)
		// legitimately produce 0 embeddings — bumping the counter for them is a false
		// positive that fires on every boot and can cause spurious alerts.
		// The real desync class (code repo with source files but 0 stored rows) is still
		// caught — rootHasEmbeddableFiles walks the directory cheaply (early-exit on first
		// match). See also advanceStateNoEmbed which gates the counter on result.Total > 0.
		if rootHasEmbeddableFiles(root) {
			repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repoKey).Inc()
		}
		slog.Warn("indexRepo: same SHA but 0 embeddings — recovery re-index",
			slog.String("repo", repoKey), slog.String("sha", shortSHA(currentSHA)))
		return nil, false
	}
}

// advanceStateNoEmbed handles the len(toEmbed)==0 path: all parsed symbols
// matched existing hashes so nothing new needs embedding. It advances the
// repo fingerprint (SHA + indexed_at) and sets the embeddings-present gauge.
//
// Defense-in-depth: if CountEmbeddings returns 0 despite parsed symbols > 0
// (data inconsistency), the SHA is NOT advanced — the caller will retry next
// boot. This guards against advancing the SHA when the store is unexpectedly
// empty (would freeze the repo forever — same root cause as Bug #1).
func (p *Pipeline) advanceStateNoEmbed(ctx context.Context, repoKey, currentSHA string, result *IndexResult) (*IndexResult, error) {
	existingCount, countErr := p.store.CountEmbeddings(ctx, repoKey)
	if countErr != nil {
		slog.Warn("indexRepo: CountEmbeddings failed on no-embed path",
			slog.String("repo", repoKey), slog.Any("error", countErr))
	}
	if countErr == nil && existingCount == 0 && result.Total > 0 {
		// Parsed symbols exist but store is empty — data inconsistency.
		// Do not advance SHA; next boot will retry.
		repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repoKey).Inc()
		slog.Warn("indexRepo: no-embed path with 0 store rows despite parsed symbols; skipping SHA advance",
			slog.String("repo", repoKey), slog.Int("parsed", result.Total))
		return result, nil
	}
	if currentSHA != "" {
		if err := p.writeRepoState(ctx, repoKey, currentSHA); err != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:no-embed", err)
		}
	}
	SetEmbeddingsPresentGauge(repoKey, existingCount)
	return result, nil
}

// indexRepoWithTool is the internal implementation that optionally reports progress.
// tool is passed down to embedChunks for cancel-counter attribution.
func (p *Pipeline) indexRepoWithTool(
	ctx context.Context, tool, repoKey, root string, prog *indexProgress,
) (*IndexResult, error) {
	// Fast path: skip the entire parse + embed cycle when the repo's main
	// branch has not moved since the last successful index. Cuts boot-time
	// embed-server load from "48 repos × N symbols" to zero for unchanged
	// repos. A repo with no main/master/HEAD ref (non-git path) returns
	// sha="" and falls through to the full path.
	currentSHA, _ := repoMainBranchSHA(root)
	if currentSHA != "" {
		if result, skip := p.checkSameSHAFastPath(ctx, repoKey, root, currentSHA); skip {
			return result, nil
		}
	}

	symbols, files, err := p.collectSymbolsCached(ctx, repoKey, root)
	if err != nil {
		return nil, err
	}

	result := &IndexResult{Total: len(symbols)}

	existing, err := p.store.GetHashes(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("get existing hashes: %w", err)
	}

	toEmbed, seen := filterSymbols(symbols, files, existing, result)

	// Intra-key orphan reconciliation: delete rows in code_embeddings for this
	// repo_key that are NOT in the freshly-parsed symbol set.
	//
	// This is ONLY safe here — on the full-walk path — where `seen` contains the
	// COMPLETE (file_path, symbol_name) set for the repo. The same-SHA fast-path
	// above returns before reaching this point, and the no-embed short-circuit
	// below has a complete `seen` (it just has nothing new to embed).
	//
	// A partial parse (collectSymbolsCached error) returns early before this
	// point, so `seen` is always the full set when we reach this line.
	//
	// Batched anti-join, intraKeyOrphanChunkSize rows per DELETE, to avoid
	// statement_timeout on large repos (#201 lesson: data-size-bound, not param-bound).
	deleteIntraKeyOrphans(ctx, p.store, repoKey, seen)

	if prog != nil {
		atomic.StoreInt64(&prog.total, int64(len(toEmbed)))
	}

	if len(toEmbed) == 0 {
		return p.advanceStateNoEmbed(ctx, repoKey, currentSHA, result)
	}

	if err := p.embedChunks(ctx, repoKey, tool, toEmbed, result, prog); err != nil {
		return nil, err
	}

	if currentSHA != "" {
		if err := p.writeRepoState(ctx, repoKey, currentSHA); err != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:post-embed", err)
		}
	}
	SetEmbeddingsPresentGauge(repoKey, result.Indexed)

	return result, nil
}

// filterSymbols deduplicates symbols by (file:name) key and returns the subset
// that differ from the existing hash map (toEmbed) along with the complete
// parsed key set (seen). Matching symbols increment result.Skipped.
//
// seen must be the COMPLETE (file_path:symbol_name) set; callers use it for
// orphan detection (DeleteIntraKeyOrphans). Do not call on a partial parse.
func filterSymbols(
	symbols []*parser.Symbol, files []*ingest.File,
	existing map[string]uint64, result *IndexResult,
) (toEmbed []symbolEntry, seen map[string]bool) {
	seen = make(map[string]bool, len(symbols))
	for i, sym := range symbols {
		key := files[i].RelPath + ":" + sym.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		embedText := buildEmbedText(sym, files[i].RelPath)
		h := textHash(embedText)
		if prev, ok := existing[key]; ok && prev == h {
			result.Skipped++
			continue
		}
		toEmbed = append(toEmbed, symbolEntry{sym: sym, file: files[i], hash: h, embedText: embedText})
	}
	return toEmbed, seen
}

// deleteIntraKeyOrphans removes code_embeddings rows for repoKey not in parsedKeys.
// Non-fatal: logs a WARN on failure, increments the orphan-deleted counter on success.
// Separated from indexRepo to reduce cognitive complexity.
func deleteIntraKeyOrphans(ctx context.Context, store *Store, repoKey string, parsedKeys map[string]bool) {
	orphansDeleted, orphanErr := store.DeleteIntraKeyOrphans(ctx, repoKey, parsedKeys)
	if orphanErr != nil {
		slog.Warn("indexRepo: intra-key orphan delete failed",
			slog.String("repo", repoKey),
			slog.Any("error", orphanErr))
		return
	}
	if orphansDeleted > 0 {
		indexOrphansDeletedTotal.Add(float64(orphansDeleted))
		slog.Info("indexRepo: intra-key orphans deleted",
			slog.String("repo", repoKey),
			slog.Int64("deleted", orphansDeleted))
	}
}

// embedChunks processes toEmbed in indexChunkSize batches, writing dense (and
// sparse when configured) vectors to the store. Updates result.Indexed and prog
// as each chunk completes. Returns the first error encountered.
//
// Observability: each successfully committed chunk logs at Info so mid-run stalls
// are visible in the log stream ("chunk 1 committed, chunk 2 never logged"). On any
// error, if ≥1 chunk was already committed (rowsWritten > 0), RecordIndexPartialAbort
// is bumped — this is the "0→100→0→100" churn signature: rows survive but SHA is
// frozen because indexRepo only advances SHA on full success.
//
// tool is threaded through for attributable cancel accounting; pass "unknown" when
// the triggering tool is not available (legacy callers and non-async paths).
func (p *Pipeline) embedChunks(ctx context.Context, repoKey, tool string, toEmbed []symbolEntry, result *IndexResult, prog *indexProgress) error {
	chunkIdx := 0
	for start := 0; start < len(toEmbed); start += indexChunkSize {
		if ctx.Err() != nil {
			RecordIndexCancelled(tool, "chunk_loop")
			if result.Indexed > 0 {
				RecordIndexPartialAbort(repoKey)
			}
			return ctx.Err()
		}
		end := min(start+indexChunkSize, len(toEmbed))
		chunkStart := time.Now()
		indexed, err := p.embedAndUpsert(ctx, repoKey, toEmbed[start:end])
		if err != nil {
			if isContextErr(err) {
				RecordIndexCancelled(tool, "chunk_loop")
			}
			if result.Indexed > 0 {
				RecordIndexPartialAbort(repoKey)
			}
			return err
		}
		result.Indexed += indexed
		slog.Info("index chunk committed",
			slog.String("repo", repoKey),
			slog.Int("chunk_idx", chunkIdx),
			slog.Int("rows", indexed),
			slog.Duration("elapsed", time.Since(chunkStart)),
		)
		chunkIdx++
		if prog != nil {
			atomic.StoreInt64(&prog.done, int64(end))
		}
	}
	return nil
}

// embedAndUpsert embeds a chunk of symbols and upserts them into the store.
//
// Dense path: always calls p.client.Embed and writes the dense vector. Failure is fatal.
// Sparse path: when p.sparseClient != nil, also calls embedSparseBatched then writes
// each row's sparse_embedding via a separate best-effort UPDATE — completely decoupled
// from the dense INSERT. A sparse UPDATE failure:
//   - does NOT roll back the dense row (the INSERT committed independently)
//   - logs at WARN
//   - bumps gocode_sparse_embed_failures_total{stage="write"}
//   - leaves that row's sparse_embedding NULL (Phase-5 IS NULL cursor retries later)
//   - NEVER propagates fatal to embedAndUpsert or IncrementalSync
//
// The dense vector is always persisted regardless of sparse outcome.
func (p *Pipeline) embedAndUpsert(
	ctx context.Context, repoKey string, chunk []symbolEntry,
) (int, error) {
	texts := make([]string, len(chunk))
	for i, e := range chunk {
		texts[i] = e.embedText
	}

	// 1. Dense embed (load-bearing; failure aborts the chunk).
	vectors, err := p.client.Embed(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embed symbols: %w", err)
	}

	// 2. Sparse embed (additive; failure degrades ranking only, never fatal).
	//    sparseVecs[i] is zero-valued (→ NULL) when the sparse leg is disabled
	//    or fails, preserving byte-identical behaviour on the dense path.
	sparseVecs := make([]sparse.SparseVector, len(chunk)) // zero-valued = empty = NULL
	if p.sparseClient != nil {
		svecs, serr := embedSparseBatched(ctx, p.sparseClient, texts, p.sparseMaxBatch)
		if serr != nil {
			// Non-fatal: log once per chunk failure; counter already bumped inside
			// embedSparseBatched. Rows upserted with NULL sparse_embedding — the
			// Phase-5 backfill will fill them on the next resumable pass.
			slog.Warn("sparse embed failed; writing NULL sparse_embedding for chunk",
				slog.String("repo", repoKey),
				slog.Int("chunk_size", len(chunk)),
				slog.Any("error", serr))
		} else {
			sparseVecs = svecs
		}
	}

	records := make([]EmbeddingRecord, len(chunk))
	for i, e := range chunk {
		records[i] = EmbeddingRecord{
			RepoKey:         repoKey,
			FilePath:        e.file.RelPath,
			SymbolName:      e.sym.Name,
			SymbolKind:      string(e.sym.Kind),
			Language:        e.sym.Language,
			StartLine:       int(e.sym.StartLine),
			BodyHash:        e.hash,
			Embedding:       vectors[i],
			SparseEmbedding: sparseVecs[i],
		}
	}

	// 3. Dense upsert — always the source of truth; sparse decoupled below.
	if err := p.store.Upsert(ctx, records); err != nil {
		return 0, fmt.Errorf("upsert embeddings: %w", err)
	}

	// 4. Sparse write — best-effort UPDATE per row, completely independent of step 3.
	//    A failure here never aborts the batch or blocks SHA advance.
	if p.sparseClient != nil {
		p.runSparseWrites(ctx, repoKey, records, sparseVecs)
	}

	return len(chunk), nil
}

// runSparseWrites accumulates all sanitized sparse vectors for a chunk into a
// single batch UPDATE (one round-trip per chunk instead of one per row).
// Failures bump the write counter by the batch's row count and log at WARN,
// but are never propagated — leaving rows with NULL sparse_embedding is correct
// (the Phase-5 IS NULL backfill cursor will retry them later).
//
// Dense-independence invariant: this function is called AFTER p.store.Upsert
// (step 3 in embedAndUpsert) has already committed the dense rows. A failure
// here leaves sparse_embedding NULL for those rows but never rolls back or
// blocks the dense INSERT.
//
// writeSparsesBatch is used instead of p.store.UpdateSparseEmbeddingsBatch
// directly so that tests can inject a spy without a real Postgres store.
func (p *Pipeline) runSparseWrites(ctx context.Context, repoKey string, records []EmbeddingRecord, sparseVecs []sparse.SparseVector) {
	var batch []SparseUpdate
	for i, r := range records {
		sv := sparseVecs[i]
		if sv.IsEmpty() {
			continue // NULL already in DB from INSERT default; skip UPDATE
		}
		lit := SanitizeAndFormatSparseVector(sv, sparseDim)
		if lit == "" {
			// Sanitized to nothing (all-zero / all-OOB); leave NULL.
			continue
		}
		batch = append(batch, SparseUpdate{
			RepoKey:    r.RepoKey,
			FilePath:   r.FilePath,
			SymbolName: r.SymbolName,
			Literal:    lit,
		})
	}
	if len(batch) == 0 {
		return
	}
	if werr := p.writeSparsesBatch(ctx, batch); werr != nil {
		sparseEmbedFailTotal.WithLabelValues("write").Add(float64(len(batch)))
		slog.Warn("sparse batch write failed; rows stay NULL",
			slog.String("repo", repoKey),
			slog.Int("rows", len(batch)),
			slog.Any("error", werr))
	}
}

// symbolEntry pairs a symbol with its source file, precomputed hash, and embed text.
type symbolEntry struct {
	sym       *parser.Symbol
	file      *ingest.File
	hash      uint64
	embedText string
}

// isContextErr reports whether err wraps context.Canceled or context.DeadlineExceeded.
// Used by embedChunks to detect when the embed client propagated a context error
// through multiple wrapping layers (e.g. "embed symbols: ... context canceled").
func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
