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
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/websearch"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
func registerTools(server *mcp.Server, cfg Config) {
	parseCacheSize := env.Int("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
	llmCacheSize := env.Int("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
	llmCacheTTLMin := env.Int("LLM_CACHE_TTL_MIN", 60) //nolint:mnd // default TTL in minutes

	parseCache := cache.NewParseCache(parseCacheSize)
	llmCache := cache.NewLLMCache(llmCacheSize, time.Duration(llmCacheTTLMin)*time.Minute)

	toolCache := kitcache.New(kitcache.Config{
		L1MaxItems:    env.Int("TOOL_CACHE_SIZE", 200), //nolint:mnd // default cache size
		L1TTL:         time.Hour,
		L2TTL:         time.Hour,
		RedisURL:      cfg.RedisURL,
		Prefix:        "gc:",
		JitterPercent: 0.1,
	})

	const defaultLLMMaxTokens = 16384

	deps := analyze.Deps{
		LLM: llm.NewClient(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel,
			llm.WithFallbackKeys(cfg.LLMFallbackKeys),
			llm.WithMaxTokens(defaultLLMMaxTokens),
		),
		MaxFileBytes: cfg.MaxFileBytes,
		GithubToken:  cfg.GithubToken,
		WorkspaceDir: cfg.WorkspaceDir,
		PathMappings: cfg.PathMappings,
		ParseCache:   parseCache,
		LLMCache:     llmCache,
		Forges:    buildForgeRegistry(cfg),
		WebSearch: buildWebSearchClient(cfg),
		ToolCache: toolCache,
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
	registerCodeCompare(server, cfg, deps)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps, &semDeps)
	registerCallTrace(server, cfg, deps, &semDeps)
	registerImpact(server, cfg, deps, &semDeps)
	registerDeadCode(server, cfg, deps)
	registerExplore(server, cfg, deps)
	registerCodeHealth(server, cfg, deps)
	registerCodeGraph(server, cfg, deps, graphStore)
	registerRepoSearch(server, cfg, deps)
	registerCodeSearch(server, cfg, deps, &semDeps)
	registerWPPluginSearch(server, cfg, deps)
	registerSemanticSearch(server, cfg, semDeps)
	registerSiteAnalyze(server, cfg)
	registerSiteCrawl(server, cfg)

	// Auto-index local repos in background.
	if semDeps.Pipeline != nil && len(cfg.AutoIndexDirs) > 0 {
		go embeddings.AutoIndex(semDeps.Pipeline, cfg.AutoIndexDirs, codegraph.GraphNameFor)
	}
}

// buildWebSearchClient creates a go-search client if configured.
func buildWebSearchClient(cfg Config) *websearch.Client {
	if cfg.GoSearchURL == "" {
		return nil
	}
	return websearch.NewClient(cfg.GoSearchURL)
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

