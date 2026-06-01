package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/go-kit/llm"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/designmd"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/websearch"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
// Returns the analyze.Deps for use by other components (e.g., webhook handler).
func registerTools(server *mcp.Server, cfg Config, reg *kitmetrics.Registry) analyze.Deps {
	parseCacheSize := env.Int("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
	llmCacheSize := env.Int("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
	llmCacheTTLMin := env.Int("LLM_CACHE_TTL_MIN", defaultLLMCacheTTL)

	parseCache := cache.NewParseCache(parseCacheSize)
	llmCache := cache.NewLLMCache(llmCacheSize, time.Duration(llmCacheTTLMin)*time.Minute)

	toolCacheTTL := time.Duration(env.Int("TOOL_CACHE_TTL_MIN", defaultToolCacheTTL)) * time.Minute
	toolCache := kitcache.New(kitcache.Config{
		L1MaxItems:    env.Int("TOOL_CACHE_SIZE", defaultToolCacheSize),
		L1TTL:         toolCacheTTL,
		L2TTL:         toolCacheTTL,
		RedisURL:      cfg.RedisURL,
		Prefix:        "gc:",
		JitterPercent: 0.1,
	})

	// Build LLM option set. Model chain and key-rotation are mutually exclusive:
	// WithEndpoints owns per-endpoint retry; WithFallbackKeys keys same-model retries.
	// When chain is configured, use it (cross-provider failure-domain via cliproxyapi
	// model routing). Otherwise fall back to key-rotation for single-provider pools.
	modelChain := llm.ParseModelFallbackChain(cfg.LLMModelFallback)
	llmOpts := []llm.Option{
		llm.WithMaxTokens(cfg.LLMMaxTokens),
		llm.WithCircuitBreaker(llm.CircuitConfig{
			FailThreshold:  5,                // 5 consecutive failures trip the breaker
			OpenDuration:   30 * time.Second, // fail-fast for 30s, then probe
			HalfOpenProbes: 1,                // one probe request before closing
		}),
		llm.WithMiddleware(newLLMObs(reg).middleware), // records gocode_llm_calls_total / gocode_llm_request_seconds
	}
	if len(modelChain) > 0 {
		// Each model in the chain is already a retry layer; cap per-endpoint retries
		// to 1 to avoid O(chain_len × retries) wall time on full outage.
		llmOpts = append(llmOpts,
			llm.WithEndpoints(llm.BuildModelChainEndpoints(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel, modelChain)),
			llm.WithMaxRetries(1),
		)
		slog.Info("llm: model chain enabled",
			slog.String("primary", cfg.LLMModel),
			slog.String("chain", cfg.LLMModelFallback),
		)
	} else {
		llmOpts = append(llmOpts, llm.WithFallbackKeys(cfg.LLMFallbackKeys))
	}

	llmClient, hasKey := llm.NewOptional(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel, llmOpts...)
	if !hasKey {
		slog.Warn("llm: disabled (LLM_API_KEY unset) — code_graph/repo_search/debug_investigate will error; narratives in call_trace/dead_code/impact omitted")
	}

	deps := analyze.Deps{
		LLM:            llmClient,
		LLMHasKey:      hasKey,
		MaxFileBytes:   cfg.MaxFileBytes,
		GithubToken:    cfg.GithubToken,
		CloneTokenFunc: buildCloneTokenFunc(cfg),
		WorkspaceDir:   cfg.WorkspaceDir,
		PathMappings:   cfg.PathMappings,
		LocalRepoDirs:  autoIndexDirs(cfg),
		ParseCache:     parseCache,
		LLMCache:       llmCache,
		Forges:         buildForgeRegistry(cfg),
		WebSearch:      buildWebSearchClient(cfg),
		ToolCache:      toolCache,
		OxCodes:        buildOxCodesClient(cfg),
		Learnings:      buildLearningsStore(cfg),
	}

	// Database pool (optional — needs DATABASE_URL). Shared by code_graph and semantic_search.
	var graphStore *codegraph.Store
	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		poolCfg, cfgErr := pgxpool.ParseConfig(cfg.DatabaseURL)
		if cfgErr != nil {
			slog.Warn("database: parse config failed", slog.Any("error", cfgErr))
		} else {
			poolCfg.MaxConns = 10 // code_graph build + concurrent queries need > default 4
			// SR-A: RESET ALL on every conn release resets every session GUC dirtied by
			// user code back to its role/database default in one command:
			// search_path (dirtied by acquireAGE / ageExpandSetup) and the
			// synchronous_commit=off + statement_timeout=0 that BulkCopyInsert sets with
			// no explicit reset (a conn returned with those active could silently lose
			// acknowledged writes on crash). RESET ALL covers all three at once.
			//
			// Why RESET ALL, NOT DISCARD ALL:
			//   The pool runs pgx's DEFAULT exec mode = QueryExecModeCacheStatement, which
			//   keeps a PER-CONNECTION server-side prepared-statement cache (the
			//   `stmtcache_<hash>` names). DISCARD ALL includes DEALLOCATE ALL, which drops
			//   those statements server-side while pgx's client-side LRU still believes
			//   they exist → the next reuse fails with `prepared statement "stmtcache_…"
			//   does not exist (SQLSTATE 26000)`. That regression broke SetRepoState on
			//   every same-sha sync (embed_repo_state_write_failures_total climbed, indexed
			//   SHA stopped persisting). RESET ALL resets GUCs ONLY — it leaves prepared
			//   statements, cursors, temp tables and plan caches intact, so pgx's cache
			//   stays consistent with the server.
			//
			// No session-level temp tables are shared across acquire/release boundaries,
			// so dropping DISCARD's temp/sequence reset costs nothing. acquireAGE re-applies
			// ageSetup on every acquire, so AGE paths work fine after the reset.
			// AfterRelease is chosen over BeforeAcquire so the conn is clean when it leaves
			// user code, not when it enters. NOTE: RESET ALL resets search_path to the pool
			// ROLE's default, not a hardcoded value — safe because the pool connects as
			// gocode_app (default `"$user", public`). If the DSN ever switched to a role
			// whose default search_path leads with ag_catalog (e.g. memos), RESET ALL alone
			// would re-leak; SR-B (public.* qualification on the data tables) is the
			// belt-and-suspenders that keeps routing correct regardless of role default.
			poolCfg.AfterRelease = func(conn *pgx.Conn) bool {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if _, err := conn.Exec(ctx, "RESET ALL"); err != nil {
					// Connection is unhealthy; destroy it rather than returning it.
					return false
				}
				return true
			}
		}
		p, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
		if err != nil {
			slog.Warn("database: failed to connect, code_graph and semantic_search disabled",
				slog.Any("error", err))
		} else {
			dbPool = p
			graphStore = codegraph.NewStore(dbPool)
			// Preflight verifies AGE is server-preloaded (#111: per-connection LOAD removed)
			// and that the role has ag_catalog USAGE + database CREATE privileges (#112).
			// Fail fast at startup so operators get clear instructions rather than a
			// cryptic permission error on the first repo index request.
			if err := graphStore.Preflight(context.Background()); err != nil {
				slog.Error("database: preflight failed", slog.Any("error", err))
				os.Exit(1)
			}
			// SR-OBS: boot-time drift guard — detect any table that leaked into
			// ag_catalog and bump gocode_schema_drift_total so alerts fire immediately.
			graphStore.AssertSchemaDrift(context.Background())
		}
	}

	// Wire graph signals — always non-nil (Noop when no store available).
	deps.Graph, deps.Refs = buildGraphDeps(graphStore, cfg.PathMappings)

	// RRF weights: published once at startup so /metrics records the deployed
	// values, and threaded into SemanticDeps so MergeRRF picks them up. Logged
	// for ops visibility — defaults (1.0, 1.0) are byte-identical to v0.32.0.
	rrfWeights := cfg.RRFWeights()
	embeddings.PublishRRFWeights(rrfWeights)
	slog.Info("rrf weights",
		slog.Float64("semantic", rrfWeights.Semantic),
		slog.Float64("keyword", rrfWeights.Keyword),
	)

	// Semantic deps (optional — needs EMBED_URL + DATABASE_URL).
	// Created early so tools can use semantic fallback.
	var semDeps SemanticDeps
	if cfg.EmbedURL != "" && dbPool != nil {
		ec, err := newCodeEmbedder(cfg)
		if err != nil {
			slog.Warn("embed: code client disabled", slog.Any("error", err))
		} else {
			es := embeddings.NewStore(dbPool)
			var pipelineOpts []embeddings.PipelineOpt
			if cfg.EmbedPipelineCache {
				pipelineOpts = append(pipelineOpts, embeddings.WithFileCache(embeddings.NewPipelineCache()))
			}
			semDeps = SemanticDeps{
				Client:      ec,
				Store:       es,
				Pipeline:    embeddings.NewPipeline(ec, es, pipelineOpts...),
				AnalyzeDeps: deps,
				Expander:    embeddings.NewExpander(dbPool),
				OxCodes:     buildOxCodesClient(cfg),
				RRFWeights:  rrfWeights,
			}
		}
	}

	// Wire pg_trgm symbol boosting for repo_analyze when embeddings are available.
	if semDeps.Store != nil {
		deps.SymbolBooster = &symbolBoostAdapter{store: semDeps.Store}
		deps.RepoKeyFunc = codegraph.GraphNameFor
		// Wire indexed-SHA resolver for WithFreshness staleness signal.
		// Captures embedStore by closure; errors collapse to "" (cold-path guarantee).
		embedStore := semDeps.Store
		deps.IndexedSHAFunc = func(ctx context.Context, repoKey string) string {
			sha, err := embedStore.GetRepoState(ctx, repoKey)
			if err != nil {
				slog.Debug("freshness: GetRepoState failed", "repo_key", repoKey, "err", err)
				return ""
			}
			return sha
		}
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg, deps)
	registerCodeCompare(server, cfg, deps, &semDeps, graphStore)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps, &semDeps)
	registerCallTrace(server, cfg, deps, &semDeps)
	registerImpact(server, cfg, deps, &semDeps)
	registerDeadCode(server, cfg, deps, graphStore)
	registerExplore(server, cfg, deps)
	registerCodeHealth(server, cfg, deps, &semDeps, graphStore)
	registerCodeGraph(server, cfg, deps, graphStore)
	registerRememberGraphInsights(server, cfg, deps, graphStore)
	registerRepoSearch(server, cfg, deps)
	registerCodeSearch(server, cfg, deps, &semDeps)
	registerWPPluginSearch(server, cfg, deps)
	registerSemanticSearch(server, cfg, semDeps)
	registerCodeResearch(server, cfg, deps, &semDeps)
	registerSiteAnalyze(server, cfg)
	registerSiteCrawl(server, cfg)
	registerUnderstand(server, cfg, deps, &semDeps, graphStore)
	registerPrepareChange(server, cfg, deps, &semDeps)
	registerReviewDelta(server, cfg, deps, graphStore)
	registerReviewPR(server, cfg, deps, graphStore)
	registerRewrite(server, cfg, deps)
	registerDataflow(server, cfg, deps)
	// Design search deps (optional — needs DESIGN_EMBED_URL + DATABASE_URL).
	var designDeps DesignDeps
	if cfg.DesignEmbedURL != "" && dbPool != nil {
		dc, err := newDesignEmbedder(cfg)
		if err != nil {
			slog.Warn("embed: design client disabled", slog.Any("error", err))
		} else {
			designDeps = DesignDeps{
				Client: dc,
				Store:  designmd.NewStore(dbPool),
			}
		}
	}
	registerDesignSearch(server, cfg, designDeps)
	registerDebugInvestigate(server, cfg, deps)
	registerFleetVersions(server, cfg, deps)
	registerResolveFrame(server, cfg)
	registerFileHealth(server, cfg, deps)
	registerSuggestReviewers(server, cfg, deps)
	registerFederatedCoChange(server, cfg, deps)

	// Auto-index local repos in background.
	if semDeps.Pipeline != nil && len(cfg.AutoIndexDirs) > 0 {
		opts := embeddings.AutoIndexOpts{
			Concurrency: cfg.AutoIndexConcurrency,
			RetryMax:    cfg.AutoIndexRetryMax,
			RetryBase:   cfg.AutoIndexRetryBase,
		}
		go embeddings.AutoIndex(semDeps.Pipeline, autoIndexDirs(cfg), codegraph.GraphNameFor, opts)
	}

	return deps
}

