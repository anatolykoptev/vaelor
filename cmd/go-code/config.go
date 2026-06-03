package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/forge"
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

	// GithubAppConfig holds optional GitHub App credentials. When all three
	// fields are set, App auth is used in place of GithubToken for GitHub API
	// calls (separate 5000/h rate-limit pool, independent from the gh CLI PAT).
	GithubAppConfig forge.AppConfig

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
	// Mutually exclusive with LLMModelFallback: when LLMModelFallback is set,
	// model chain takes precedence and key rotation is disabled.
	LLMFallbackKeys []string

	// LLMModelFallback is a CSV cross-provider model chain (e.g.
	// "gemini-3.1-flash-lite-preview,cerebras-qwen-3-235b"). When non-empty,
	// cliproxyapi routes each model id to its upstream provider, enabling
	// cross-provider failover without rotating API keys.
	// Env: LLM_MODEL_FALLBACK.
	LLMModelFallback string

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

	// EmbedHTTPTimeout is the per-request HTTP timeout for the code embed client.
	// The default (30s) is too short when the shared embed-server is under boot-time
	// load (48 repos × N symbols saturate jina-code-v2). Set via EMBED_HTTP_TIMEOUT
	// (e.g. "120s"). 0 leaves the go-kit default unchanged (30s). Values are parsed
	// by env.Duration, which accepts Go duration syntax (e.g. "2m", "90s").
	EmbedHTTPTimeout time.Duration

	// AutoIndexDirs are directories to scan for repos at startup (comma-separated).
	AutoIndexDirs []string

	// IndexBudget is the per-goroutine deadline for IndexRepoAsyncWithTool.
	// A background index goroutine waiting on a permanently-unreachable embed server
	// will run until this budget expires. Default 30m (generous for the largest repos).
	// Set via INDEX_BUDGET env (Go duration syntax, e.g. "45m", "1h").
	IndexBudget time.Duration

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

	// FlowsMax caps the number of flows extracted per repo (FLOWS_MAX, default 50).
	FlowsMax int

	// FlowsDFSDepth bounds the DFS traversal depth per flow (FLOWS_DFS_DEPTH, default 8).
	FlowsDFSDepth int

	// EmbedPipelineCache toggles the per-file symbol-entry cache wrapped around
	// the embed pipeline (Stream 4). Default true. Set EMBED_PIPELINE_CACHE=false
	// to fall back to the byte-identical v0.32.0 indexer behavior.
	EmbedPipelineCache bool

	// AnalyzeRank* control prioritizeFilesWithScores fusion (Stream 3).
	// Mode = "minmax" (default, legacy byte-identical) | "rrf" (opt-in, routes
	// signals through rerank.WeightedRRF). Default flip pending offline-harness
	// validation; do not flip in this sprint. Weights apply to the rrf path.
	AnalyzeRankFusionMode     analyze.FusionMode
	AnalyzeRankWeightBM25     float64
	AnalyzeRankWeightPageRank float64
	AnalyzeRankWeightSeed     float64

	// RRFWeightSemantic is the per-list weight applied to the semantic ranked
	// list inside MergeRRF (Stream 1). Default 1.0 — combined with
	// RRFWeightKeyword=1.0 reproduces byte-identical unweighted rerank.RRF.
	// Operators tune via RRF_WEIGHT_SEMANTIC env. Must be ≥ 0; negative values
	// panic (programmer error per go-kit/rerank.WeightedRRF contract).
	RRFWeightSemantic float64

	// RRFWeightKeyword is the per-list weight applied to the keyword ranked
	// list inside MergeRRF (Stream 1). Default 1.0. Tune via RRF_WEIGHT_KEYWORD.
	RRFWeightKeyword float64

	// RRFWeightSparse is the per-list weight applied to the SPLADE sparse
	// retrieval arm inside MergeRRF (SPLADE P4). Default 0.0 — DARK-LAUNCHED:
	// the arm is plumbed and exercised in prod but contributes nothing to
	// ranking until Phase 6 A/B clears the gate. Post-A/B recommended value:
	// 0.2–0.4 (below dense per 2026-06-01 SPLADE landscape research).
	// Tune via RRF_WEIGHT_SPARSE env. Must be ≥ 0.
	RRFWeightSparse float64

	// RRFWeightGraph is the per-list weight applied to the graph-candidate arm
	// inside MergeRRF (Phase 1 graph-first retrieval plan, 2026-06-02).
	// Default 0.0 — DARK-LAUNCHED: the arm is plumbed but contributes nothing to
	// ranking (and adds ZERO hot-path latency — the GraphCandidates call is skipped)
	// until A/B gate clears. Post-A/B recommended band: 0.2–0.4 (below dense per
	// graph-first plan ADR). Tune via RRF_WEIGHT_GRAPH env. Must be ≥ 0.
	RRFWeightGraph float64

	// KeywordArm selects the lexical retriever that feeds the Keyword slot of
	// MergeRRF. Allowed values: "grep" (default, byte-identical to today) |
	// "bm25f" (BM25F over trigram-prefiltered candidates, BM25F P4).
	// Invalid values WARN and fall back to "grep".
	// Env: KEYWORD_ARM. Dark-launch: flip to "bm25f" only after Phase 5 A/B
	// gate clears (non-inferiority on nDCG@10). Operator-ack required per git §4.
	KeywordArm string

	// SparseEmbedURL is the base URL for the SPLADE sparse-embedding server
	// (e.g. http://embed-server:8082). Empty means sparse indexing is disabled
	// (nil sparseClient in Pipeline — byte-identical dense-only cold-path).
	// Env: SPARSE_EMBED_URL.
	SparseEmbedURL string

	// SparseEmbedModel is the SPLADE model name to request from the sparse
	// server (e.g. splade-v3-distilbert). Empty falls back to the go-kit/sparse
	// default ("splade-v3-distilbert"). Env: SPARSE_EMBED_MODEL.
	SparseEmbedModel string

	// SparseEmbedMaxArray is the per-request input cap for the sparse server
	// (EMBED_MAX_INPUT_ARRAY on the embed-server side). embedSparseBatched
	// sub-batches by this value so no single /embed_sparse request exceeds it.
	// Default 32. Override via SPARSE_EMBED_MAX_ARRAY if the server cap changes
	// without a go-code redeploy. Env: SPARSE_EMBED_MAX_ARRAY.
	SparseEmbedMaxArray int

	// SparseBackfillDeadline is the per-call MCP tool deadline for sparse_backfill.
	// The previous 90s harness default was too short for bulk backfill of large
	// repos (103K rows ≈ 207 pages × ~32 embed calls/page). Default 600s (10 min).
	// Set via SPARSE_BACKFILL_DEADLINE_S (integer seconds).
	// Env: SPARSE_BACKFILL_DEADLINE_S.
	SparseBackfillDeadline time.Duration

	// Debug-investigate tool dependencies. Empty values disable the tool
	// (handler returns "configuration missing" instead of running).
	PrometheusURL string
	JaegerURL     string

	// DozorURL is the base URL for the dozor sidecar API (e.g. http://dozor:8765).
	// When set, Phase 6 of debug_investigate fetches recent log excerpts.
	// Defaults to http://dozor:8765 when empty (set DOZOR_URL=" to disable).
	DozorURL string

	// DozorAPIToken is the optional Bearer token for dozor API auth.
	// Set via DOZOR_API_TOKEN env var; empty means no auth header is sent.
	DozorAPIToken string

	// SourcemapAllowedHosts is the list of JS bundle hosts from which source
	// maps may be fetched. Empty means resolve_frame tool and POST /resolve are
	// disabled. Set via SOURCEMAP_ALLOWED_HOSTS env var (CSV).
	SourcemapAllowedHosts []string

	// Fleet runtime-image probing (fleet_versions tool and debug_investigate Phase 7).
	// All settings are safe-by-default: SSH disabled, socket is well-known location,
	// timeout is conservative. Existing operators see no behaviour change unless they
	// opt in.

	// FleetDefaultHost is the default probe target for debug_investigate Phase 7
	// when the caller does not specify a host. Reserved for P6; fleet_versions
	// always requires an explicit host (defaulting to local://). Empty = disabled.
	// Env: GOCODE_FLEET_DEFAULT_HOST
	FleetDefaultHost string

	// FleetDockerSocket is the path to the Docker Engine unix socket.
	// Env: GOCODE_FLEET_DOCKER_SOCKET (default /var/run/docker.sock)
	FleetDockerSocket string

	// FleetSSHEnable gates the ssh:// probe driver. False by default (security gate).
	// Set GOCODE_FLEET_SSH_ENABLE=true to enable ssh:// targets in fleet_versions.
	// Env: GOCODE_FLEET_SSH_ENABLE
	FleetSSHEnable bool

	// FleetSSHBinary is the path or name of the system ssh binary.
	// Env: GOCODE_FLEET_SSH_BINARY (default ssh)
	FleetSSHBinary string

	// FleetTimeout is the per-call timeout for fleet probe drivers.
	// Env: GOCODE_FLEET_TIMEOUT (default 10s)
	FleetTimeout time.Duration

	// FleetSSHHomeSrc is the source path for the ssh home shadow-copy.
	// When non-empty (together with FleetSSHHomeDst), the ssh driver copies
	// ~/.ssh files from this path to a writable directory before exec, so
	// that the OpenSSH client's strict-mode ownership check passes.
	// Typical value in the krolik container: /root/.ssh (the bind-mounted host ~/.ssh).
	// Env: GOCODE_FLEET_SSH_HOME_SRC (default "" — no shadow-copy)
	FleetSSHHomeSrc string

	// FleetSSHHomeDst is the writable parent directory for the shadow-copy.
	// A .ssh subdirectory is created inside it.
	// Typical value: /tmp/fleet-ssh-home (writable tmpfs inside the container).
	// Env: GOCODE_FLEET_SSH_HOME_DST (default "" — no shadow-copy)
	FleetSSHHomeDst string

	// FleetUpstreamDisable skips GitHub upstream changelog enrichment for TagDrift rows.
	// When false (default) and GITHUB_TOKEN is set, fleet_versions and
	// debug_investigate Phase 7 enrich each TagDrift diff with the commit range
	// between the pinned and running tags via the GitHub Compare API.
	// Set GOCODE_FLEET_UPSTREAM_DISABLE=true to skip all enrichment.
	// Env: GOCODE_FLEET_UPSTREAM_DISABLE
	FleetUpstreamDisable bool
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
	defaultGraphBatchSize = 500   // AGE UNWIND is stable to 5000+; was 5 for legacy multi-MERGE

	// Cache defaults.
	defaultToolCacheSize = 200
	defaultToolCacheTTL  = 60 // minutes
	defaultLLMCacheTTL   = 60 // minutes

	// defaultEmbedHTTPTimeout: generous default for the shared external embed host
	// (jina-code-v2 on embed.krolik.tools). Boot-time load (48 repos indexing in
	// parallel) causes p99 > 30s on a 32-text sub-batch. 120s gives ≈4× headroom
	// over the observed 30s timeout tail while still bounding a stuck goroutine.
	// Override via EMBED_HTTP_TIMEOUT env (Go duration syntax, e.g. "90s", "2m").
	defaultEmbedHTTPTimeout = 120 * time.Second

	// defaultIndexBudget: 30m per background goroutine. Generous enough for the
	// largest repos (go-code ~5k symbols at 120s/batch = ~90s total). Matches
	// the embeddings package internal constant; duplicated here so cmd env wiring
	// has a named default without importing the internal package constant directly.
	defaultIndexBudget = 30 * time.Minute

	// AutoIndex defaults — keep in sync with embeddings.DefaultAutoIndexOpts.
	// Concurrency=1 serializes autoindex embed calls onto the single-worker
	// embed backend so its queue depth stays bounded and individual requests
	// complete within defaultEmbedHTTPTimeout. Raise to 2+ only after the
	// embed backend is confirmed pool_size>1 under fleet load.
	// Override via AUTOINDEX_CONCURRENCY env.
	defaultAutoIndexConcurrency = 1
	defaultAutoIndexRetryMax    = 3
	defaultAutoIndexRetryBase   = 5 * time.Second

	// AnalyzeRank fusion defaults (Stream 3). pagerank centrality outweighs
	// pure text relevance; exact-match seed boost is auxiliary. These apply
	// only to the rrf path — minmax mode uses its own const weights in rank.go.
	defaultAnalyzeRankWeightBM25     = 1.0
	defaultAnalyzeRankWeightPageRank = 1.5
	defaultAnalyzeRankWeightSeed     = 0.5

	// RRF defaults: (1.0, 1.0) is the v0.32.0 baseline (mathematically
	// identical to plain rerank.RRF). Stream 1 makes them tunable; defaults
	// stay 1.0 so deploys are byte-identical until weights are grid-searched
	// via the offline harness.
	defaultRRFWeightSemantic = 1.0
	defaultRRFWeightKeyword  = 1.0
	// defaultRRFWeightSparse: 0.0 = DARK-LAUNCHED. The arm is plumbed (P4)
	// but contributes nothing to ranking until Phase 6 A/B validates the
	// quality gain (target p<0.05 nDCG@10 improvement). Flip to 0.2–0.4
	// post-A/B per SPLADE landscape research (2026-06-01).
	defaultRRFWeightSparse = 0.0

	// defaultRRFWeightGraph: 0.0 = DARK-LAUNCHED. The graph-candidate arm (Phase 1
	// graph-first retrieval plan, 2026-06-02) is plumbed but contributes nothing to
	// ranking until A/B gate clears (target p<0.05 nDCG@10 improvement).
	// When weight == 0 the GraphCandidates call is skipped entirely — zero added
	// hot-path latency. Post-A/B recommended band: 0.2–0.4 (below dense per plan ADR).
	// Flip via RRF_WEIGHT_GRAPH env (≥ 0, negative rejected at startup).
	defaultRRFWeightGraph = 0.0

	// defaultKeywordArm: "grep" = byte-identical to pre-BM25F behavior.
	// Dark-launched: no prod change until operator sets KEYWORD_ARM=bm25f after
	// Phase 5 A/B gate (non-inferiority on nDCG@10). Valid values: grep | bm25f.
	defaultKeywordArm = keywordArmGrep

	// keywordArm* are the allowed values for KEYWORD_ARM.
	keywordArmGrep  = "grep"
	keywordArmBM25F = "bm25f"

	// Sparse embed defaults — Phase P2 (indexing).
	// defaultSparseBackfillDeadlineS: 600s = 10 min. Chosen to cover 103K-row
	// full backfill: 207 pages × ~0.5s/page (batch write + embed) ≈ 104s worst
	// case; 600s leaves 5.8× headroom for slow embed-server or disk I/O spikes.
	defaultSparseBackfillDeadlineS = 600

	// defaultSparseEmbedModel: splade-v3-distilbert matches the embed-server
	// default and go-kit/sparse.httpSparseDefaultModel; no override needed
	// unless a second SPLADE model is deployed.
	defaultSparseEmbedModel = "splade-v3-distilbert"
	// defaultSparseEmbedMaxArray: embed-server EMBED_MAX_INPUT_ARRAY cap (32).
	// Sub-batching by this value prevents 400 "input too large" errors.
	// Override via SPARSE_EMBED_MAX_ARRAY if the server cap is raised.
	defaultSparseEmbedMaxArray = 32
)

