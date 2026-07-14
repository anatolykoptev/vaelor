# go-code Followups

One-line issue pointers for the backlog. Resolved items are marked as such.

## Open

- #344 — FU-3.1: make `ubiquityPct` configurable
- #345 — FU-2.3: G² scale-invariance and multiple comparison correction over `maxCrossPairs`
- #346 — FU-B.2: T2 AGE graph-confirm enrichment for federated co-change
- #347 — FU-B.3: T3 embeddings soft cosine fallback + commit-message ticket-ID linkage
- #348 — FU-B.4: promote `RouteKey` into `internal/routes` and collapse duplicates
- #349 — FU-C.1: true zero-IO shared-byte cache for route/symbol verification
- #350 — FU-C.2: refine generic-infra suffix list precision
- #351 — FU-C.3: per-symbol strength weighting
- #352 — FU-CG.7: verify route handler resolution for Go client, C#, Java, Python, Ruby
- #353 — Ops: reindex AGE graphs for oxpulse-chat/partner-edge/admin/sfu-kit after route-edge fixes
- #354 — Add skip-list for non-buildable repos in eager warm
- #355 — BUG-SR-1: `suggest_reviewers` returns co-change=0 for paths with obvious coupling

## Resolved

- 2026-05-12 CPU cap added (150%) — operational note; no issue.
- Stale repo paths: log-level demoted to `DEBUG`; skip-list tracked in #354.
- BUG-FH-1 — resolved: `isHealthEligible` source allow-list and dir deny-list in `cmd/go-code/tool_file_health.go`.
- BUG-FH-2 — resolved: `BatchPriorDefect` batches `git log` per root.
- BUG-FH-3 — resolved: tool description now states "returns up to 5 distinct authors".
- BUG-FH-2b — resolved: `BatchInitialCreationLines` batches `git log --diff-filter=A` per root.
- FU-2.2 — resolved: pass asOf time.Time to CrossRepoCoChange / collectTouches (#343).
- FU-2.1 — done: G² + support tier ranking.
- FU-1.1 — done: #337.
- Phase 3a.3 Wilson-LB + ubiquity filter — done.
- FU-B.1 — done: T1 shared-symbol verifier.
- FU-C generic-infra suffix floor — done.
- FU-CG.1 — done: enclosing-function resolver fallback for empty `Route.Handler`.
- FU-CG.2 — done: junk route filter and TS receiver allow-list.
- FU-CG.3 — done: `\x00` delimiter for colon-safe route keys.
- FU-CG.4 — done: `side` property on `Route` vertices.
- FU-CG.5 — done: axum matcher in `match_rust.go`.
- FU-CG.6 — done: `route_handler_unresolved_total` and graph-build counters.
- FU-CG.8 — done: `side` added to `HandlesRoute`/`FetchedBy` Cypher queries.
- FU-CG.9 — done: route edge `unmatched` counter (#337).
- FU-P5.1 — resolved: investigated, premise doesn't hold; no code change.
- FU-P5.2 — done: shared `parseTree` helper (#275).
- Phase 2b smoke — done.
