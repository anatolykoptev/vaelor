# ADR 0003: Callgraph Resolver Strategy — Bounded go/types Enrichment

- **Status:** Accepted
- **Date:** 2026-07-13
- **Arc:** `plans/go-code/2026-07-02-unify-callgraph-seam-dead-code-bug-a.md` (callgraph-seam unification plan)

## Context

The in-memory callgraph and the persistent AGE-graph were built from tree-sitter call sites only. Tree-sitter sees names and call expressions, but it does not know Go type information, so it silently dropped edges that require a typed view of the program. The concrete bug classes were:

- **BUG A (homonymous methods / sibling package functions):** a call like `obj.Method()` in a package that also contains a top-level `func Method(...)` resolves by name to the wrong symbol, or the object `obj` is not known to the parser so the edge is missing entirely.
- **BUG B (func-value alias / method-value dispatch):** a package-level var `var recordEagerWarmFn = recordEagerWarm` or `var handler = s.Handle` is a CALLS target, but tree-sitter resolves the call name to the *var*, not the underlying function, and the dead-code tool then flags the real function as unused.
- **Interface dispatch:** tree-sitter records `obj.Method()` as one unresolved edge; `go/types` can expand it to every concrete implementation in the loaded transitive closure.

Both `call_trace`/`impact_analysis` (the in-memory path, `internal/callgraph/repo.go:63`) and `code_graph` (the persistent AGE-graph path, `internal/codegraph/index.go:338`) need the same typed fix. The two paths must share the same warm/cold/degrade semantics so a fix for one path is guaranteed to fix the other.

## Decisions

### 1. Single shared enrichment seam: `EnrichWithTypedResolution`

`callgraph.EnrichWithTypedResolution` (`internal/callgraph/repo.go:164`) is the only place where typed edges are merged into the base tree-sitter call graph. `BuildFromRepo` (in-memory path, `repo.go:63`) and `buildAGECallGraph` (AGE-graph path, `codegraph/index.go:338`) both call it. No other package composes `goanalysis.Resolve` directly.

This guarantees:

- One degrade contract: the base graph is returned unchanged on any failure.
- One backend vocabulary: `BackendTreeSitter`, `BackendGoTypes`, `BackendSCIP` (`internal/callgraph/repo.go:30-34`).
- One cache: `goanalysis.CachedLoadPackages` is hit whether the CALLS or IMPLEMENTS pass runs first.

### 2. Use `go/types` via `go/packages` with `NeedDeps`, not CHA/RTA/VTA

For Go, the canonical type-checker is the ground truth. `goanalysis.CachedLoadPackages` loads `packages` with `NeedDeps` and stores `*types.Package` + `*types.Info` so `goanalysis.Resolve` (`internal/goanalysis/resolver.go:45`) can extract real edges.

- **Class Hierarchy Analysis (CHA)** and **Rapid Type Analysis (RTA)** were rejected. They are whole-program pointer/flow approximations that add complexity without solving the targeted shape classes above. The `go/types` object graph already resolves direct calls, interface-to-concrete dispatch, and package-level var aliases.
- **Variable Type Analysis (VTA)** is deferred behind a *recurrence trigger*. If the current `go/types` pass leaves a reproducible class of false dead-code positives after it is live, we will reconsider VTA. Today the targeted shapes are resolved by `go/types` plus the conservative alias pass described in decision 3.

### 3. Shape classes and the shape-count trigger

`resolver_dispatch.go` (`internal/goanalysis/resolver_dispatch.go`) currently resolves three shape classes:

1. Direct function and method calls (`*types.Func` via `resolveIdent`/`resolveSelector`, `resolver_dispatch.go:18-90`).
2. Func-value aliases (`var a = f`) resolved through `resolveFuncOrAlias` and `collectFuncValueAliases` (`resolver.go:62-134`, `resolver_dispatch.go:18-31`).
3. Interface dispatch expanded to concrete implementors (`resolveInterfaceDispatch`, `resolver_dispatch.go:126-168`).

If a fourth shape-class is proposed, we will extract a small shape registry from `resolver_dispatch.go` rather than adding a fourth `if` branch. The `funcValueAliasEdgesTotal` metric (`resolver.go:40-43`) is the burn-in signal for alias usage; a new shape class needs its own counter and its own fixture before it is added.