// loadConfig reads environment variables and returns a Config with defaults applied.
// Returns an error when an env value is invalid (currently: ANALYZE_RANK_FUSION_MODE
// outside {minmax, rrf} and any negative ANALYZE_RANK_WEIGHT_*). Other env values
// fall back to documented defaults silently per the env package contract.
func loadConfig() (Config, error) {
	mode, err := parseFusionMode(env.Str("ANALYZE_RANK_FUSION_MODE", string(analyze.FusionModeMinmax)))
	if err != nil {
		return Config{}, err
	}
	wBM25, err := parseNonNegFloat("ANALYZE_RANK_WEIGHT_BM25", defaultAnalyzeRankWeightBM25)
	if err != nil {
		return Config{}, err
	}
	wPR, err := parseNonNegFloat("ANALYZE_RANK_WEIGHT_PAGERANK", defaultAnalyzeRankWeightPageRank)
	if err != nil {
		return Config{}, err
	}
	wSeed, err := parseNonNegFloat("ANALYZE_RANK_WEIGHT_SEED", defaultAnalyzeRankWeightSeed)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:                   env.Str("MCP_PORT", defaultPort),
		LLMURL:                 env.Str("LLM_API_BASE", defaultLLMURL),
		LLMAPIKey:              env.Str("LLM_API_KEY", ""),
		LLMModel:               env.Str("LLM_MODEL", defaultLLMModel),
		LLMMaxTokens:           env.Int("LLM_MAX_TOKENS", defaultLLMMaxTokens),
		GithubToken:            env.Str("GITHUB_TOKEN", ""),
		GithubAppConfig:        loadGithubAppConfig(),
		WorkspaceDir:           env.Str("WORKSPACE_DIR", defaultWorkspaceDir),
		RedisURL:               env.Str("REDIS_URL", ""),
		LLMFallbackKeys:        env.List("LLM_API_KEY_FALLBACK", ""),
		LLMModelFallback:       env.Str("LLM_MODEL_FALLBACK", ""),
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
		EmbedHTTPTimeout:       env.Duration("EMBED_HTTP_TIMEOUT", defaultEmbedHTTPTimeout),
		IndexBudget:            env.Duration("INDEX_BUDGET", defaultIndexBudget),
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
		FlowsMax:               env.Int("FLOWS_MAX", 0),       // 0 → applyConfigDefaults uses flowsMax=50
		FlowsDFSDepth:          env.Int("FLOWS_DFS_DEPTH", 0), // 0 → applyConfigDefaults uses flowsDFSDepth=8
		EmbedPipelineCache:     env.Bool("EMBED_PIPELINE_CACHE", true),

		AnalyzeRankFusionMode:     mode,
		AnalyzeRankWeightBM25:     wBM25,
		AnalyzeRankWeightPageRank: wPR,
		AnalyzeRankWeightSeed:     wSeed,

		RRFWeightSemantic:      env.Float("RRF_WEIGHT_SEMANTIC", defaultRRFWeightSemantic),
		RRFWeightKeyword:       env.Float("RRF_WEIGHT_KEYWORD", defaultRRFWeightKeyword),
		RRFWeightSparse:        env.Float("RRF_WEIGHT_SPARSE", defaultRRFWeightSparse),
		RRFWeightGraph:         env.Float("RRF_WEIGHT_GRAPH", defaultRRFWeightGraph),
		KeywordArm:             parseKeywordArm(env.Str("KEYWORD_ARM", defaultKeywordArm)),
		SparseEmbedURL:         env.Str("SPARSE_EMBED_URL", ""),
		SparseEmbedModel:       env.Str("SPARSE_EMBED_MODEL", defaultSparseEmbedModel),
		SparseEmbedMaxArray:    env.Int("SPARSE_EMBED_MAX_ARRAY", defaultSparseEmbedMaxArray),
		SparseBackfillDeadline: clampSparseBackfillDeadline(env.Int("SPARSE_BACKFILL_DEADLINE_S", defaultSparseBackfillDeadlineS)),
		PrometheusURL:          env.Str("PROMETHEUS_URL", ""),
		DozorURL:               env.Str("DOZOR_URL", "http://dozor:8765"),
		DozorAPIToken:          env.Str("DOZOR_API_TOKEN", ""),
		JaegerURL:              env.Str("JAEGER_URL", ""),
		SourcemapAllowedHosts:  env.List("SOURCEMAP_ALLOWED_HOSTS", ""),

		// Fleet probe settings — safe-by-default (SSH off, standard socket).
		FleetDefaultHost:     env.Str("GOCODE_FLEET_DEFAULT_HOST", ""),
		FleetDockerSocket:    env.Str("GOCODE_FLEET_DOCKER_SOCKET", "/var/run/docker.sock"),
		FleetSSHEnable:       env.Bool("GOCODE_FLEET_SSH_ENABLE", false),
		FleetSSHBinary:       env.Str("GOCODE_FLEET_SSH_BINARY", "ssh"),
		FleetTimeout:         env.Duration("GOCODE_FLEET_TIMEOUT", 10*time.Second),
		FleetSSHHomeSrc:      env.Str("GOCODE_FLEET_SSH_HOME_SRC", ""),
		FleetSSHHomeDst:      env.Str("GOCODE_FLEET_SSH_HOME_DST", ""),
		FleetUpstreamDisable: env.Bool("GOCODE_FLEET_UPSTREAM_DISABLE", false),
	}, nil
}

