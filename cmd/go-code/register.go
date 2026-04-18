package main

import (
	"context"
	"log/slog"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/go-kit/llm"

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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
// Returns the analyze.Deps for use by other components (e.g., webhook handler).
func registerTools(server *mcp.Server, cfg Config) analyze.Deps {
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

	deps := analyze.Deps{
		LLM: llm.NewClient(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel,
			llm.WithFallbackKeys(cfg.LLMFallbackKeys),
			llm.WithMaxTokens(cfg.LLMMaxTokens),
		),
		MaxFileBytes: cfg.MaxFileBytes,
		GithubToken:  cfg.GithubToken,
		WorkspaceDir: cfg.WorkspaceDir,
		PathMappings: cfg.PathMappings,
		ParseCache:   parseCache,
		LLMCache:     llmCache,
		Forges:       buildForgeRegistry(cfg),
		WebSearch:    buildWebSearchClient(cfg),
		ToolCache:    toolCache,
		OxCodes:      buildOxCodesClient(cfg),
		Learnings:    buildLearningsStore(cfg),
	}

	// Database pool (optional — needs DATABASE_URL). Shared by code_graph and semantic_search.
	var graphStore *codegraph.Store
	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		p, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			slog.Warn("database: failed to connect, code_graph and semantic_search disabled",
				slog.Any("error", err))
		} else {
			dbPool = p
			graphStore = codegraph.NewStore(dbPool)
		}
	}

	// Wire graph signals — always non-nil (Noop when no store available).
	deps.Graph, deps.Refs = buildGraphDeps(graphStore)

	// Semantic deps (optional — needs EMBED_URL + DATABASE_URL).
	// Created early so tools can use semantic fallback.
	var semDeps SemanticDeps
	if cfg.EmbedURL != "" && dbPool != nil {
		ec := embeddings.NewClient(cfg.EmbedURL, cfg.EmbedModel)
		es := embeddings.NewStore(dbPool)
		semDeps = SemanticDeps{
			Client:      ec,
			Store:       es,
			Pipeline:    embeddings.NewPipeline(ec, es),
			AnalyzeDeps: deps,
			Expander:    embeddings.NewExpander(dbPool),
		}
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg, deps)
	registerCodeCompare(server, cfg, deps, &semDeps, graphStore)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps, &semDeps)
	registerCallTrace(server, cfg, deps, &semDeps)
	registerImpact(server, cfg, deps, &semDeps)
	registerDeadCode(server, cfg, deps)
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
	registerUnderstand(server, cfg, deps, &semDeps)
	registerPrepareChange(server, cfg, deps, &semDeps)
	registerReviewDelta(server, cfg, deps)
	registerReviewPR(server, cfg, deps)
	registerRewrite(server, cfg, deps)
	registerDataflow(server, cfg, deps)
	// Design search deps (optional — needs DESIGN_EMBED_URL + DATABASE_URL).
	var designDeps DesignDeps
	if cfg.DesignEmbedURL != "" && dbPool != nil {
		designDeps = DesignDeps{
			Client: embeddings.NewClient(cfg.DesignEmbedURL, cfg.DesignEmbedModel),
			Store:  designmd.NewStore(dbPool),
		}
	}
	registerDesignSearch(server, cfg, designDeps)

	// Auto-index local repos in background.
	if semDeps.Pipeline != nil && len(cfg.AutoIndexDirs) > 0 {
		go embeddings.AutoIndex(semDeps.Pipeline, cfg.AutoIndexDirs, codegraph.GraphNameFor)
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
	reg.Register(forge.GitHub, forge.NewGitHubForge(cfg.GithubToken))
	if cfg.GitLabToken != "" || cfg.GitLabURL != "" {
		reg.Register(forge.GitLab, forge.NewGitLabForge(cfg.GitLabToken, cfg.GitLabURL))
	}
	return reg
}

// buildGraphDeps wires graphx.Analytics and graphx.CrossRefs from an optional
// codegraph.Store. Returns Noop{} for both when the store is nil (no DATABASE_URL
// or pool construction failed).
func buildGraphDeps(store *codegraph.Store) (graphx.Analytics, graphx.CrossRefs) {
	if store == nil {
		return graphx.Noop{}, graphx.Noop{}
	}
	slog.Info("graph signals enabled via codegraph.Store")
	return codegraph.NewAnalyticsAdapter(store), codegraph.NewCrossRefsAdapter(store)
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
