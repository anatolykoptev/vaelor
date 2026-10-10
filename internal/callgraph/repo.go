package callgraph

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
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
	imports []string // import paths declared in the file
	src     []byte   // raw file bytes, needed for template-ref resolution
	fileRel string   // file path relative to repo root
	tplRefs []preproc.TemplateRef
}

// BuildFromRepo ingests a repo, parses files, and returns the call graph
// without tracing a specific symbol.
//
// Delegates to BuildAndEnrich (the unified pipeline, issue #463) and caches
// the result. The background go/types warm-up is kept here because it is
// call_trace-specific (it targets the cgCache entry, not the pipeline result).
func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
	// Check cache first — parsing all repo files is expensive (15-60s on cold start).
	cacheKey := cgCacheKey(input)
	if !input.Refresh {
		if cached, entryAt, ok := cgCache.getWithAt(cacheKey, input.Root); ok {
			// The entry's Warm stamp says what was true when it was
			// WRITTEN; the per-root registry (goTypesWarm) is the live
			// authority — a warm started, finished, or failed since the
			// stamp. On divergence the caller gets a shallow copy carrying
			// the live state; the cached entry itself is never mutated
			// (round-5 race, issue #746).
			switch phase, at := warmStatus(input.Root); {
			case phase == WarmDone && cached.Warm != WarmNone && entryAt.Before(at):
				// Sibling-stale divert (rounds 7-8 of #735): a warm
				// completed after this entry was cached with a
				// pending/failed stamp. Warm claims are root-keyed, so
				// only ONE scope's entry was refreshed by the warm —
				// sibling scopes (different Focus/Language/
				// IncludeFieldAccess) still hold the stale stamp.
				// Rebuild once against the now-warm GOCACHE: fast, and
				// the rebuild returns the enhanced tier honestly.
				// The entryAt<at guard terminates the divert: entries
				// re-cached AFTER the warm carry a fresh stamp and never
				// divert again.
				slog.Info("callgraph: cache entry predates completed warm; rebuilding",
					slog.String("root", input.Root))
				// Fall through to rebuild below.
			case phase == WarmPending && cached.Warm != WarmPending:
				// A warm is in flight — pending is the live truth
				// regardless of the entry's stamp (an entry stamped
				// failed can coexist with a retried warm).
				return cloneWithWarm(cached, WarmPending), nil
			case phase == WarmFailed && cached.Warm != WarmFailed:
				// The last warm failed durably (issue #738.1): the
				// honest note is "retry cannot help", not "warming".
				return cloneWithWarm(cached, WarmFailed), nil
			case phase == WarmNone && cached.Warm != WarmNone:
				// No live record, but the entry believes a warm matters:
				// either an L2-imported entry whose registry state died
				// with the previous process, or a failed record that just
				// expired. Kick a fresh warm so the stamp stays honest —
				// claimWarm inside warmGoTypesCache suppresses duplicates.
				go warmGoTypesCache(input.Root, cached.Symbols, cacheKey)
				return cached, nil
			default:
				slog.Debug("callgraph: BuildFromRepo cache hit", slog.String("root", input.Root))
				return cached, nil
			}
		}
	}

	result, err := BuildAndEnrich(ctx, PipelineOpts{
		Root:               input.Root,
		Focus:              input.Focus,
		Language:           input.Language,
		IncludeFieldAccess: input.IncludeFieldAccess,
		MaxFileBytes:       maxFileBytes,
		TypedEnrich:        true,
	})
	if err != nil {
		return nil, err
	}
	cg := result.CG

	// Filter stdlib method calls (clone, unwrap, to_string, iter, …) that
	// tree-sitter captures as unresolved "external" nodes. SCIP applies the
	// same filter at conversion time (convert.go); this covers the
	// tree-sitter-only path and any edges that survived enrichment unresolved.
	// See issue #466.
	cg.Edges = FilterStdlibCalls(cg.Edges)

	// Reconcile the fresh graph's warm stamp with the per-root registry
	// BEFORE caching — the stamp must say what the registry knows, not
	// only what this load saw (issue #746):
	//
	//   - sync load failed (WarmPending stamped by the enrich seam): if a
	//     durable failure is still in backoff, restamp WarmFailed so the
	//     note says "retry cannot help" instead of lying for the TTL
	//     (issue #738.1). Otherwise kick the background warm — its
	//     claimWarm handles the single-flight dedup.
	//   - sync load succeeded but stayed basic (zero typed call edges):
	//     the load already did everything a warm would — publish done so
	//     sibling-scope entries stamped pending by an earlier cold call
	//     get diverted and rebuilt. No goroutine is spawned: there is
	//     nothing left to warm.
	if goanalysis.HasGoModule(input.Root) && cg.Backend != BackendGoTypes {
		if cg.Warm == WarmPending {
			if phase, _ := warmStatus(input.Root); phase == WarmFailed {
				cg.Warm = WarmFailed
			} else {
				go warmGoTypesCache(input.Root, result.Symbols, cacheKey)
			}
		} else {
			setWarmDone(input.Root)
		}
	}

	// Cache the result for subsequent calls within the same session.
	cgCache.set(cacheKey, cg, input.Root)
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
	result.TierNote = TierNote(g)
	result.Warm = g.Warm
	result.WarmCause = g.WarmCause

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
// This seam OWNS the go/packages load for the request (issue #747): it loads
// once and hands the same *LoadResult to both tryGoTypesResolution (CALLS)
// and ExtractGoImplements (IMPLEMENTS), which run sequentially. The load was
// previously shared between the two via a process-global TTL+LRU cache
// (goanalysis.CachedLoadPackages), which pinned the full go/types arena
// (NeedDeps: .Types/.TypesInfo/.Syntax for every package) for the cache TTL
// — ~2 GB per entry, size 8, OOM-killing the 3 GiB indexer ten times in two
// days. A value shared between two sequential steps of one request is a
// parameter, not process-global state; passing it makes the arena's lifetime
// the request's, so it becomes collectable the moment the request ends, with
// no TTL, no LRU, and no way for eight arenas to coexist. Concurrent requests
// and the background warm for one module now also share ONE load, and loads of
// different modules are admitted against a process-wide memory budget
// (typed_load.go); this seam releases its share once both consumers are done.
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
		cg = enrichWithGoTypes(ctx, root, cg, symbols)
	}

	// Attempt SCIP resolution for non-Go languages (or when go/types failed).
	if cg.Tier == "basic" {
		if scipCG := trySCIPResolution(ctx, root, files, symbols); scipCG != nil {
			cg = MergeCallGraphs(cg, scipCG)
			cg.Tier = "enhanced"
			cg.Backend = BackendSCIP
		}
	}
	if cg.Tier == "basic" {
		cg.TypedSkipped = typedSkipReasons(root, files)
	}

	return cg
}