// parseFusionMode validates ANALYZE_RANK_FUSION_MODE. Empty/missing falls back
// to minmax via the caller's default; any non-empty value must be exactly
// "minmax" or "rrf" — typos must surface loudly rather than silently default.
func parseFusionMode(raw string) (analyze.FusionMode, error) {
	switch analyze.FusionMode(raw) {
	case analyze.FusionModeMinmax, analyze.FusionModeRRF:
		return analyze.FusionMode(raw), nil
	default:
		return "", fmt.Errorf("invalid ANALYZE_RANK_FUSION_MODE %q: must be %q or %q",
			raw, analyze.FusionModeMinmax, analyze.FusionModeRRF)
	}
}

// parseKeywordArm validates KEYWORD_ARM. Valid values are "grep" and "bm25f".
// Any other value WARNs and falls back to "grep" so operators see a clear signal
// without crashing (contrast parseFusionMode which returns an error — keyword arm
// misconfiguration degrades gracefully to today's behavior rather than refusing startup).
func parseKeywordArm(raw string) string {
	switch raw {
	case keywordArmGrep, keywordArmBM25F:
		return raw
	default:
		slog.Warn("invalid KEYWORD_ARM: falling back to grep",
			slog.String("value", raw),
			slog.String("allowed", keywordArmGrep+"|"+keywordArmBM25F),
		)
		return keywordArmGrep
	}
}

