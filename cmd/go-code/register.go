package main

import (
	"os"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
func registerTools(server *mcp.Server, cfg Config) {
	parseCacheSize := envIntOrDefault("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
	llmCacheSize := envIntOrDefault("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
	llmCacheTTLMin := envIntOrDefault("LLM_CACHE_TTL_MIN", 60) //nolint:mnd // default TTL in minutes

	parseCache := cache.NewParseCache(parseCacheSize)
	llmCache := cache.NewLLMCache(llmCacheSize, time.Duration(llmCacheTTLMin)*time.Minute)

	deps := analyze.Deps{
		LLM: llm.NewClient(llm.Config{
			BaseURL: cfg.LLMURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
		}),
		MaxFileBytes: cfg.MaxFileBytes,
		GithubToken:  cfg.GithubToken,
		WorkspaceDir: cfg.WorkspaceDir,
		PathMappings: cfg.PathMappings,
		ParseCache:   parseCache,
		LLMCache:     llmCache,
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg)
	registerCodeCompare(server, cfg, deps)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps)
	registerCallTrace(server, cfg, deps)
}

func envIntOrDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
