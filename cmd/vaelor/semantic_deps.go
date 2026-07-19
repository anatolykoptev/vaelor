package main

import (
	"log/slog"

	"github.com/anatolykoptev/go-kit/sparse"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newSemanticDeps constructs the SemanticDeps value used by semantic_search
// (and the CLI search subcommand). It is a pure extraction of the inline
// construction that lived in registerTools (register.go:242-289): the
// returned SemanticDeps is byte-identical to the previous inline construction.
//
// Both the MCP serve path (registerTools) and the CLI search subcommand call
// this function so there is a single wiring site for the semantic dependency
// graph. The function stays in cmd/vaelor (boundaries lens — wiring concern,
// not domain logic; direction cmd → internal/embeddings preserved).
//
// Parameters mirror the variables available at the original call site:
//   - cfg:        loaded Config (embed URL, model, sparse, keyword arm, etc.)
//   - deps:       the analyze.Deps built earlier in registerTools (or a
//     minimal equivalent for the CLI path)
//   - dataPool:   pgvector / relational pool (nil when DATABASE_URL unset)
//   - agePool:    Apache AGE pool (nil when DATABASE_URL unset); used by Expander
//   - graphStore: codegraph.Store (nil when DATABASE_URL unset or preflight failed)
//   - rrfWeights: pre-computed RRF weights (published separately by the caller)
//
// Returns a zero-value SemanticDeps (all nil fields) when EMBED_URL is empty
// or dataPool is nil — matching the original guard semantics.
func newSemanticDeps(
	cfg Config,
	deps analyze.Deps,
	dataPool, agePool *pgxpool.Pool,
	graphStore *codegraph.Store,
	rrfWeights embeddings.RRFWeights,
) SemanticDeps {
	if cfg.EmbedURL == "" || dataPool == nil {
		return SemanticDeps{}
	}

	ec, err := newCodeEmbedder(cfg)
	if err != nil {
		slog.Warn("embed: code client disabled", slog.Any("error", err))
		return SemanticDeps{}
	}

	es := embeddings.NewStore(dataPool)

	var pipelineOpts []embeddings.PipelineOpt
	if cfg.EmbedPipelineCache {
		pipelineOpts = append(pipelineOpts, embeddings.WithFileCache(embeddings.NewPipelineCache()))
	}
	// INDEX_BUDGET bounds the background index goroutine so a hung embed
	// server cannot keep a goroutine alive indefinitely (Fix 3).
	pipelineOpts = append(pipelineOpts, embeddings.WithIndexBudget(cfg.IndexBudget))

	// Sparse embed (P2+P4): optional SPLADE gate. When SPARSE_EMBED_URL is
	// empty the sparseClient is nil — Pipeline stays dense-only AND the P4
	// sparse retrieval arm in handleSemanticHits is skipped entirely
	// (byte-identical to pre-P4 behavior). Token auto-resolved from
	// EMBED_TOKEN env by go-kit/sparse v2 NewHTTPSparseEmbedder.
	var sparseClient sparse.SparseEmbedder
	if sc := newSparseEmbedder(cfg); sc != nil {
		sparseClient = sc
		pipelineOpts = append(pipelineOpts, embeddings.WithSparseEmbedder(sc))
		pipelineOpts = append(pipelineOpts, embeddings.WithSparseMaxBatch(cfg.SparseEmbedMaxArray))
		slog.Info("sparse embed: enabled (P4 dark-launch: rrf_weight_sparse=0.0 until A/B)",
			slog.String("url", cfg.SparseEmbedURL),
			slog.String("model", cfg.SparseEmbedModel),
			slog.Int("max_array", cfg.SparseEmbedMaxArray),
			slog.Float64("rrf_weight_sparse", rrfWeights.Sparse))
	}

	// QueryClient wraps ec with the model-correct retrieval prefix.
	// For code-rank-embed: prepends the required query prefix on EmbedQuery.
	// For all other models: returns ec unwrapped (zero overhead, no allocation).
	// Document embedding (Pipeline.Embed) always uses ec directly — never QueryClient.
	qc := embeddings.NewQueryClient(ec, cfg.EmbedModel)

	return SemanticDeps{
		Client:       ec,
		QueryClient:  qc,
		Store:        es,
		Pipeline:     embeddings.NewPipeline(ec, es, cfg.EmbedModel, pipelineOpts...),
		AnalyzeDeps:  deps,
		Expander:     embeddings.NewExpander(agePool),
		GraphStore:   graphStore,
		OxCodes:      buildOxCodesClient(cfg),
		RRFWeights:   rrfWeights,
		SparseClient: sparseClient,
		KeywordArm:   cfg.KeywordArm,
	}
}