### 4. Cache-reuse at the `go/packages` boundary

`goanalysis.CachedLoadPackages` (`internal/goanalysis/cached_loader.go:151`) wraps `go/packages.Load` with a small bounded LRU (max 8 entries, 5-minute TTL) and `singleflight` so a single `go/types` load per repo is shared by:

- `extractGoImplements` (IMPLEMENTS pass, `codegraph/satisfaction.go:53`),
- `tryGoTypesResolution` (CALLS pass, `callgraph/repo.go:199`),
- any future typed pass.

The load is decoupled from any caller context: the shared closure uses its own `context.WithTimeout` from `context.Background` (`cached_loader.go:170`), while each caller independently selects on its own deadline. This prevents a short-budget caller from cancelling a long-budget sibling or vice versa.

### 5. Bounded warm path and background warm

- `EnrichWithTypedResolution` uses a 10-second warm path (`repo.go:168`). If the `go/packages` load is not in cache (cold GOCACHE), it returns the tree-sitter graph and starts a background warm (`repo.go:254-299`). The next call against the same repo gets the enhanced graph.
- `extractGoImplements` uses a 30-second synchronous bound (`codegraph/satisfaction.go:29`) because IMPLEMENTS is already a best-effort filter; it also degrades to the heuristic.

Both non-fatal failures are metrics: `gocode_callgraph_gotypes_fallback_total{reason="deadline|load_error"}` (`callgraph/metrics.go:37-62`) and `gocode_implement_load_total{result="error"}` (`codegraph/implements_metrics.go`).

### 6. AGE-graph landing counter

`buildAGECallGraph` stamps `applied` or `degraded` on `gocode_agegraph_typed_enrich_total{result="applied|degraded"}` (`codegraph/index.go:280-293`). `applied` means the final graph's backend is `BackendGoTypes`; `degraded` means the go/types attempt did not land (including a SCIP fallback, which is not the targeted BUG A fix). This is the input to the SLO alert (see Consequences).

### 7. Default-ON gate with runtime kill switch

`typedEnrichEnabled()` (`codegraph/index.go:301-303`) returns `CODEGRAPH_TYPED_ENRICH` and defaults to `true`. The canary showed a healthy applied ratio and no load regression, so the gate is now on by default. `CODEGRAPH_TYPED_ENRICH=0` disables it and returns the pre-change behavior.

## Consequences

- `call_trace` and `code_graph` now get the same typed edges for Go repos.
- `dead_code` and `code_health` stop false-flagging the func-value alias and homonymous-method shapes.
- The warm path is not free: the first `go/types` call against a cold repo can miss the 10s budget and return a `basic` graph, then upgrade in background. Operators watch `gocode_callgraph_gotypes_fallback_total` and `gocode_agegraph_typed_enrich_total`.
- Fitness functions are in place:
  - `internal/goanalysis/cached_loader_ctx_budget_test.go` proves cache reuse and per-caller deadline isolation.
  - `internal/goanalysis/resolver_hardred_test.go` (var-func-binding and method-value aliases) and `internal/codegraph/satisfaction_test.go` (AGE-graph typed enrichment) are permanent regression guards.
  - The `gocode_agegraph_typed_enrich_total{result="applied"}` / `degraded` SLO alert is the live operational guard. It lives in `deploy/deploy-config/config/prometheus/` and will be added in a separate deploy-config PR.

## Alternatives considered and rejected

- **CHA/RTA** — rejected (decision 2): over-approximation for the targeted bug classes; would require building a whole-program pointer scaffolding we do not need.
- **VTA now** — rejected (decision 2): more power than the current bug classes require; deferred until a recurrence trigger demonstrates a concrete gap.
- **Separate typed builders in `callgraph` and `codegraph`** — rejected (decision 1): would duplicate the warm/cold/degrade logic and allow one path to silently miss a fix.
- **SCIP for Go** — rejected as primary: SCIP is the fallback for non-Go languages or when `go/types` fails (`repo.go:179-187`), not the source of truth for Go call edges.
