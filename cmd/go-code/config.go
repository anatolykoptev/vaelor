package main

import (
	"os"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-kit/env"
)

// Config holds all runtime configuration for go-code.
type Config struct {
	// HTTP server port.
	Port string

	// LLM (CLIProxyAPI) config.
	LLMURL       string
	LLMAPIKey    string
	LLMModel     string
	LLMMaxTokens int

	// GitHub API token for cloning private repos and higher rate limits.
	GithubToken string

	// Workspace directory for cloning repos.
	WorkspaceDir string

	// Max file size to parse (bytes). Files larger than this are skipped.
	MaxFileBytes int64

	// Max total repo size to accept for analysis (bytes).
	MaxRepoBytes int64

	// PathMappings translates external paths to container-internal paths.
	PathMappings []analyze.PathMapping

	// RedisURL is the optional Redis URL for L2 cache (e.g. redis://redis:6379/6).
	RedisURL string

	// LLMFallbackKeys are fallback API keys tried when primary gets 429/5xx.
	LLMFallbackKeys []string

	// GithubSearchRepos are default repos for quick mode code search.
	GithubSearchRepos []string

	// OutputDir is the directory for writing large analysis results as files.
	// When set, results exceeding the inline threshold are saved here and a
	// summary with the file path is returned instead.
	OutputDir string

	// DatabaseURL is the PostgreSQL DSN for Apache AGE graph storage.
	// Empty means code_graph tool is disabled.
	DatabaseURL string

	// GraphTTLLocal is the TTL in seconds for local repo graphs.
	GraphTTLLocal int

	// GraphTTLRemote is the TTL in seconds for remote repo graphs.
	GraphTTLRemote int

	// GraphBatchSize is the batch size for graph upsert operations.
	GraphBatchSize int

	// EmbedURL is the base URL for the embedding API (e.g. http://memdb-go:8080).
	// Empty means semantic search is disabled.
	EmbedURL string

	// EmbedModel is the embedding model name (e.g. multilingual-e5-large).
	EmbedModel string

	// AutoIndexDirs are directories to scan for repos at startup (comma-separated).
	AutoIndexDirs []string

	// AutoIndexConcurrency caps the worker pool used by AutoIndex. Default 2;
	// =1 reverts to byte-identical legacy serial behavior.
	AutoIndexConcurrency int

	// AutoIndexRetryMax is the per-repo retry budget on transient embed
	// failures (deadline, 5xx, conn refused). Default 3; 0 disables retry.
	AutoIndexRetryMax int

	// AutoIndexRetryBase is the initial backoff before the first retry.
	// Doubles on each subsequent attempt. Default 5s.
	AutoIndexRetryBase time.Duration

	// OxBrowserURL is the base URL for ox-browser HTTP API (e.g. http://ox-browser:8901).
	// Empty means site_analyze tool is disabled.
	OxBrowserURL string

	// GoSearchURL is the go-search MCP endpoint for web search (e.g. http://go-search:8890/mcp).
	// Empty means web search is disabled in repo_search.
	GoSearchURL string

	// GitLabToken is the optional GitLab API token (PRIVATE-TOKEN).
	GitLabToken string

	// GitLabURL is the GitLab API base URL (default: https://gitlab.com).
	// Set for self-hosted GitLab instances.
	GitLabURL string

	// OxCodesURL is the base URL for the ox-codes search service (e.g. http://ox-codes:8902).
	// When set, code_search uses ox-codes with fallback to Go codesearch.
	OxCodesURL string

	// DesignMDDir is the base directory for design_search (contains design-md/, design-md-styles/, etc.).
	DesignMDDir string

	// DesignEmbedURL is the embedding server for design_search (e5-large, 1024-dim).
	DesignEmbedURL string

	// DesignEmbedModel is the model name for design embeddings.
	DesignEmbedModel string

	// LearningsDSN is the PostgreSQL DSN for the review_learnings store.
	// Falls back to DATABASE_URL if unset.
	LearningsDSN string

	// CodegraphSurpriseIndex enables per-edge and per-symbol surprise persistence
	// at index time (CODEGRAPH_SURPRISE_INDEX=1). Default off.
	CodegraphSurpriseIndex bool

	// EmbedPipelineCache toggles the per-file symbol-entry cache wrapped around
	// the embed pipeline (Stream 4). Default true. Set EMBED_PIPELINE_CACHE=false
	// to fall back to the byte-identical v0.32.0 indexer behavior.
	EmbedPipelineCache bool
}

