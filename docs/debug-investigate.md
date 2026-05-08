# debug_investigate MCP tool

Correlate **Prometheus metrics**, **Jaeger failed traces**, and **go-code symbol intelligence** to suggest the likely buggy `file:function` for a given service+window.

## Configuration

Two env vars are required (tool is silently disabled if either is empty):

```
PROMETHEUS_URL=http://prometheus:9090
JAEGER_URL=http://jaeger:16686
```

## Inputs

| Field | Type | Description |
|-------|------|-------------|
| `service` | string | Required. Service name as seen by Jaeger (`go-code`, `acme-web`, ...). |
| `start_unix` | int64 | Window start, unix seconds. `0` → now-15m. |
| `end_unix` | int64 | Window end, unix seconds. `0` → now. |
| `hint` | string | Optional free-text hint about the suspected behaviour. |
| `repo` | string | Repo path for symbol lookup. Defaults to the go-code repo when omitted. |

## Lifecycle

The tool is **long-running** (5-minute budget). First call returns `"investigation started"` and kicks off a background goroutine. Re-run the same call (same `service` + `start_unix` + `end_unix`) every 30s to poll. Final response is XML.

```
1. fresh call          → "Investigation started ..."
2. while running       → "Investigation in progress ..."
3. complete            → <response tool="debug_investigate"> ... </response>
4. failed              → "Previous investigation failed: <reason>"
```

## Phases

| Phase | What | Source |
|-------|------|--------|
| 1 | List Jaeger services | `jaegerclient.ListServices` |
| 2 | Fetch failed traces (`error=true` tag) | `jaegerclient.FindTraces` |
| 3 | Span operation → Go function via `OperationToFuncName` + `compound.FindSymbol` | `internal/investigate/correlate.go` |
| 4 | Prometheus baseline anomaly score (window vs 1h earlier, ratio bucket) | `promclient.QueryRange` |
| 5 | LLM correlate summary with ground-truth context (metric names + service names + ops seen) | `deps.LLM.Complete` |

## Output

```xml
<response tool="debug_investigate">
  <investigation service="go-code" started_at="..." finished_at="...">
    <summary>One-paragraph LLM summary.</summary>
    <hypothesis rank="1" confidence="high">
      <subject>HandleMessage in $REPO_ROOT/cmd/server.go</subject>
      <location file="$REPO_ROOT/cmd/server.go" line="42"/>
      <signals span_count="17" anomaly_score="0.800"/>
      <evidence>operation=/api.Service/HandleMessage; spans=17</evidence>
      <next_check>understand symbol="HandleMessage" repo="$REPO_ROOT"</next_check>
    </hypothesis>
    ...
    <diagnostics>{"metrics_queried":2,"traces_fetched":20,"spans_analyzed":143,"symbols_touched":3}</diagnostics>
  </investigation>
</response>
```

## Reference architecture

- **Zagalin** (Grafana plugin) — ground-truth metric/service injection in system prompt.
- **Grafana Sift** — analysis result struct + lifecycle.
- **Sentry Autofix** — condensed→expand trace strategy.
- **Datadog Bits AI** — parallel hypothesis ranking.