// enrichWithGoTypes is the go/types leg of EnrichWithTypedResolution. It owns
// the shared load's release: deferred here, so a panic in either consumer cannot
// leave the flight held (its budget never returned, its arena pinned, every
// later request for the module joining the stale result), and so the arena is
// dropped before the SCIP leg — which can run for minutes — starts.
func enrichWithGoTypes(ctx context.Context, root string, cg *CallGraph, symbols []*parser.Symbol) *CallGraph {
	warmCtx, warmCancel := context.WithTimeout(context.Background(), syncLoadBudget)
	lr, release, loadErr := loadTypedShared(warmCtx, root, goanalysis.LoadOpts{Tests: true})
	warmCancel()
	if loadErr != nil {
		// Cold cache: the go/packages LOAD failed. Stamp the graph
		// WarmPending — BuildFromRepo reconciles it against the warm
		// registry before caching (a warm may already be running, or a
		// durable failure may be in backoff — issue #746). Do NOT call
		// ExtractGoImplements here — it would block on the same slow
		// packages.Load that already failed, burning the request's
		// remaining deadline (issue #735).
		recordGotypesFallback(loadErr)
		slog.Warn("go/packages load failed; falling back to tree-sitter", "err", loadErr)
		cg.Warm = WarmPending
		cg.WarmCause = warmCauseOf(loadErr)
		return cg
	}
	defer release()

	// Load succeeded (with or without typed call edges). In either case
	// ExtractGoImplements reuses the SAME *LoadResult the load just produced —
	// passed through the seam, not re-loaded — so it cannot block on a cold load
	// here. Running it unconditionally restores the pre-round-1 behaviour for the
	// zero-edge case (a Go module with only type declarations and no function
	// calls) — round 1's single nil-return silently dropped IMPLEMENTS on that
	// case, regressing issue #467's whole feature.
	typedCG := tryGoTypesResolution(lr, symbols)
	if typedCG != nil {
		cg = MergeCallGraphs(cg, typedCG)
		cg.Tier = "enhanced"
		cg.Backend = BackendGoTypes
	}
	// When typedCG is nil, the load succeeded with zero typed CALL edges — Tier
	// stays "basic" for CALLS (honest), and IMPLEMENTS enrichment still runs.
	cg.TypeRels = append(cg.TypeRels, ExtractGoImplements(ctx, root, lr)...)
	return cg
}

