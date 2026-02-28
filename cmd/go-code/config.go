package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
)

// Config holds all runtime configuration for go-code.
type Config struct {
	// HTTP server port.
	Port string

	// LLM (CLIProxyAPI) config.
	LLMURL    string
	LLMAPIKey string
	LLMModel  string

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

	// SearxngURL is the SearXNG instance URL for repo_search.
	SearxngURL string

	// RedisURL is the optional Redis URL for L2 cache (e.g. redis://redis:6379/6).
	RedisURL string

	// LLMFallbackKeys are fallback API keys tried when primary gets 429/5xx.
	LLMFallbackKeys []string

	// GithubSearchRepos are default repos for quick mode code search.
	GithubSearchRepos []string

	// DatabaseURL is the PostgreSQL DSN for Apache AGE graph storage.
	// Empty means code_graph tool is disabled.
	DatabaseURL string

	// GraphTTLLocal is the TTL in seconds for local repo graphs.
	GraphTTLLocal int

	// GraphTTLRemote is the TTL in seconds for remote repo graphs.
	GraphTTLRemote int

	// GraphBatchSize is the batch size for graph upsert operations.
	GraphBatchSize int
}

const (
	defaultLLMURL       = "http://127.0.0.1:8317/v1"
	defaultLLMModel     = "gemini-2.5-flash"
	defaultWorkspaceDir = "/tmp/go-code-workspace"

	// 512 KB per file.
	defaultMaxFileBytesKB = 512
	bytesPerKB            = 1024

	// 200 MB per repo.
	defaultMaxRepoBytesMB = 200
	bytesPerMB            = 1024 * 1024

	// Graph defaults.
	defaultGraphTTLLocal  = 3600  // 1 hour
	defaultGraphTTLRemote = 86400 // 24 hours
	defaultGraphBatchSize = 5
)

// loadConfig reads environment variables and returns a Config with defaults applied.
func loadConfig() Config {
	return Config{
		Port:         env("MCP_PORT", defaultPort),
		LLMURL:       env("LLM_URL", defaultLLMURL),
		LLMAPIKey:    env("LLM_API_KEY", ""),
		LLMModel:     env("LLM_MODEL", defaultLLMModel),
		GithubToken:  env("GITHUB_TOKEN", ""),
		WorkspaceDir: env("WORKSPACE_DIR", defaultWorkspaceDir),
		SearxngURL:        env("SEARXNG_URL", "http://searxng:8888"),
		RedisURL:          env("REDIS_URL", ""),
		LLMFallbackKeys:  envList("LLM_API_KEY_FALLBACK", ""),
		GithubSearchRepos: envList("GITHUB_SEARCH_REPOS", ""),
		PathMappings: parsePathMappings(env("PATH_MAPPINGS", "")),
		MaxFileBytes: int64(envInt("MAX_FILE_KB", defaultMaxFileBytesKB)) * bytesPerKB,
		MaxRepoBytes:  int64(envInt("MAX_REPO_MB", defaultMaxRepoBytesMB)) * bytesPerMB,
		DatabaseURL:    env("DATABASE_URL", ""),
		GraphTTLLocal:  envInt("GRAPH_TTL_LOCAL", defaultGraphTTLLocal),
		GraphTTLRemote: envInt("GRAPH_TTL_REMOTE", defaultGraphTTLRemote),
		GraphBatchSize: envInt("GRAPH_BATCH_SIZE", defaultGraphBatchSize),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

func envList(key, def string) []string {
	v := env(key, def)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
