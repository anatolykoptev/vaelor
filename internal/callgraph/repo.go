package callgraph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

const maxFileBytes = 512 * 1024

// Backend identifies which resolution pass(es) contributed edges to a
// CallGraph. Plain strings by design (see docs/adr, "cut tier/backend
// provenance stamping" — internal/tier is orphaned and neither AGE-graph
// fixture needs a richer vocabulary); named as consts here only to keep the
// literal that SETS CallGraph.Backend and the literal that later COMPARES
// against it from drifting apart. Exported (not package-private) because
// codegraph/index.go's buildAGECallGraph also stamps and compares against
// these exact values — a second unexported copy in that package would be
// the same literal-drift risk these consts exist to prevent, just moved one
// package over.
const (
	BackendTreeSitter = "tree-sitter"
	BackendGoTypes    = "tree-sitter+go/types"
	BackendSCIP       = "tree-sitter+scip"
)

// TraceRepoInput configures a full repo call chain trace.
type TraceRepoInput struct {
	Root     string
	Symbol   string
	Focus    string
	Language string
	Opts     TraceOpts

	// IncludeFieldAccess keeps heuristic argref/field-access call sites even
	// when they don't resolve to a known function symbol. Default false —
	// unresolved argref captures (`opts.Slug`, `ctx`, `localPath`) are
	// dropped. Set via the `field_access=true` MCP tool flag for legacy
	// permissive behaviour.
	IncludeFieldAccess bool

	// Refresh forces a cache bypass — re-parses the repo and re-runs
	// SCIP/go/types enrichment instead of returning the cached call graph.
	// Use when the repo has changed (git checkout, new commit) and the
	// in-memory cgCache is stale.
	Refresh bool
}

type parseResult struct {
	symbols []*parser.Symbol
	calls   []parser.CallSite
	rels    []parser.TypeRelationship
	src     []byte // raw file bytes, needed for template-ref resolution
	fileRel string // file path relative to repo root
	tplRefs []preproc.TemplateRef
}

// BuildFromRepo ingests a repo, parses files, and returns the call graph
// without tracing a specific symbol.
func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
	// Check cache first — parsing all repo files is expensive (15-60s on cold start).
	cacheKey := cgCacheKey(input)
	if !input.Refresh {
		if cached, ok := cgCache.get(cacheKey); ok {
			slog.Debug("callgraph: BuildFromRepo cache hit", slog.String("root", input.Root))
			return cached, nil
		}
	}

	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	results := parseFilesParallel(ctx, ir.Files)

	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	var allRels []parser.TypeRelationship
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
		allRels = append(allRels, r.rels...)
	}

	cg := BuildCallGraphWithOpts(allSymbols, allCalls, BuildOpts{
		IncludeFieldAccess: input.IncludeFieldAccess,
	})
	cg.TypeRels = allRels
	cg.Tier = "basic"
	cg.Backend = BackendTreeSitter

	// Attempt go/types resolution for Go modules, falling back to SCIP for
	// non-Go languages (or when go/types made no progress) — both purely
	// additive. See EnrichWithTypedResolution for the composition order and
	// degrade contract; it is the single shared seam for typed enrichment.
	cg = EnrichWithTypedResolution(ctx, input.Root, cg, allSymbols, ir.Files)

	// Filter stdlib method calls (clone, unwrap, to_string, iter, …) that
	// tree-sitter captures as unresolved "external" nodes. SCIP applies the
	// same filter at conversion time (convert.go); this covers the
	// tree-sitter-only path and any edges that survived enrichment unresolved.
	// See issue #466.
	cg.Edges = FilterStdlibCalls(cg.Edges)

	// The seam bounds its go/types attempt to a 10s warm-path (fast when
	// GOCACHE is already warm). If that didn't land — Backend still isn't
	// BackendGoTypes — kick off a background goroutine that warms GOCACHE
	// and upgrades this cache entry once done (the next call_trace/
	// impact_analysis against this root will complete in <10s instead of
	// 3+ minutes).
	if goanalysis.HasGoModule(input.Root) && cg.Backend != BackendGoTypes {
		go warmGoTypesCache(input.Root, allSymbols, cgCacheKey(input))
	}

	// Inject WordPress hook edges for PHP files.
	hookRoutes := extractHookRoutes(ir.Files)
	if len(hookRoutes) > 0 {
		InjectHookEdges(cg, hookRoutes)
	}

	// Populate UsesIndex from Astro template component references.
	cg.UsesIndex = buildUsesIndex(results, input.Root)

	// Cache the result for subsequent calls within the same session.
	cgCache.set(cacheKey, cg)
	slog.Debug("callgraph: BuildFromRepo cached", slog.String("root", input.Root),
		slog.String("tier", cg.Tier))
	return cg, nil
}