// tryGoTypesResolution resolves typed call edges from an ALREADY-LOADED
// *LoadResult (the caller — EnrichWithTypedResolution or warmGoTypesCache —
// owns the load and passes it through the seam, issue #747). It no longer
// loads go/packages itself; the process-global cache that previously shared
// one load between this CALLS pass and ExtractGoImplements' IMPLEMENTS pass
// has been removed (it pinned the go/types arena for the cache TTL,
// OOM-killing the indexer — issue #747).
//
// Returns:
//
//   - (nil, nil): goanalysis.Resolve produced zero typed call edges (e.g. a
//     Go module with only type declarations and no function calls). Nothing
//     is warming — the load already succeeded — so callers must NOT stamp
//     WarmPending, and SHOULD still call ExtractGoImplements (it reuses the
//     same *LoadResult; skipping it silently drops issue #467's IMPLEMENTS
//     feature). Bumps gocode_callgraph_gotypes_fallback_total{reason="no_edges"}
//     so this case is no longer invisible.
//
//   - (cg, nil): load succeeded with typed call edges; callers merge and mark
//     the enhanced tier.
//
// The (nil, err) load-failed case is gone — the load is upstream now, and
// callers handle its failure (EnrichWithTypedResolution stamps WarmPending;
// warmGoTypesCache stores WarmFailed in the registry) before reaching this
// function.
func tryGoTypesResolution(lr *goanalysis.LoadResult, tsSymbols []*parser.Symbol) *CallGraph {
	if lr == nil {
		return nil
	}
	typedEdges := goanalysis.ResolveWithTests(lr.Packages, lr.TestPackages)
	if len(typedEdges) == 0 {
		recordGotypesNoEdges()
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

// warmGoTypesCache runs go/types analysis in background to warm GOCACHE.
// When the load succeeds it publishes WarmDone to the per-root registry and
// refreshes THIS scope's cached entry; sibling scopes are rebuilt once on
// their next hit via the registry divert in BuildFromRepo (issue #746).
//
// Lifecycle protocol: the goroutine claims WarmPending (single-flight and
// failure backoff live in claimWarm) and finishes by storing WarmDone or
// WarmFailed — the registry, not the cache entry, is the live authority.
// The outcome counter increments exactly once via defer: the "failed"
// default covers load errors AND panics (issue #738.3); "completed" is
// assigned only at the point the upgraded entry is actually written, and
// "evicted" covers the LRU having dropped the entry meanwhile — the counter
// can no longer overstate delivery (issue #738.2).
//
// The goroutine owns its go/packages load and hands the *LoadResult through
// the seam to both tryGoTypesResolution (CALLS) and ExtractGoImplements
// (IMPLEMENTS), exactly as EnrichWithTypedResolution does on the request
// path (issue #747): the arena is collectable when the warm returns.
// goTypesLoadFn is the packages.Load seam for the background warm. Tests
// swap it to simulate load failures/panics without a real 15-minute load.
//
// The default is the shared, budget-admitted load (typed_load.go): the warm
// joins the probe's still-running load instead of building a second arena. The
// returned release func must be called once the result is no longer read.
var goTypesLoadFn = loadTypedShared

// syncLoadBudget bounds the request-path packages.Load probe in
// EnrichWithTypedResolution. 10s is the production contract — a request
// must not wait minutes for a cold GOCACHE — but tests raise it because a
// loaded CI box can exceed 10s even on a warm cache, flapping the warm
// stamp between pending and done.
var syncLoadBudget = 10 * time.Second

func warmGoTypesCache(root string, symbols []*parser.Symbol, cacheKey string) {
	if !claimWarm(root) {
		recordBackgroundWarm("skipped")
		return
	}

	outcome := "failed"
	var warmErr error
	defer func() {
		// Publish the failure BEFORE counting it — including panics, which
		// would otherwise leave the registry pinned at WarmPending forever:
		// claimWarm denies every future warm and the "retry" note lies for
		// the process lifetime (issue #746 registry redesign).
		if outcome == "failed" {
			setWarmFailed(root, warmErr)
		}
		recordBackgroundWarm(outcome)
	}()

	slog.Info("go/types: warming GOCACHE in background", "root", root)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Load once with the patient 15-minute budget and hand the *LoadResult to
	// both consumers. The synchronous 10s probe in EnrichWithTypedResolution
	// that triggered this background warm already ran (and failed) against
	// this same root; this retry re-attempts with a now-warm GOCACHE and a
	// much longer budget. No cache eviction is needed — there is no
	// process-global load cache anymore (issue #747).
	lr, release, loadErr := goTypesLoadFn(ctx, root, goanalysis.LoadOpts{Tests: true})
	if loadErr == nil {
		defer release()
	}
	if loadErr != nil {
		recordGotypesFallback(loadErr)
		warmErr = loadErr
		slog.Error("go/types: background warm failed — enrichment unavailable until backoff expiry",
			"root", root, "err", loadErr)
		return
	}
	typedCG := tryGoTypesResolution(lr, symbols)

	cached, ok := cgCache.get(cacheKey, root)
	if !ok {
		// The LRU (cgCacheMaxSize) evicted the entry between the cold call
		// and warm completion. GOCACHE is warm either way — publish done so
		// the next cold call rebuilds fast at the enhanced tier. This is
		// NOT "completed": that outcome promises a cache upgrade.
		setWarmDone(root)
		outcome = "evicted"
		slog.Info("go/types: warm done but cache entry was evicted; next call rebuilds", "root", root)
		return
	}

	// Write-once replacement (issue #746): nothing here writes through the
	// shared cached pointer. MergeCallGraphs returns a fresh graph on the
	// typed branch; the zero-edge branch copies the struct and gives
	// TypeRels a fresh backing array before appending, severing both the
	// slice-header and the backing-array alias (round-5 defect). The other
	// slice/map fields are carried over read-only — concurrent readers are
	// safe.
	var upgraded *CallGraph
	if typedCG != nil {
		upgraded = MergeCallGraphs(cached, typedCG)
		upgraded.Tier = "enhanced"
		upgraded.Backend = BackendGoTypes
	} else {
		cp := *cached
		cp.TypeRels = append([]parser.TypeRelationship(nil), cached.TypeRels...)
		upgraded = &cp
	}
	upgraded.Warm = WarmNone
	if !hasImplementsEdge(upgraded) {
		// IMPLEMENTS extraction reuses the SAME *LoadResult the warm just
		// produced. Entries built on the cold-fail path carry no IMPLEMENTS
		// (ExtractGoImplements is skipped there, issue #735); the warm
		// restores them.
		upgraded.TypeRels = append(upgraded.TypeRels, ExtractGoImplements(ctx, root, lr)...)
	}
	cgCache.set(cacheKey, upgraded, root)
	setWarmDone(root)

	outcome = "completed"
	slog.Info("go/types: GOCACHE warmed", "root", root)
}

// hasImplementsEdge reports whether cg already carries at least one
// IMPLEMENTS TypeRelationship. warmGoTypesCache uses it to decide whether to
// run ExtractGoImplements on a successful warm: entries whose sync path
// failed carry no IMPLEMENTS (the call is skipped there, issue #735) and
// need them restored; entries upgraded after a zero-edge sync load already
// have them, so re-running would duplicate.
func hasImplementsEdge(cg *CallGraph) bool {
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			return true
		}
	}
	return false
}
