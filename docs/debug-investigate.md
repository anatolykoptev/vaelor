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
      <spike kind="error"     metric="signaling_call_outcome_total" labels="{service=&quot;acme-web&quot;}" ratio="4.70" score="0.800"/>
      <spike kind="invariant" metric="WireWriteMissing"             labels="{service=&quot;acme-sfu&quot;,severity=&quot;critical&quot;}" ratio="0.00" score="1.000"/>
    </metric_spikes>
    <alert_violations>
      <alert_violation alertname="WireWriteMissing" severity="critical" service="acme-sfu" active_at="2026-05-08T10:00:00Z">wire_written stayed at 0 while forward_decisions advanced</alert_violation>
    </alert_violations>
    <diagnostics>{"metrics_queried":4,"traces_fetched":20,"spans_analyzed":143,"symbols_touched":3,"alerts_queried":1}</diagnostics>
  </investigation>
</response>
```

### `<metric_spikes>`

When auto-discovery finds anomalous failure counters, the result includes a `<metric_spikes>` block listing the top spikes (up to 5), sorted by anomaly score descending.

| Attribute | Meaning |
|---|---|
| `kind`   | `error` (failure-counter spike), `latency`, `saturation`, or `invariant` (firing alert). |
| `metric` | Full Prometheus metric name (or alert name for `kind=invariant`). |
| `labels` | Label selector used when querying (always includes `service=`). |
| `ratio` | `window_max / baseline_max` (1h earlier window of same duration). `0` for invariant spikes. |
| `score` | Bucketed anomaly score 0..1; ≥ 0.8 = critical, ≥ 0.6 = elevated, ≥ 0.4 = mild. |

The `anomaly_score` on each `<hypothesis>` reflects the score of the top spike (or 0.5 default when no spikes are found).

### `<alert_violations>`

When Prometheus `/api/v1/alerts` returns firing alerts for the investigated service, the result includes an `<alert_violations>` block. These capture **constant-state invariant violations** — cases where a metric ratio has been wrong continuously (no delta) and thus escapes Phase 4 spike detection.

**How it works:** operators define invariant rules as standard Prometheus alerting rules. The tool consumes them without any additional configuration — if `PROMETHEUS_URL` is set, alerts are queried automatically.

**Service matching:** an alert is included when `labels.service == input.service` OR `labels.job == input.service`.

| Attribute | Meaning |
|---|---|
| `alertname` | Prometheus alert name (from `labels.alertname`). |
| `severity`  | Severity label value (`critical`, `warning`, etc.). |
| `service`   | Matched service name. |
| `active_at` | ISO-8601 timestamp when the alert began firing. |
| (text body) | Alert `annotations.summary`. |

**Defining invariant rules (operator guide):**

```yaml
# Example: wire_written should equal forward_decisions{action="forwarded"}
- alert: WireWriteMissing
  expr: |
    sum(rate(wire_written_total{service="acme-sfu"}[5m])) /
    sum(rate(forward_decisions_total{service="acme-sfu",action="forwarded"}[5m])) < 0.9
  labels:
    service: acme-sfu
    severity: critical
  annotations:
    summary: wire_written stayed below 90% of forward_decisions
    runbook_url: https://runbooks/wire-write-missing
```

When `hint_kind` is set, the `<investigation>` element includes it as an attribute:

```xml
<investigation service="acme-web" hint_kind="latency_spike" started_at="..." finished_at="...">
```

## Reference architecture

- **Zagalin** (Grafana plugin) — ground-truth metric/service injection in system prompt.
- **Grafana Sift** — analysis result struct + lifecycle.
- **Sentry Autofix** — condensed→expand trace strategy.
- **Datadog Bits AI** — parallel hypothesis ranking.

## Phase 6 — log excerpts

**Phase 6** fetches recent log lines from the [dozor](https://github.com/anatolykoptev/dozor) sidecar and attaches them to the investigation result as `<log_excerpts>`. This surfaces `panic`, `fatal`, and `error` messages that do not leave a Prometheus metric (one-off panics, cold-path errors, startup failures).

### Required environment variable

| Variable | Default | Description |
|---|---|---|
| `DOZOR_URL` | `http://dozor:8765` | Base URL of the dozor API. Set to empty string (`DOZOR_URL=`) to disable Phase 6. |
| `DOZOR_API_TOKEN` | _(empty — no auth)_ | Optional Bearer token sent in `Authorization` header. |

### What gets fetched

- Up to **20 lines** from the investigation time window (`start` → `end`).
- Server-side default grep applied when no explicit grep given: `panic|fatal|error` (case-insensitive).
- Lines are returned in ascending timestamp order by the server.

### Output XML structure

```xml
<log_excerpts>
  <line ts="2026-05-08T10:00:00Z" level="ERROR">connection refused to postgres</line>
  <line ts="2026-05-08T10:00:01Z" level="FATAL">panic: nil pointer dereference</line>
</log_excerpts>
```

The block is omitted entirely when:
- `DOZOR_URL` is empty.
- Dozor returns an error (error is added to `diagnostics.warnings` instead).
- No lines match the default filter in the window.

### Compose environment note

After the go-code service is (re)deployed, add to `compose/search.yml`:

```yaml
environment:
  - DOZOR_URL=${DOZOR_URL:-http://dozor:8765}
  - DOZOR_API_TOKEN=${DOZOR_API_TOKEN:-}
```