// TraceRepo ingests a repo, extracts symbols and calls, builds call graph, traces from symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
	g, err := BuildFromRepo(ctx, input)
	if err != nil {
		return nil, err
	}

	result := Trace(ctx, g, input.Symbol, input.Opts)
	result.Tier = g.Tier

	return &result, nil
}

// EnrichWithTypedResolution is the single shared composition seam for typed
// call-edge enrichment: given a base (tree-sitter-only) CallGraph, it
// attempts go/types resolution for Go modules, then — only if that made no
// progress — SCIP resolution for non-Go languages, merging any successful
// pass additively via MergeCallGraphs. Route ANY new typed-edge source
// through this seam (and MergeCallGraphs) rather than composing typed
// enrichment ad hoc at a second call site; BuildFromRepo is the reference
// caller.
//
// Both passes are bounded and non-fatal: on any failure (no go.mod, cold
// GOCACHE, no indexer, timeout) base is returned with Tier/Backend
// unchanged, exactly the tree-sitter-only degrade contract callers already
// depend on. root and files are needed independently of base/symbols
// because SCIP resolution walks the raw ingested file set for the dominant
// language, not the already-parsed symbol table.
func EnrichWithTypedResolution(ctx context.Context, root string, base *CallGraph, symbols []*parser.Symbol, files []*ingest.File) *CallGraph {
	cg := base

	if goanalysis.HasGoModule(root) {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		typedCG := tryGoTypesResolution(warmCtx, root, symbols)
		warmCancel()
		if typedCG != nil {
			cg = MergeCallGraphs(cg, typedCG)
			cg.Tier = "enhanced"
			cg.Backend = BackendGoTypes
		}

		// Go IMPLEMENTS enrichment: tree-sitter cannot see structural interface
		// satisfaction (no `implements` keyword), so cg.TypeRels from parsing
		// carries only embedding (INHERITS) edges for Go. Compute (type→interface)
		// satisfaction via go/types and append it as RelImplements relationships.
		// Both call_trace (via BuildFromRepo) and code_graph (via
		// buildAGECallGraph) call this seam, so both get IMPLEMENTS edges now.
		// Non-fatal and bounded: on go.mod-absent / load-failure / timeout this
		// returns nil. See issue #467.
		cg.TypeRels = append(cg.TypeRels, ExtractGoImplements(ctx, root)...)
	}

	// Attempt SCIP resolution for non-Go languages (or when go/types failed).
	if cg.Tier == "basic" {
		if scipCG := trySCIPResolution(ctx, root, files, symbols); scipCG != nil {
			cg = MergeCallGraphs(cg, scipCG)
			cg.Tier = "enhanced"
			cg.Backend = BackendSCIP
		}
	}

	return cg
}

// tryGoTypesResolution attempts to load Go packages and resolve typed call
// edges. Returns nil on any failure — callers fall back to tree-sitter-only
// graph. On failure, bumps gocode_callgraph_gotypes_fallback_total{reason}
// so the degradation rate is visible without requiring operators to grep
// logs. Routes through goanalysis.CachedLoadPackages so a load already
// warmed by ExtractGoImplements (IMPLEMENTS, callgraph/satisfaction.go)
// against the same root within the cache TTL is reused here instead of
// re-run.
func tryGoTypesResolution(ctx context.Context, root string, tsSymbols []*parser.Symbol) *CallGraph {
	lr, err := goanalysis.CachedLoadPackages(ctx, root)
	if err != nil {
		recordGotypesFallback(err)
		slog.Warn("go/packages load failed; falling back to tree-sitter", "err", err)
		return nil
	}
	typedEdges := goanalysis.Resolve(lr.Packages)
	if len(typedEdges) == 0 {
		return nil
	}
	return ConvertToCallGraph(typedEdges, tsSymbols)
}

