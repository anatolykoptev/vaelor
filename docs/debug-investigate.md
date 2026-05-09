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
| `service` | string | Required. Service name as seen by Jaeger (`go-code`, `oxpulse-chat`, ...). |
| `start_unix` | int64 | Window start, unix seconds. `0` → now-15m. |
| `end_unix` | int64 | Window end, unix seconds. `0` → now. |
| `hint` | string | Optional free-text hint about the suspected behaviour. |
| `hint_kind` | string | Optional structured hint kind (see "Hint kinds" below). Empty = auto-detect. |
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

## Auto-discovered failure metrics

Phase 4 no longer relies on the hardcoded `http_requests_total` metric. Instead, it queries Prometheus for all metric names and retains those matching:

```
(?i)(_failed_total|_failures?_total|_errors?_total|_dropped_total|_failure(_|$)|_outcome($|_total))
```

Any conventionally-named counter — `signaling_call_outcome_total`, `ws_handshake_failed_total`, `sfu_chat_relay_dropped_total`, etc. — is picked up automatically. No code change is needed to make a new metric appear; just follow the naming convention.

**Legacy fallback:** when no matching metric is found (e.g. an older service that exports only `http_requests_total`), Phase 4 falls back to the `http_requests_total` path it used before.

## Hint kinds

The optional `hint_kind` field lets callers communicate a known failure class, sharpening the investigation report. The routing logic is captured but **not yet active** (lands in Phase β/γ).

| Value | Meaning |
|---|---|
| _(empty)_ | Auto-detect — full pipeline runs (default). |
| `frontend_reactive_cycle` | Suspected infinite Svelte reactive cycle / client-side error storm. |
| `panic_at_handler` | Go server panic in an HTTP/gRPC handler. |
| `metric_spike_unknown_source` | Anomalous counter spike with no matching trace. |
| `latency_spike` | P99 or mean latency jump without a clear error signal. |

> **Note:** `hint_kind` is validated on input and surfaced in output XML (`hint_kind` attribute on `<investigation>`). It does not yet alter which data sources are queried — that routing is the Phase β/γ work item.

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
    <metric_spikes>
      <spike metric="signaling_call_outcome_total" labels="{service=&quot;oxpulse-chat&quot;}" ratio="4.70" score="0.800"/>
      <spike metric="ws_handshake_failed_total"    labels="{service=&quot;oxpulse-chat&quot;}" ratio="2.10" score="0.600"/>
    </metric_spikes>
    <diagnostics>{"metrics_queried":4,"traces_fetched":20,"spans_analyzed":143,"symbols_touched":3}</diagnostics>
  </investigation>
</response>
```

### `<metric_spikes>`

When auto-discovery finds anomalous failure counters, the result includes a `<metric_spikes>` block listing the top spikes (up to 5), sorted by anomaly score descending.

| Attribute | Meaning |
|---|---|
| `metric` | Full Prometheus metric name. |
| `labels` | Label selector used when querying (always includes `service=`). |
| `ratio` | `window_max / baseline_max` (1h earlier window of same duration). |
| `score` | Bucketed anomaly score 0..1; ≥ 0.8 = critical, ≥ 0.6 = elevated, ≥ 0.4 = mild. |

The `anomaly_score` on each `<hypothesis>` reflects the score of the top spike (or 0.5 default when no spikes are found).

When `hint_kind` is set, the `<investigation>` element includes it as an attribute:

```xml
<investigation service="oxpulse-chat" hint_kind="latency_spike" started_at="..." finished_at="...">
```

## Reference architecture

- **Zagalin** (Grafana plugin) — ground-truth metric/service injection in system prompt.
- **Grafana Sift** — analysis result struct + lifecycle.
- **Sentry Autofix** — condensed→expand trace strategy.
- **Datadog Bits AI** — parallel hypothesis ranking.
