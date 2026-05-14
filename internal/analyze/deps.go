package analyze

import (
	"context"
	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/llm"

	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/websearch"
)

// defaultMaxFileBytes is the default maximum file size for parsing (512 KB).
const defaultMaxFileBytes = 512 * 1024

// PathMapping maps an external filesystem prefix to a container-internal prefix.
type PathMapping struct {
	External string
	Internal string
}

// Deps holds injected dependencies for analysis operations.
type Deps struct {
	// LLM is the client used for natural-language queries.
	LLM *llm.Client

	// MaxFileBytes is the max file size to parse. 0 uses the default.
	MaxFileBytes int64

	// GithubToken is the optional GitHub token for cloning private repos.
	GithubToken string

	// CloneTokenFunc, when non-nil, is called before each git fetch to obtain
	// a fresh credential token. Supersedes the static GithubToken for cache-hit
	// refresh calls so installation tokens (TTL ~1 h) do not cause fetch failures.
	// Set by registerTools when GitHub App credentials are configured.
	CloneTokenFunc func(ctx context.Context) (string, error)

	// WorkspaceDir is the directory used for temporary clones.
	WorkspaceDir string

	// PathMappings translates external paths to container-internal paths.
	PathMappings []PathMapping

	// ParseCache caches parsed AST results per file. Optional.
	ParseCache *cache.ParseCache

	// LLMCache caches LLM responses. Optional.
	LLMCache *cache.LLMCache

	// Forges is the multi-forge registry for search operations.
	Forges *forge.Registry

	// WebSearch is the go-search client for web-based repo discovery. Optional.
	WebSearch *websearch.Client

	// ToolCache is a generic cache for tool results (search, etc.).
	ToolCache *kitcache.Cache

	// OxCodes is the optional ox-codes search backend client.
	// When set, code_search uses ox-codes for grep, scoped, and structural search.
	OxCodes *oxcodes.Client

	// Learnings is the optional store for prior review findings.
	// When set, review tools persist and surface past verdicts.
	Learnings *learnings.Store

	// Graph returns persistent-graph-computed signals (pagerank, community, surprise)
	// for a symbol. Always non-nil; use graphx.Noop{} when no snapshot is available.
	Graph graphx.Analytics

	// Refs surfaces graph edges not carried in callgraph (HANDLES, FETCHES, TESTED_BY).
	// Always non-nil; use graphx.Noop{} when no snapshot is available.
	Refs graphx.CrossRefs

	// SymbolBooster is the optional pg_trgm symbol name searcher used to boost
	// file scores when symbols match query keywords. Optional — nil disables boosting.
	SymbolBooster SymbolNameSearcher

	// RepoKeyFunc derives the embedding store repo key from a local root path.
	// Must be set when SymbolBooster is non-nil. If nil, boosting is skipped.
	RepoKeyFunc func(root string) string
}

// maxFileBytes returns the effective file size limit.
func (d Deps) maxFileBytes() int64 {
	if d.MaxFileBytes > 0 {
		return d.MaxFileBytes
	}
	return defaultMaxFileBytes
}
