# Dead Code: False Positives for Svelte/Vue Imports and Rust Framework Handlers

## Problem

`dead_code` reports false positives in two common scenarios.

### 1. Svelte/Vue imports invisible

Functions exported from `.ts` files and imported by `.svelte` files are marked dead because `.svelte` is not in the language detection map.

```typescript
// src/lib/webrtc.ts — marked dead (high confidence)
export function createCall(config) { ... }
```

```svelte
<!-- src/routes/+page.svelte — never parsed -->
<script>
  import { createCall } from '$lib/webrtc'
</script>
```

**Real impact:** In a SvelteKit project, **14 out of 36 functions (39%)** were incorrectly flagged — all high confidence.

**Root cause:**
- `internal/ingest/ingest.go:229` `DetectLanguage()` — `.svelte`/`.vue` not recognized
- `internal/parser/parser.go:237` `extToLanguage` — missing entries
- `internal/callgraph/repo.go:58-67` — no import edge injection for these files

**Fix options:**
- **A)** Add `.svelte`/`.vue` to `extToLanguage`, create handler that extracts `<script>` block as TS
- **B)** Regex-scan `.svelte`/`.vue` for `import {...} from '...'` and inject synthetic `CallEdge` entries (like `extractHookRoutes` for PHP)

### 2. Rust Axum/Actix route handlers marked dead

Handlers passed as function references to `.route()` aren't detected as called:

```rust
async fn ws_call_route(ws: WebSocketUpgrade) -> impl IntoResponse { ... }  // marked dead
app.route("/ws/call/{room_id}", get(ws_call_route))  // reference, not call
```

**Root cause:**
- `internal/routes/match_rust.go:23-25` — regex captures path+method but **not handler name**
- `internal/deadcode/helpers.go:45-54` — `httpHandlerPatterns` covers Go/Python/Node but not Rust (`impl IntoResponse`, `WebSocketUpgrade`)
- No `InjectRouteHandlerEdges()` equivalent for Rust in `callgraph/repo.go`

**Fix:**
1. Enhance regex in `match_rust.go` to capture handler function names
2. Add Rust handler signature patterns to `deadcode/helpers.go`:
   ```go
   "impl IntoResponse", "WebSocketUpgrade", "axum::extract", "actix_web"
   ```
3. Inject route handler edges in `BuildFromRepo()` like PHP hooks

## Affected Files

| File | What to change |
|------|---------------|
| `internal/ingest/ingest.go:229` | Add `.svelte`, `.vue` to `DetectLanguage()` |
| `internal/parser/parser.go:237` | Add to `extToLanguage` map |
| `internal/callgraph/repo.go:58-67` | Add `extractSvelteImports()` + inject edges |
| `internal/routes/match_rust.go:23-25` | Capture handler name in regex |
| `internal/deadcode/helpers.go:45-54` | Add Rust handler patterns |
| `internal/callgraph/repo.go:60-64` | Add `InjectRouteHandlerEdges()` |

## Impact

| Issue | False Positive Rate | Fix Complexity |
|-------|-------------------|----------------|
| Svelte/Vue imports | 39% in SvelteKit project | Medium |
| Rust route handlers | ~4 functions per Axum project | Low |

Both patterns are common in modern full-stack stacks (SvelteKit + Axum, Nuxt + Actix).
