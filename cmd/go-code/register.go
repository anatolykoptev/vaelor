package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/github"
	"github.com/anatolykoptev/go-code/internal/search"
	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/go-kit/llm"
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

	toolCache := cache.NewGenericCache[string](cache.GenericCacheConfig{
		MaxSize:  env.Int("TOOL_CACHE_SIZE", 200), //nolint:mnd // default cache size
		TTL:      time.Hour,
		RedisURL: cfg.RedisURL,
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
		GitHub:       github.NewClient(cfg.GithubToken),
		SearXNG:      search.NewSearXNGClient(cfg.SearxngURL),
		ToolCache:    toolCache,
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg)
	registerCodeCompare(server, cfg, deps)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps)
	registerCallTrace(server, cfg, deps)

	// Code graph (optional — needs DATABASE_URL).
	var graphStore *codegraph.Store
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			slog.Warn("code_graph: failed to connect to database, tool disabled",
				slog.Any("error", err))
		} else {
			graphStore = codegraph.NewStore(pool)
		}
	}
	registerCodeGraph(server, cfg, deps, graphStore)
	registerRepoSearch(server, cfg, deps)
}