const (
	defaultLLMURL       = "http://127.0.0.1:8317/v1"
	defaultLLMModel     = "gemini-3.1-flash-lite-preview"
	defaultLLMMaxTokens = 16384
	defaultWorkspaceDir = "/tmp/go-code-workspace"
	defaultEmbedModel   = "jina-code-v2"

	// 512 KB per file.
	defaultMaxFileBytesKB = 512
	bytesPerKB            = 1024

	// 200 MB per repo.
	defaultMaxRepoBytesMB = 200
	bytesPerMB            = 1024 * 1024

	// Graph defaults.
	defaultGraphTTLLocal  = 3600  // 1 hour
	defaultGraphTTLRemote = 86400 // 24 hours
	defaultGraphBatchSize = 500 // AGE UNWIND is stable to 5000+; was 5 for legacy multi-MERGE

	// Cache defaults.
	defaultToolCacheSize = 200
	defaultToolCacheTTL  = 60 // minutes
	defaultLLMCacheTTL   = 60 // minutes

	// AutoIndex defaults — keep in sync with embeddings.DefaultAutoIndexOpts.
	// Concurrency=2 starts conservative (today's serial baseline = 1) and
	// will ramp to 4 once the embed-server fan-out is load-tested.
	defaultAutoIndexConcurrency = 2
	defaultAutoIndexRetryMax    = 3
	defaultAutoIndexRetryBase   = 5 * time.Second
)

// loadConfig reads environment variables and returns a Config with defaults applied.
func loadConfig() Config {
	return Config{
		Port:                   env.Str("MCP_PORT", defaultPort),
		LLMURL:                 env.Str("LLM_API_BASE", defaultLLMURL),
		LLMAPIKey:              env.Str("LLM_API_KEY", ""),
		LLMModel:               env.Str("LLM_MODEL", defaultLLMModel),
		LLMMaxTokens:           env.Int("LLM_MAX_TOKENS", defaultLLMMaxTokens),
		GithubToken:            env.Str("GITHUB_TOKEN", ""),
		WorkspaceDir:           env.Str("WORKSPACE_DIR", defaultWorkspaceDir),
		RedisURL:               env.Str("REDIS_URL", ""),
		LLMFallbackKeys:        env.List("LLM_API_KEY_FALLBACK", ""),
		GithubSearchRepos:      env.List("GITHUB_SEARCH_REPOS", ""),
		OutputDir:              env.Str("OUTPUT_DIR", ""),
		PathMappings:           parsePathMappings(env.Str("PATH_MAPPINGS", "")),
		MaxFileBytes:           int64(env.Int("MAX_FILE_KB", defaultMaxFileBytesKB)) * bytesPerKB,
		MaxRepoBytes:           int64(env.Int("MAX_REPO_MB", defaultMaxRepoBytesMB)) * bytesPerMB,
		DatabaseURL:            env.Str("DATABASE_URL", ""),
		GraphTTLLocal:          env.Int("GRAPH_TTL_LOCAL", defaultGraphTTLLocal),
		GraphTTLRemote:         env.Int("GRAPH_TTL_REMOTE", defaultGraphTTLRemote),
		GraphBatchSize:         env.Int("GRAPH_BATCH_SIZE", defaultGraphBatchSize),
		EmbedURL:               env.Str("EMBED_URL", ""),
		EmbedModel:             env.Str("EMBED_MODEL", defaultEmbedModel),
		AutoIndexDirs:          env.List("AUTO_INDEX_DIRS", ""),
		AutoIndexConcurrency:   env.Int("AUTOINDEX_CONCURRENCY", defaultAutoIndexConcurrency),
		AutoIndexRetryMax:      env.Int("AUTOINDEX_RETRY_MAX", defaultAutoIndexRetryMax),
		AutoIndexRetryBase:     env.Duration("AUTOINDEX_RETRY_BASE", defaultAutoIndexRetryBase),
		OxBrowserURL:           env.Str("OX_BROWSER_URL", ""),
		GoSearchURL:            env.Str("GO_SEARCH_URL", ""),
		GitLabToken:            env.Str("GITLAB_TOKEN", ""),
		GitLabURL:              env.Str("GITLAB_URL", ""),
		OxCodesURL:             env.Str("OX_CODES_URL", ""),
		DesignMDDir:            env.Str("DESIGN_MD_DIR", ""),
		DesignEmbedURL:         env.Str("DESIGN_EMBED_URL", ""),
		DesignEmbedModel:       env.Str("DESIGN_EMBED_MODEL", "multilingual-e5-large"),
		LearningsDSN:           env.Str("LEARNINGS_DATABASE_URL", os.Getenv("DATABASE_URL")),
		CodegraphSurpriseIndex: env.Bool("CODEGRAPH_SURPRISE_INDEX", false),
		EmbedPipelineCache:     env.Bool("EMBED_PIPELINE_CACHE", true),
	}
}

func parsePathMappings(raw string) []analyze.PathMapping {
	if raw == "" {
		return nil
	}
	var mappings []analyze.PathMapping
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			mappings = append(mappings, analyze.PathMapping{
				External: parts[0],
				Internal: parts[1],
			})
		}
	}
	return mappings
}