// buildWebSearchClient creates a go-search client if configured.
func buildWebSearchClient(cfg Config) *websearch.Client {
	if cfg.GoSearchURL == "" {
		return nil
	}
	return websearch.NewClient(cfg.GoSearchURL)
}

// buildOxCodesClient creates an ox-codes client if configured.
func buildOxCodesClient(cfg Config) *oxcodes.Client {
	if cfg.OxCodesURL == "" {
		return nil
	}
	return oxcodes.NewClient(cfg.OxCodesURL)
}

// buildForgeRegistry creates a forge registry from config.
func buildForgeRegistry(cfg Config) *forge.Registry {
	reg := forge.NewRegistry()
	reg.Register(forge.GitHub, forge.NewGitHubForge(cfg.GithubToken, cfg.GithubAppConfig))
	if cfg.GitLabToken != "" || cfg.GitLabURL != "" {
		reg.Register(forge.GitLab, forge.NewGitLabForge(cfg.GitLabToken, cfg.GitLabURL))
	}
	return reg
}

// buildGraphDeps wires graphx.Analytics and graphx.CrossRefs from an optional
// codegraph.Store. Returns Noop{} for both when the store is nil (no DATABASE_URL
// or pool construction failed).
func buildGraphDeps(store *codegraph.Store, mappings []analyze.PathMapping) (graphx.Analytics, graphx.CrossRefs) {
	if store == nil {
		return graphx.Noop{}, graphx.Noop{}
	}
	slog.Info("graph signals enabled via codegraph.Store")
	return codegraph.NewAnalyticsAdapter(store, mappings), codegraph.NewCrossRefsAdapter(store, mappings)
}