// buildUsesIndex resolves Astro template refs from all parse results and returns
// a map from target-file → []using-file (all paths relative to root).
func buildUsesIndex(results []parseResult, root string) map[string][]string {
	idx := make(map[string][]string)
	for _, r := range results {
		if len(r.tplRefs) == 0 {
			continue
		}
		for _, u := range ResolveTemplateRefs(r.src, r.tplRefs, r.fileRel, root) {
			idx[u.To] = append(idx[u.To], u.From)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// goTypesWarmingSet tracks repos currently being warmed to avoid duplicate goroutines.
var goTypesWarmingSet sync.Map

// buildPrewarmEnv returns the environment for the go build pre-warm subprocess.
// CGO_ENABLED=0 is required: tree-sitter grammars need C headers that are absent
// outside the container build context. Without it the build fails instantly and
// GOCACHE stays empty. With CGO_ENABLED=0 the pure-Go packages still produce
// typed object files — exactly what packages.Load needs to skip its cold-start work.
func buildPrewarmEnv() []string {
	// CGO_ENABLED=0 must come AFTER os.Environ() — append order matters in
	// exec.Cmd.Env (later entries win), and ambient CGO_ENABLED=1 must be
	// shadowed so the prewarm builds without the missing tree_sitter headers.
	return append(os.Environ(),
		"CGO_ENABLED=0",
		"GOCACHE=/tmp/go-build-cache",
		"GOPATH=/tmp/gopath",
		"GOWORK=off",
		"GOFLAGS=-mod=vendor",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// warmGoTypesCache runs go/types analysis in background to warm GOCACHE.
// When complete, it upgrades the cached CallGraph from basic to enhanced tier.
func warmGoTypesCache(root string, symbols []*parser.Symbol, cacheKey string) {
	_, alreadyWarming := goTypesWarmingSet.LoadOrStore(root, true)
	if alreadyWarming {
		return
	}
	defer goTypesWarmingSet.Delete(root)

	slog.Info("go/types: warming GOCACHE in background", "root", root)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Pre-warm GOCACHE with go build -- fills object cache without NeedTypesInfo overhead.
	// After this, packages.Load completes in <10s instead of 3+ minutes.
	slog.Info("go/types: pre-warming GOCACHE via go build", "root", root)
	buildCmd := exec.CommandContext(ctx, "go", "build", "-mod=vendor", "./...")
	buildCmd.Dir = root
	buildCmd.Env = buildPrewarmEnv()
	if berr := buildCmd.Run(); berr != nil {
		slog.Warn("go/types: go build pre-warm failed (non-fatal)", "err", berr)
		// Continue anyway -- packages.Load may still succeed from partial cache.
	}
	slog.Info("go/types: go build pre-warm done, starting packages.Load", "root", root)

	// tryGoTypesResolution now routes through goanalysis.CachedLoadPackages.
	// The synchronous 10s probe in EnrichWithTypedResolution that triggered
	// this background warm almost certainly already ran (and failed) against
	// this same root, negative-caching that failure. Evict it first so this
	// patient, now-warm-GOCACHE retry isn't short-circuited by the stale
	// cold-cache failure instead of actually re-running the load.
	goanalysis.InvalidateCachedLoad(root)
	typedCG := tryGoTypesResolution(ctx, root, symbols)
	if typedCG == nil {
		slog.Warn("go/types: background warm failed", "root", root)
		return
	}

	// Upgrade existing cache entry to enhanced tier.
	if cached, ok := cgCache.get(cacheKey); ok {
		enhanced := MergeCallGraphs(cached, typedCG)
		enhanced.Tier = "enhanced"
		enhanced.Backend = BackendGoTypes
		cgCache.set(cacheKey, enhanced)
	}
	slog.Info("go/types: GOCACHE warmed, cache upgraded to enhanced", "root", root)
}