// RRFWeights returns the configured per-retriever weights for embeddings.MergeRRF.
// Semantic and Keyword default to 1.0 (byte-identical to the unweighted RRF
// baseline). Sparse defaults to 0.0 (dark-launched — inert until Phase 6 A/B).
func (c Config) RRFWeights() embeddings.RRFWeights {
	return embeddings.RRFWeights{
		Semantic: c.RRFWeightSemantic,
		Keyword:  c.RRFWeightKeyword,
		Sparse:   c.RRFWeightSparse,
		Graph:    c.RRFWeightGraph,
	}
}

// parseNonNegFloat reads a float env var and rejects negative values with a
// loud error. Empty falls back to def. Unparseable surfaces a typed error so
// operators see the bad value rather than silently inheriting the default.
func parseNonNegFloat(key string, def float64) (float64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid %s %g: must be ≥ 0 (omit a retriever rather than negating it)", key, v)
	}
	return v, nil
}

// loadGithubAppConfig reads GO_CODE_GITHUB_APP_ID, GO_CODE_GITHUB_APP_INSTALLATION_ID,
// and GO_CODE_GITHUB_APP_KEY_PATH from the environment. Returns a zero-value
// AppConfig (App auth disabled) when:
//   - any required env var is missing or empty
//   - the key file does not exist or cannot be read
//
// A warning is logged so operators know App auth is inactive.
func loadGithubAppConfig() forge.AppConfig {
	appID, err := strconv.ParseInt(os.Getenv("GO_CODE_GITHUB_APP_ID"), 10, 64)
	if err != nil || appID == 0 {
		return forge.AppConfig{}
	}
	installID, err := strconv.ParseInt(os.Getenv("GO_CODE_GITHUB_APP_INSTALLATION_ID"), 10, 64)
	if err != nil || installID == 0 {
		slog.Warn("GO_CODE_GITHUB_APP_ID set but GO_CODE_GITHUB_APP_INSTALLATION_ID missing; App auth disabled")
		return forge.AppConfig{}
	}

	keyPath := os.Getenv("GO_CODE_GITHUB_APP_KEY_PATH")
	if keyPath == "" {
		keyPath = "/run/secrets/go-code-app-key"
	}

	pem, err := os.ReadFile(keyPath) //nolint:gosec // path from operator-controlled env var
	if err != nil {
		slog.Warn("github app key file unreadable, App auth disabled", //nolint:gosec // G706: path is operator-supplied env var, not user input
			slog.String("path", keyPath),
			slog.Any("error", err),
		)
		return forge.AppConfig{}
	}

	slog.Info("github app auth configured", //nolint:gosec // G706: app_id/key_path from operator env, not user input
		slog.Int64("app_id", appID),
		slog.Int64("installation_id", installID),
		slog.String("key_path", keyPath),
	)
	return forge.AppConfig{
		AppID:          appID,
		InstallationID: installID,
		KeyPEM:         pem,
	}
}

// clampSparseBackfillDeadline converts SPARSE_BACKFILL_DEADLINE_S to a Duration,
// clamping ≤0 values to the default (600s). An operator setting the env to 0
// would otherwise produce a 0-duration deadline that the MCP harness treats as
// "use global default" (90s), silently re-introducing the truncation this config
// option was created to fix (root cause: 103K-row backfill cancelled after 90s).
func clampSparseBackfillDeadline(secs int) time.Duration {
	if secs <= 0 {
		return defaultSparseBackfillDeadlineS * time.Second
	}
	return time.Duration(secs) * time.Second
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