// buildLearningsStore opens a learnings.Store if configured.
// Returns nil (disabled) when LearningsDSN is empty or the pool fails to open.
func buildLearningsStore(cfg Config) *learnings.Store {
	if cfg.LearningsDSN == "" {
		return nil
	}
	ls, err := learnings.New(context.Background(), cfg.LearningsDSN, nil)
	if err != nil {
		slog.Warn("learnings store disabled", "err", err)
		return nil
	}
	return ls
}

// embeddingDims pin per-client vector dimensions for clarity/auditing. The HTTP
// backend does not validate response dims against this value (it is only
// surfaced via Dimension()), but pinning it here documents the contract:
// the code embedder MUST stay 768d to match the pgvector(768) code_embeddings
// schema; the design embedder MUST stay 1024d to match design_embeddings.
const (
	codeEmbedDim   = 768
	designEmbedDim = 1024
)

// newCodeEmbedder constructs the code-search embedder (jina-code-v2, 768d).
// Powers semantic_search, code_health, and codegraph indexing. Writes into the
// pgvector(768) code_embeddings table — must NOT be swapped for a 1024d model.
func newCodeEmbedder(cfg Config) (*embed.Client, error) {
	return embed.NewClient(cfg.EmbedURL,
		embed.WithBackend("http"),
		embed.WithModel(cfg.EmbedModel),
		embed.WithDim(codeEmbedDim),
	)
}

