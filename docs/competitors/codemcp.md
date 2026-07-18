# SimplyLiz/CodeMCP — Go-native Code Intelligence (Closest Competitor)

- **Repo**: [SimplyLiz/CodeMCP](https://github.com/SimplyLiz/CodeMCP) | ~62 stars | Go
- **CLI**: `ckb` (CodeKnowledgeBase)
- **Scale**: 683 files, 80 internal packages
- **Analyzed**: 2026-02-28 via `repo_analyze` deep mode

## What It Is

The most architecturally complex code MCP server analyzed. Go-native, SCIP-first with tree-sitter fallback.
76+ tools organized into presets with progressive disclosure.

## Architecture

### 3-Tier Analysis System (`internal/tier/`)

| Tier | Backend | Description |
|------|---------|-------------|
| Basic ("Fast") | tree-sitter only | Syntax-level analysis |
| Enhanced ("Standard") | + SCIP index | Semantic code intelligence |
| Full ("Full") | + SCIP + telemetry | Richest insights with usage data |

- Each tool has `MinimumTier` + `Fallback` flag (degrades gracefully)
- `tier.Detector` auto-detects available backends
- `tier.GetToolsForTier()` / `tier.GetUnavailableTools()` — dynamic filtering
- User can `SetRequestedMode("fast"|"standard"|"full")`, capped by available backends

### SCIP Backend (`internal/backends/scip/`)

`SCIPAdapter` wraps `sourcegraph/scip-bindings-go-scip`:
- `GetSymbol(ctx, symbolId)` → name, kind, container, module, visibility, location
- `FindReferences(ctx, symbolId, opts)` → all reference sites, filterable by tests
- `BuildCallGraph(symbolId, opts)` → callers/callees with MaxDepth + MaxNodes limits
- `AllSymbols()` → full index dump

Falls back to tree-sitter when SCIP unavailable.

### Identity System (`internal/identity/`)

- **Stable SCIP IDs**: canonical symbol identifiers that survive minor refactors
- **SymbolFingerprint**: content-based hash fallback (name+kind+signature+container+location)
- **Alias chains**: track renames across versions
- Completeness scoring: SCIP identity = 1.0, fingerprint-only = 0.5

### Fusion Ranking (`internal/query/ranking.go`)

`FusionRanker` combines 5 normalized signals:

```
fusionScore = w.FTS * fts + w.PPR * ppr + w.Hotspot * hotspot + w.Recency * recency + w.Exact * exact
```

- **FTS**: full-text search score
- **PPR**: Personalized PageRank from SCIP-built code graph
  - Seeds = top 10 FTS results
  - Seed expansion: `expandSeedsWithMethods` (class name → add all methods)
  - `graph.BuildFromSCIP()` with `DefaultEdgeWeights()`
- **Hotspot**: churn × complexity
- **Recency**: recent changes weighted higher
- **Exact**: exact name match bonus
- Normalization: all signals scaled to [0,1] before combination
- Alternative: `RerankWithPPR` = `0.6 * positionScore + 0.4 * pprScore`

### Impact Analysis (`internal/impact/`)

1. Resolve symbol via SCIP stable ID
2. `FindReferences()` → direct callers, classified (DirectCaller, ImplementsInterface, etc.)
3. `BuildCallGraph()` → transitive callers with depth limit
4. Confidence degrades with distance
5. `ClassifyBlastRadius`:
   - Low: ≤2 modules / ≤5 callers
   - Medium: ≤5 modules / ≤20 callers
   - High: above thresholds
6. Risk scoring: `visibility × impact_count`
7. Optional: telemetry enhancement (observed usage boosts/lowers risk)
8. Truncation: `budget.MaxImpactItems`, `budget.MaxModules`

### Progressive Tool Disclosure

Presets: core, review, refactor, federation, docs, ops, full.
- Default starts with "core" preset
- `expandToolset` MCP tool switches preset
- Sends `notifications/tools/list_changed` to client
- `GetFilteredTools()` combines preset × tier

### Compound Tools (`internal/query/compound.go`)

- `explore` — area overview (file_parse + symbol_search + dep_graph)
- `understand` — symbol deep-dive (call_trace + code_graph + complexity)
  - Handles `UnderstandAmbiguity`: lists top matches with disambiguation hints
- `prepareChange` — pre-change assessment (impact + dead_code)

### Additional Packages (30+)

audit, auth, breaking, complexity, compression, coupling, cycles, daemon, deadcode, decisions, diff, docs, envelope, explain, export, extract, federation, hotspots, incremental, index, jobs, modules, ownership, scheduler, secrets, streaming, suggest, telemetry, testgap, watcher, webhooks.

## What's Genuinely Good

1. **Fusion ranking** (5 signals, not 1-2) → Phase 7.5 should adopt
2. **Graceful tier degradation** (tool still works, just less precise) → Phase 11 should adopt
3. **Blast radius with concrete thresholds** → Phase 9.2 should adopt
4. **Seed expansion for PPR** (class → add methods) → Phase 7.5
5. **UnderstandAmbiguity** pattern → Phase 11.3 compound tools

## Anti-Patterns to Avoid

1. **683 files / 80 packages** — overengineered for a code analysis tool
2. **76+ tools** — CodeCompass research: agents don't use 80% of tools. Vaelor's 8 is optimal.
3. **Telemetry as tier requirement** — premature for most use cases
4. **Separate packages for simple queries** — `breaking`, `coupling`, `cycles`, `responsibilities` are just graph queries, not packages

## Comparative Position

| Feature | Vaelor | CodeMCP |
|---------|---------|---------|
| Parsing | tree-sitter (9 langs) | SCIP + tree-sitter |
| Graph DB | Apache AGE (persistent) | In-memory |
| NL→Cypher | Yes (14 templates + freeform) | No |
| Code compare | Yes (structural) | No |
| Call graph | BFS (tree-sitter) | SCIP-based (precise) |
| Impact/blast | Planned (Phase 9) | Yes |
| Dead code | Planned (Phase 9) | Yes |
| Tools | 8 (focused) | 76+ (bloated) |
| Cross-language | Yes (polyglot + routes) | No |
| Repo search | Yes | No |