// newDesignEmbedder constructs the design-search embedder (multilingual-e5-large, 1024d).
// Powers design_search and the index-designs CLI. Writes into the
// pgvector(1024) design_embeddings table — must NOT be swapped for the
// code-trained 768d jina model.
func newDesignEmbedder(cfg Config) (*embed.Client, error) {
	return embed.NewClient(cfg.DesignEmbedURL,
		embed.WithBackend("http"),
		embed.WithModel(cfg.DesignEmbedModel),
		embed.WithDim(designEmbedDim),
	)
}

// symbolBoostAdapter wraps *embeddings.Store to satisfy analyze.SymbolNameSearcher.
// analyze.SymbolNameSearcher returns []analyze.SymbolHit (FilePath only), while
// embeddings.Store.SearchBySymbolName returns []embeddings.SearchResult (full record).
// This adapter lives here — co-located with the wiring — instead of a separate file.
type symbolBoostAdapter struct {
	store *embeddings.Store
}

func (a *symbolBoostAdapter) SearchBySymbolName(
	ctx context.Context,
	repoKey string,
	keywords []string,
	language string,
	limit int,
) ([]analyze.SymbolHit, error) {
	results, err := a.store.SearchBySymbolName(ctx, repoKey, keywords, language, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]analyze.SymbolHit, len(results))
	for i, r := range results {
		hits[i] = analyze.SymbolHit{FilePath: r.FilePath}
	}
	return hits, nil
}

// buildCloneTokenFunc returns a CloneTokenFunc for analyze.Deps.
// When GitHub App credentials are fully configured, returns appTokenSource.Token
// (issues ghs_ installation tokens with auto-refresh).
// Otherwise returns a static closure that yields the configured PAT.
func buildCloneTokenFunc(cfg Config) func(ctx context.Context) (string, error) {
	if cfg.GithubAppConfig.IsConfigured() {
		src, err := forge.NewAppTokenSource(forge.AppAuthConfig{
			AppID:          cfg.GithubAppConfig.AppID,
			InstallationID: cfg.GithubAppConfig.InstallationID,
			PrivateKeyPEM:  cfg.GithubAppConfig.KeyPEM,
		})
		if err != nil {
			slog.Warn("github app token source init failed; clone will use PAT fallback",
				slog.Any("error", err),
			)
			// Fall through to PAT.
		} else {
			return src.Token
		}
	}
	pat := cfg.GithubToken
	return func(_ context.Context) (string, error) {
		return pat, nil
	}
}
