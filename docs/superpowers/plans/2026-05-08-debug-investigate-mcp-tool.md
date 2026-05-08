# debug_investigate MCP Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `debug_investigate` MCP tool to go-code that combines Prometheus metrics, Jaeger traces, and existing go-code symbol intelligence to suggest the likely buggy file:function for a given service+time-range.

**Architecture:** Three new internal packages (`promclient`, `jaegerclient`, `investigate`) plus one tool handler. Investigation lifecycle follows the existing `tool_code_health.go` polling pattern (sync.Map dedup, background goroutine, "computing" response on retry). LLM correlation uses the existing `deps.LLM.Complete` from `go-kit/llm` — no new provider abstraction. Reference architecture: Zagalin (ground-truth system prompt), Grafana Sift (analysis result struct), Sentry Autofix (condensed-then-expand trace strategy).

**Tech Stack:** Go 1.26+, pgx v5, Apache AGE, `go-mcpserver`, `go-kit/llm`, `go-kit/cache`, `slog`, `prometheus/client_golang`, Prometheus HTTP API v1, Jaeger HTTP API v3 (port 16686).

**Key audit findings (reuse-vs-new):**
- REUSE: HTTP client pattern (`internal/websearch/client.go`), LLM client (`deps.LLM.Complete`), tool registration (`tool_understand.go` template), polling lifecycle (`tool_code_health.go`), symbol intelligence (`compound.Understand`, `callgraph.BuildFromRepo`, `compound.FindSymbol`), `resolveRoot`, `errResult`/`textResult`, per-tool metrics (auto via `mcpmw.Middleware`).
- NEW: Prometheus query_range parser (~100 LOC), Jaeger /api/traces parser (~180 LOC), span→symbol correlation (~70 LOC), investigation result+ranking (~80 LOC), tool handler+lifecycle (~30 LOC), LLM correlate prompt (~25 lines), config additions (~5 lines).
- DEFER (followup PR): `internal/httputil` extraction. After this tool ships, three callsites (`promclient`, `jaegerclient`, existing `freshness.registryGet`) justify a shared `httputil.GetJSON(ctx, client, url, dest)` extraction.

---

## File Structure

```
internal/promclient/
├── client.go              # Client struct + NewClient + GetJSON wrapper
├── query_range.go         # QueryRange method + Matrix/Vector response types
└── client_test.go         # httptest.Server + table-driven response tests

internal/jaegerclient/
├── client.go              # Client struct + NewClient
├── services.go            # ListServices
├── traces.go              # FindTraces + GetTrace + Span/Trace types
└── client_test.go         # httptest.Server tests

internal/investigate/
├── correlate.go           # spanToSymbol + operationToFuncName
├── result.go              # Investigation, Hypothesis, ConfidenceLevel types + ranking
├── lifecycle.go           # InvestigationStore (sync.Map dedup) + Status enum
├── prompt.go              # systemPromptDebugInvestigate (LLM ground-truth)
├── correlate_test.go
├── result_test.go
└── lifecycle_test.go

cmd/go-code/
├── tool_debug_investigate.go   # registerDebugInvestigate + handler
├── config.go                    # +PrometheusURL, +JaegerURL
├── register.go                  # wire registerDebugInvestigate(deps, ...)
└── main.go                      # +"debug_investigate": 5*time.Minute in ToolTimeouts

docs/
└── debug-investigate.md         # User-facing doc: env vars + example call

.env.example
└── (root)                       # +PROMETHEUS_URL, +JAEGER_URL
```

**Boundary rationale:**
- `promclient` and `jaegerclient` are pure HTTP clients with no go-code coupling. Deliberate split — easier to unit-test, easier to extract into `go-kit/` later if reused.
- `investigate` package owns business logic: how a span maps to a symbol, how hypotheses rank, lifecycle. No HTTP, no MCP — pure types + stateless funcs (except `InvestigationStore`).
- `tool_debug_investigate.go` is the only file that knows about MCP. Wires `promclient` + `jaegerclient` + `investigate` + `compound.Understand` + `deps.LLM.Complete`.

---

## Task 1: Bootstrap promclient package — HTTP client skeleton

**Files:**
- Create: `internal/promclient/client.go`
- Test: `internal/promclient/client_test.go`

- [ ] **Step 1.1: Write the failing test**

```go
// internal/promclient/client_test.go
package promclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_DefaultsTimeout(t *testing.T) {
	c := NewClient("http://localhost:9090", 0)
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default 30s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestNewClient_RespectsCustomTimeout(t *testing.T) {
	c := NewClient("http://localhost:9090", 60*time.Second)
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("expected 60s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestClient_BaseURL_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:9090/", 0)
	if c.baseURL != "http://localhost:9090" {
		t.Errorf("expected trimmed baseURL, got %q", c.baseURL)
	}
}

func TestClient_GetJSON_DecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	var dest struct{ Status string `json:"status"` }
	if err := c.getJSON(t.Context(), "/test", &dest); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if dest.Status != "success" {
		t.Errorf("got %q, want %q", dest.Status, "success")
	}
}
```

- [ ] **Step 1.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/promclient/...
```

Expected: FAIL with "package promclient does not exist" or similar.

- [ ] **Step 1.3: Write minimal implementation**

```go
// internal/promclient/client.go
package promclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client queries a Prometheus HTTP API endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client. Pass timeout=0 for the default (30s).
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// getJSON performs GET <baseURL><path> and decodes the JSON response into dest.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("prometheus HTTP %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
```

- [ ] **Step 1.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/promclient/...
```

Expected: PASS — 4 tests OK.

- [ ] **Step 1.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/promclient/
git commit -m "feat(promclient): bootstrap HTTP client skeleton

Pure-HTTP wrapper for Prometheus query API. Timeout-configurable
(default 30s), trims trailing slash on baseURL, getJSON helper
decodes JSON response or returns wrapped error.

Foundation for QueryRange (next task)."
```

---

## Task 2: promclient.QueryRange — fetch metrics over time range

**Files:**
- Create: `internal/promclient/query_range.go`
- Modify: `internal/promclient/client_test.go`

- [ ] **Step 2.1: Write the failing test**

Append to `internal/promclient/client_test.go`:

```go
func TestQueryRange_ParsesMatrixResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "query=up") {
			t.Errorf("missing query=up in %q", r.URL.RawQuery)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{"metric": {"__name__":"up","instance":"a"},"values":[[1700000000,"1"],[1700000060,"0"]]}
				]
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	res, err := c.QueryRange(t.Context(), "up", time.Unix(1700000000, 0), time.Unix(1700000060, 0), 60*time.Second)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(res.Data.Result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.Data.Result))
	}
	if got := res.Data.Result[0].Metric["instance"]; got != "a" {
		t.Errorf("instance label: got %q, want %q", got, "a")
	}
	if len(res.Data.Result[0].Values) != 2 {
		t.Errorf("expected 2 sample points, got %d", len(res.Data.Result[0].Values))
	}
}

func TestQueryRange_EncodesParamsCorrectly(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, _ = c.QueryRange(t.Context(), `rate(http_requests_total{code="500"}[5m])`,
		time.Unix(1700000000, 0), time.Unix(1700000300, 0), 30*time.Second)

	for _, want := range []string{"query=rate", "start=1700000000", "end=1700000300", "step=30"} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("missing %q in query string %q", want, capturedQuery)
		}
	}
}
```

Add `"strings"` to imports if not already present.

- [ ] **Step 2.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/promclient/...
```

Expected: FAIL — `c.QueryRange` undefined.

- [ ] **Step 2.3: Write minimal implementation**

```go
// internal/promclient/query_range.go
package promclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// QueryRangeResponse mirrors the Prometheus /api/v1/query_range JSON shape.
type QueryRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []SeriesResult `json:"result"`
	} `json:"data"`
}

// SeriesResult is one labelled time series with [(unixSec, valueStr), ...] samples.
type SeriesResult struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"`
}

// QueryRange runs a PromQL query over [start, end] with the given step.
// query is the raw PromQL expression (it is URL-encoded by this method).
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryRangeResponse, error) {
	if step <= 0 {
		return nil, fmt.Errorf("step must be > 0")
	}
	if !end.After(start) {
		return nil, fmt.Errorf("end (%v) must be after start (%v)", end, start)
	}

	v := url.Values{}
	v.Set("query", query)
	v.Set("start", strconv.FormatInt(start.Unix(), 10))
	v.Set("end", strconv.FormatInt(end.Unix(), 10))
	v.Set("step", strconv.FormatInt(int64(step.Seconds()), 10))

	path := "/api/v1/query_range?" + v.Encode()
	var resp QueryRangeResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query_range: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", resp.Status)
	}
	return &resp, nil
}
```

- [ ] **Step 2.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/promclient/...
```

Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/promclient/
git commit -m "feat(promclient): QueryRange — fetch Prometheus matrix over time

QueryRange(ctx, promql, start, end, step) → matrix response.
Encodes query/start/end/step into URL params, validates step > 0
and end > start. Returns wrapped Prometheus matrix response."
```

---

## Task 3: Bootstrap jaegerclient package — HTTP client + ListServices

**Files:**
- Create: `internal/jaegerclient/client.go`
- Create: `internal/jaegerclient/services.go`
- Test: `internal/jaegerclient/client_test.go`

- [ ] **Step 3.1: Write the failing test**

```go
// internal/jaegerclient/client_test.go
package jaegerclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient_DefaultsTimeout(t *testing.T) {
	c := NewClient("http://localhost:16686", 0)
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default 30s, got %v", c.httpClient.Timeout)
	}
}

func TestListServices_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			t.Errorf("expected /api/services, got %q", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":["go-code","acme-web","memdb-go"],"total":3}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	got, err := c.ListServices(t.Context())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(got) != 3 || got[0] != "go-code" {
		t.Errorf("unexpected services: %v", got)
	}
}

func TestListServices_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":null,"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	got, err := c.ListServices(t.Context())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestClient_BaseURL_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://localhost:16686/", 0)
	if !strings.HasSuffix(c.baseURL, "16686") {
		t.Errorf("expected trimmed, got %q", c.baseURL)
	}
}
```

- [ ] **Step 3.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/jaegerclient/...
```

Expected: FAIL — package not found.

- [ ] **Step 3.3: Write minimal implementation — client**

```go
// internal/jaegerclient/client.go
package jaegerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client queries a Jaeger query API endpoint (typically port 16686).
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Jaeger HTTP API client. timeout=0 → default 30s.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// getJSON GETs the path and decodes JSON into dest.
func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("jaeger HTTP %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
```

- [ ] **Step 3.4: Write services.go**

```go
// internal/jaegerclient/services.go
package jaegerclient

import (
	"context"
	"fmt"
)

// servicesResponse wraps Jaeger /api/services.
type servicesResponse struct {
	Data  []string `json:"data"`
	Total int      `json:"total"`
}

// ListServices returns all services that have submitted traces to Jaeger.
func (c *Client) ListServices(ctx context.Context) ([]string, error) {
	var resp servicesResponse
	if err := c.getJSON(ctx, "/api/services", &resp); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	if resp.Data == nil {
		return []string{}, nil
	}
	return resp.Data, nil
}
```

- [ ] **Step 3.5: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/jaegerclient/...
```

Expected: PASS.

- [ ] **Step 3.6: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/jaegerclient/
git commit -m "feat(jaegerclient): bootstrap HTTP client + ListServices

Pure-HTTP wrapper for Jaeger query API. ListServices returns the
list of services Jaeger has seen via /api/services. Foundation for
FindTraces/GetTrace (next task)."
```

---

## Task 4: jaegerclient.FindTraces + GetTrace — fetch failed traces and span trees

**Files:**
- Create: `internal/jaegerclient/traces.go`
- Modify: `internal/jaegerclient/client_test.go`

- [ ] **Step 4.1: Write the failing test**

Append to `internal/jaegerclient/client_test.go`:

```go
func TestFindTraces_BuildsCorrectQuery(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.FindTraces(t.Context(), FindTracesParams{
		Service:    "go-code",
		Tags:       map[string]string{"error": "true"},
		StartTime:  time.Unix(1700000000, 0),
		EndTime:    time.Unix(1700001000, 0),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("FindTraces: %v", err)
	}
	for _, want := range []string{"service=go-code", "limit=10", "start=1700000000000000", "end=1700001000000000"} {
		if !strings.Contains(captured, want) {
			t.Errorf("missing %q in query %q", want, captured)
		}
	}
}

func TestFindTraces_DecodesSpansAndOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"traceID": "abc123",
					"spans": [
						{"spanID":"s1","operationName":"/api.Service/Method","duration":50000,
						 "tags":[{"key":"error","type":"bool","value":true}]}
					],
					"processes": {"p1": {"serviceName": "go-code"}}
				}
			],
			"total": 1
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	traces, err := c.FindTraces(t.Context(), FindTracesParams{Service: "go-code", Limit: 1})
	if err != nil {
		t.Fatalf("FindTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].TraceID != "abc123" {
		t.Errorf("traceID: got %q", traces[0].TraceID)
	}
	if len(traces[0].Spans) != 1 || traces[0].Spans[0].OperationName != "/api.Service/Method" {
		t.Errorf("operation: got %+v", traces[0].Spans)
	}
}

func TestGetTrace_FetchesByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces/abc123" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"traceID":"abc123","spans":[]}],"total":1}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	tr, err := c.GetTrace(t.Context(), "abc123")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if tr.TraceID != "abc123" {
		t.Errorf("traceID: got %q", tr.TraceID)
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.GetTrace(t.Context(), "nonexistent")
	if err == nil {
		t.Error("expected error for empty data, got nil")
	}
}
```

- [ ] **Step 4.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/jaegerclient/...
```

Expected: FAIL — `FindTraces`, `GetTrace`, `FindTracesParams`, `Trace`, `Span` undefined.

- [ ] **Step 4.3: Write traces.go**

```go
// internal/jaegerclient/traces.go
package jaegerclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Span is one operation in a Jaeger trace.
type Span struct {
	SpanID        string    `json:"spanID"`
	OperationName string    `json:"operationName"`
	Duration      int64     `json:"duration"` // microseconds
	Tags          []SpanTag `json:"tags"`
}

// SpanTag is a key=value attribute on a span.
type SpanTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Trace is one Jaeger trace tree.
type Trace struct {
	TraceID   string                    `json:"traceID"`
	Spans     []Span                    `json:"spans"`
	Processes map[string]map[string]any `json:"processes,omitempty"`
}

// tracesResponse wraps Jaeger /api/traces.
type tracesResponse struct {
	Data  []Trace `json:"data"`
	Total int     `json:"total"`
}

// FindTracesParams configures the trace query.
type FindTracesParams struct {
	Service   string            // required
	Operation string            // optional, narrows to one operation name
	Tags      map[string]string // e.g. {"error":"true"}
	StartTime time.Time         // inclusive lower bound
	EndTime   time.Time         // inclusive upper bound
	Limit     int               // max traces (Jaeger default 20, hard cap typical 1500)
}

// FindTraces queries Jaeger for traces matching the filter.
// Tags are encoded as JSON via the "tags" param. Times use microsecond precision.
func (c *Client) FindTraces(ctx context.Context, p FindTracesParams) ([]Trace, error) {
	if p.Service == "" {
		return nil, fmt.Errorf("FindTracesParams.Service is required")
	}

	v := url.Values{}
	v.Set("service", p.Service)
	if p.Operation != "" {
		v.Set("operation", p.Operation)
	}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	if !p.StartTime.IsZero() {
		v.Set("start", strconv.FormatInt(p.StartTime.UnixMicro(), 10))
	}
	if !p.EndTime.IsZero() {
		v.Set("end", strconv.FormatInt(p.EndTime.UnixMicro(), 10))
	}
	if len(p.Tags) > 0 {
		// Jaeger expects tags as repeated key value via a single JSON map param.
		// Format: tags={"error":"true","http.status_code":"500"}
		var sb []byte
		sb = append(sb, '{')
		first := true
		for k, val := range p.Tags {
			if !first {
				sb = append(sb, ',')
			}
			first = false
			sb = append(sb, '"')
			sb = append(sb, k...)
			sb = append(sb, '"', ':', '"')
			sb = append(sb, val...)
			sb = append(sb, '"')
		}
		sb = append(sb, '}')
		v.Set("tags", string(sb))
	}

	path := "/api/traces?" + v.Encode()
	var resp tracesResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("find traces: %w", err)
	}
	return resp.Data, nil
}

// GetTrace fetches a single trace by ID.
func (c *Client) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	if traceID == "" {
		return nil, fmt.Errorf("traceID is required")
	}
	var resp tracesResponse
	if err := c.getJSON(ctx, "/api/traces/"+url.PathEscape(traceID), &resp); err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("trace %q not found", traceID)
	}
	return &resp.Data[0], nil
}
```

- [ ] **Step 4.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/jaegerclient/...
```

Expected: PASS — 6 tests.

- [ ] **Step 4.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/jaegerclient/
git commit -m "feat(jaegerclient): FindTraces + GetTrace

FindTraces(ctx, params) → []Trace filtered by service/operation/tags
over [start, end] microsecond range. GetTrace(ctx, id) → single trace.
Tag encoding builds a tiny JSON map for the 'tags' param matching
Jaeger's expected shape."
```

---

## Task 5: investigate.spanToSymbol — operation-name → symbol mapping

**Files:**
- Create: `internal/investigate/correlate.go`
- Test: `internal/investigate/correlate_test.go`

- [ ] **Step 5.1: Write the failing test**

```go
// internal/investigate/correlate_test.go
package investigate

import (
	"testing"
)

func TestOperationToFuncName_GRPCStyle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api.Service/Method", "Method"},
		{"/grpc.health.v1.Health/Check", "Check"},
		{"/app.example.com.v1.ChatService/SendMessage", "SendMessage"},
	}
	for _, c := range cases {
		if got := OperationToFuncName(c.in); got != c.want {
			t.Errorf("OperationToFuncName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOperationToFuncName_HTTPStyle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"GET /api/v1/users", "users"},
		{"POST /api/v1/messages", "messages"},
		{"PUT /api/v1/posts/:id", "posts"},
		{"GET /", ""}, // trailing-only — empty fallback
	}
	for _, c := range cases {
		if got := OperationToFuncName(c.in); got != c.want {
			t.Errorf("OperationToFuncName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOperationToFuncName_PlainFunc(t *testing.T) {
	if got := OperationToFuncName("ProcessMessage"); got != "ProcessMessage" {
		t.Errorf("got %q", got)
	}
	if got := OperationToFuncName("(*Server).Handle"); got != "Handle" {
		t.Errorf("got %q", got)
	}
}

func TestOperationToFuncName_Empty(t *testing.T) {
	if got := OperationToFuncName(""); got != "" {
		t.Errorf("got %q for empty input, want empty", got)
	}
}
```

- [ ] **Step 5.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: FAIL — `OperationToFuncName` undefined.

- [ ] **Step 5.3: Write minimal implementation**

```go
// internal/investigate/correlate.go
package investigate

import "strings"

// OperationToFuncName extracts a Go-friendly function name from a Jaeger
// span operation name. Handles three shapes:
//
//   - gRPC: "/pkg.Service/Method" → "Method"
//   - HTTP: "GET /api/v1/users" → "users" (last non-empty path segment)
//   - Plain: "ProcessMessage" or "(*Server).Handle" → "ProcessMessage" / "Handle"
//
// Returns empty string if no meaningful identifier can be extracted.
// The output is the symbol name to feed into compound.FindSymbol — best-effort,
// not guaranteed to match an existing function.
func OperationToFuncName(op string) string {
	op = strings.TrimSpace(op)
	if op == "" {
		return ""
	}

	// gRPC shape: starts with "/", contains "/" between path and method.
	if strings.HasPrefix(op, "/") && strings.Count(op, "/") >= 2 {
		idx := strings.LastIndex(op, "/")
		method := op[idx+1:]
		if method != "" {
			return method
		}
	}

	// HTTP shape: starts with HTTP method.
	for _, verb := range []string{"GET ", "POST ", "PUT ", "DELETE ", "PATCH ", "HEAD ", "OPTIONS "} {
		if strings.HasPrefix(op, verb) {
			path := strings.TrimPrefix(op, verb)
			path = strings.TrimSuffix(path, "/")
			path = strings.SplitN(path, "?", 2)[0]
			path = strings.TrimRight(path, "/")
			parts := strings.Split(path, "/")
			// Walk back to the last segment that doesn't start with ':' (param) and isn't empty.
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] != "" && !strings.HasPrefix(parts[i], ":") {
					return parts[i]
				}
			}
			return ""
		}
	}

	// Receiver-method shape: "(*Type).Method" → "Method".
	if idx := strings.LastIndex(op, ")."); idx >= 0 {
		method := op[idx+2:]
		if method != "" {
			return method
		}
	}

	// Plain identifier — return as-is.
	return op
}
```

- [ ] **Step 5.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: PASS — 4 test groups.

- [ ] **Step 5.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/investigate/
git commit -m "feat(investigate): OperationToFuncName — span op → symbol name

Maps Jaeger operation names to Go function identifiers:
- gRPC '/pkg.Service/Method' → 'Method'
- HTTP 'GET /api/v1/users' → 'users'
- Receiver-method '(*Type).Method' → 'Method'
- Plain identifier → unchanged

Best-effort, feeds into compound.FindSymbol."
```

---

## Task 6: investigate.Hypothesis types + ranking

**Files:**
- Create: `internal/investigate/result.go`
- Test: `internal/investigate/result_test.go`

- [ ] **Step 6.1: Write the failing test**

```go
// internal/investigate/result_test.go
package investigate

import (
	"sort"
	"testing"
)

func TestRankHypotheses_OrdersByCompositeScore(t *testing.T) {
	in := []Hypothesis{
		{Subject: "low_count_high_anomaly", SpanCount: 1, AnomalyScore: 0.9},
		{Subject: "high_count_low_anomaly", SpanCount: 100, AnomalyScore: 0.1},
		{Subject: "balanced", SpanCount: 10, AnomalyScore: 0.5},
		{Subject: "no_signal", SpanCount: 0, AnomalyScore: 0.0},
	}
	got := RankHypotheses(in)

	// Composite score is span_count * anomaly_score:
	// low_count_high_anomaly=0.9, high_count_low_anomaly=10, balanced=5, no_signal=0
	// Expected order: high_count_low_anomaly, balanced, low_count_high_anomaly, no_signal
	want := []string{"high_count_low_anomaly", "balanced", "low_count_high_anomaly", "no_signal"}
	for i, h := range got {
		if h.Subject != want[i] {
			t.Errorf("rank[%d]: got %q, want %q", i, h.Subject, want[i])
		}
	}
}

func TestRankHypotheses_StableOnTies(t *testing.T) {
	in := []Hypothesis{
		{Subject: "first", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "second", SpanCount: 5, AnomalyScore: 0.5},
		{Subject: "third", SpanCount: 5, AnomalyScore: 0.5},
	}
	got := RankHypotheses(in)
	if got[0].Subject != "first" || got[1].Subject != "second" || got[2].Subject != "third" {
		t.Errorf("not stable: %v", got)
	}
}

func TestConfidenceFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  ConfidenceLevel
	}{
		{0.0, ConfidenceLow},
		{0.05, ConfidenceLow},
		{0.3, ConfidenceMedium},
		{0.6, ConfidenceMedium},
		{0.8, ConfidenceHigh},
		{1.5, ConfidenceHigh}, // saturates at high
	}
	for _, c := range cases {
		if got := ConfidenceFromScore(c.score); got != c.want {
			t.Errorf("ConfidenceFromScore(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestInvestigationResult_StableSortPreserved(t *testing.T) {
	r := &InvestigationResult{
		Hypotheses: []Hypothesis{
			{Subject: "z", SpanCount: 1, AnomalyScore: 0.1},
			{Subject: "a", SpanCount: 10, AnomalyScore: 0.5},
		},
	}
	sort.SliceStable(r.Hypotheses, compositeLess(r.Hypotheses))
	if r.Hypotheses[0].Subject != "a" {
		t.Errorf("expected 'a' first by score, got %q", r.Hypotheses[0].Subject)
	}
}
```

- [ ] **Step 6.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: FAIL — `Hypothesis`, `RankHypotheses`, `ConfidenceFromScore`, `InvestigationResult`, `compositeLess` undefined.

- [ ] **Step 6.3: Write result.go**

```go
// internal/investigate/result.go
package investigate

import (
	"sort"
	"time"
)

// ConfidenceLevel buckets a continuous score into a human-readable label.
type ConfidenceLevel string

const (
	ConfidenceLow    ConfidenceLevel = "low"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceHigh   ConfidenceLevel = "high"
)

// ConfidenceFromScore maps a [0, ∞) score to a 3-bucket confidence label.
//   score < 0.2  → low
//   0.2 ≤ x < 0.7 → medium
//   x ≥ 0.7      → high
func ConfidenceFromScore(score float64) ConfidenceLevel {
	switch {
	case score < 0.2:
		return ConfidenceLow
	case score < 0.7:
		return ConfidenceMedium
	default:
		return ConfidenceHigh
	}
}

// Hypothesis is one candidate root-cause site.
type Hypothesis struct {
	// Subject is a short human-readable summary ("HandleMessage in chat.go").
	Subject string `json:"subject"`

	// File and Line are the suspected source location, host-side path.
	// Empty if the symbol couldn't be resolved.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`

	// SpanCount is how many failed/anomalous spans pointed at this symbol.
	SpanCount int `json:"span_count"`

	// AnomalyScore is the metric-side anomaly intensity (0..1+).
	AnomalyScore float64 `json:"anomaly_score"`

	// Confidence is the bucketed label derived from SpanCount × AnomalyScore.
	Confidence ConfidenceLevel `json:"confidence"`

	// EvidenceLinks are short strings pointing to traces/queries
	// (e.g. "trace abc123", "PromQL: rate(http_errors{...}[5m])").
	EvidenceLinks []string `json:"evidence_links,omitempty"`

	// NextChecks are concrete follow-ups for the operator/agent to run
	// ("call_trace HandleMessage", "code_search 'TODO' near :42").
	NextChecks []string `json:"next_checks,omitempty"`
}

// InvestigationResult is the final tool output.
type InvestigationResult struct {
	Service     string       `json:"service"`
	Range       TimeRange    `json:"range"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	Hypotheses  []Hypothesis `json:"hypotheses"`
	LLMSummary  string       `json:"llm_summary,omitempty"`
	Diagnostics Diagnostics  `json:"diagnostics"`
}

// TimeRange is the [Start, End] window the investigation covered.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Diagnostics records counters from the investigation run for transparency.
type Diagnostics struct {
	MetricsQueried int      `json:"metrics_queried"`
	TracesFetched  int      `json:"traces_fetched"`
	SpansAnalyzed  int      `json:"spans_analyzed"`
	SymbolsTouched int      `json:"symbols_touched"`
	Warnings       []string `json:"warnings,omitempty"`
}

// compositeLess returns a less-fn that orders by (span_count*anomaly) DESC, stable.
func compositeLess(h []Hypothesis) func(i, j int) bool {
	return func(i, j int) bool {
		si := float64(h[i].SpanCount) * h[i].AnomalyScore
		sj := float64(h[j].SpanCount) * h[j].AnomalyScore
		return si > sj
	}
}

// RankHypotheses returns a copy of h sorted by composite score descending.
// Stable — equal scores preserve input order. Confidence label is recomputed
// from the composite score.
func RankHypotheses(h []Hypothesis) []Hypothesis {
	out := make([]Hypothesis, len(h))
	copy(out, h)
	sort.SliceStable(out, compositeLess(out))
	for i := range out {
		out[i].Confidence = ConfidenceFromScore(float64(out[i].SpanCount) * out[i].AnomalyScore / 10.0)
	}
	return out
}
```

- [ ] **Step 6.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/investigate/
git commit -m "feat(investigate): Hypothesis + InvestigationResult + ranking

Hypothesis: candidate root-cause site (subject, file:line, span_count,
anomaly_score, confidence bucket, evidence_links, next_checks).
InvestigationResult: full tool output (service, range, hypotheses,
LLM summary, diagnostics).
RankHypotheses sorts by composite score (span_count * anomaly) stable;
ConfidenceFromScore buckets into low/medium/high (<0.2 / <0.7 / ≥0.7
of normalised score)."
```

---

## Task 7: investigate.InvestigationStore — sync.Map dedup lifecycle

**Files:**
- Create: `internal/investigate/lifecycle.go`
- Test: `internal/investigate/lifecycle_test.go`

- [ ] **Step 7.1: Write the failing test**

```go
// internal/investigate/lifecycle_test.go
package investigate

import (
	"sync"
	"testing"
	"time"
)

func TestInvestigationStore_StartReturnsRunning(t *testing.T) {
	s := NewInvestigationStore()
	st, fresh := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !fresh {
		t.Error("expected fresh=true on first call")
	}
	if st.Status != StatusRunning {
		t.Errorf("expected running, got %q", st.Status)
	}
}

func TestInvestigationStore_StartDedupsSecondCall(t *testing.T) {
	s := NewInvestigationStore()
	_, fresh1 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	_, fresh2 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !fresh1 {
		t.Error("first call: expected fresh=true")
	}
	if fresh2 {
		t.Error("second call: expected fresh=false (dedup)")
	}
}

func TestInvestigationStore_FinishStoresResult(t *testing.T) {
	s := NewInvestigationStore()
	s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	res := &InvestigationResult{Service: "svc"}
	s.Finish("svc", time.Unix(100, 0), time.Unix(200, 0), res)

	st, ok := s.Get("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !ok {
		t.Fatal("Get returned !ok after Finish")
	}
	if st.Status != StatusDone {
		t.Errorf("status: got %q, want done", st.Status)
	}
	if st.Result == nil || st.Result.Service != "svc" {
		t.Error("result not stored correctly")
	}
}

func TestInvestigationStore_FailMarksFailed(t *testing.T) {
	s := NewInvestigationStore()
	s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	s.Fail("svc", time.Unix(100, 0), time.Unix(200, 0), "boom")

	st, _ := s.Get("svc", time.Unix(100, 0), time.Unix(200, 0))
	if st.Status != StatusFailed {
		t.Errorf("expected failed, got %q", st.Status)
	}
	if st.Error != "boom" {
		t.Errorf("expected error 'boom', got %q", st.Error)
	}
}

func TestInvestigationStore_DifferentRangeIsDifferentKey(t *testing.T) {
	s := NewInvestigationStore()
	_, fresh1 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	_, fresh2 := s.Start("svc", time.Unix(300, 0), time.Unix(400, 0))
	if !fresh1 || !fresh2 {
		t.Errorf("different ranges should both be fresh; got %v %v", fresh1, fresh2)
	}
}

func TestInvestigationStore_ConcurrentStartIsRaceFree(t *testing.T) {
	s := NewInvestigationStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
		}()
	}
	wg.Wait()
	// Run with go test -race.
}
```

- [ ] **Step 7.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: FAIL — `NewInvestigationStore`, `StatusRunning`, `StatusDone`, `StatusFailed` undefined.

- [ ] **Step 7.3: Write lifecycle.go**

```go
// internal/investigate/lifecycle.go
package investigate

import (
	"sync"
	"time"
)

// Status represents the lifecycle of an investigation.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// State is one investigation's transient state.
type State struct {
	Status    Status
	StartedAt time.Time
	UpdatedAt time.Time
	Result    *InvestigationResult // populated when StatusDone
	Error     string               // populated when StatusFailed
}

// InvestigationStore deduplicates concurrent debug_investigate calls and
// stores results for polling. Key: service + range. Thread-safe.
type InvestigationStore struct {
	m sync.Map // map[string]*State
}

// NewInvestigationStore builds an empty store.
func NewInvestigationStore() *InvestigationStore {
	return &InvestigationStore{}
}

// Start either creates a new running investigation or returns the existing
// one. fresh=true on first call for this (service, range), false if already
// running or completed (dedup).
func (s *InvestigationStore) Start(service string, start, end time.Time) (*State, bool) {
	key := stateKey(service, start, end)
	st := &State{Status: StatusRunning, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if existing, loaded := s.m.LoadOrStore(key, st); loaded {
		return existing.(*State), false
	}
	return st, true
}

// Finish marks the investigation done and stores the result.
func (s *InvestigationStore) Finish(service string, start, end time.Time, res *InvestigationResult) {
	key := stateKey(service, start, end)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.Status = StatusDone
	st.UpdatedAt = time.Now()
	st.Result = res
}

// Fail marks the investigation failed with an error message.
func (s *InvestigationStore) Fail(service string, start, end time.Time, errMsg string) {
	key := stateKey(service, start, end)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.Status = StatusFailed
	st.UpdatedAt = time.Now()
	st.Error = errMsg
}

// Get returns the State for (service, range) or (nil, false) if absent.
func (s *InvestigationStore) Get(service string, start, end time.Time) (*State, bool) {
	v, ok := s.m.Load(stateKey(service, start, end))
	if !ok {
		return nil, false
	}
	return v.(*State), true
}

// stateKey is the dedup key for the sync.Map.
func stateKey(service string, start, end time.Time) string {
	return service + "|" + start.UTC().Format(time.RFC3339) + "|" + end.UTC().Format(time.RFC3339)
}
```

- [ ] **Step 7.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test -race ./internal/investigate/...
```

Expected: PASS — including the concurrent test under `-race`.

- [ ] **Step 7.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/investigate/
git commit -m "feat(investigate): InvestigationStore — sync.Map dedup lifecycle

State machine running → done|failed, dedup on (service, range) key.
Mirrors the polling pattern from cmd/go-code/tool_code_health.go.
LoadOrStore guarantees only one investigation runs per key. Tests
include concurrent Start (-race clean)."
```

---

## Task 8: investigate.systemPromptDebugInvestigate — LLM ground-truth template

**Files:**
- Create: `internal/investigate/prompt.go`

- [ ] **Step 8.1: Write the failing test**

Append to `internal/investigate/correlate_test.go`:

```go
import "strings"

func TestBuildSystemPrompt_IncludesGroundTruth(t *testing.T) {
	ctx := PromptContext{
		Service:        "go-code",
		AvailableMetrics: []string{"http_requests_total", "http_request_duration_seconds"},
		AvailableServices: []string{"go-code", "memdb-go"},
	}
	out := BuildSystemPrompt(ctx)

	for _, want := range []string{
		"go-code",
		"http_requests_total",
		"DO NOT invent metric names",
		"three-strike rule",
		"evidence",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildSystemPrompt_TruncatesLongMetricList(t *testing.T) {
	metrics := make([]string, 200)
	for i := range metrics {
		metrics[i] = "metric_" + string(rune('a'+i%26))
	}
	out := BuildSystemPrompt(PromptContext{Service: "x", AvailableMetrics: metrics})
	// Should not blow past ~10000 chars even with 200 metrics
	if len(out) > 12000 {
		t.Errorf("prompt too long: %d chars", len(out))
	}
}
```

- [ ] **Step 8.2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: FAIL — `BuildSystemPrompt` and `PromptContext` undefined.

- [ ] **Step 8.3: Write prompt.go**

```go
// internal/investigate/prompt.go
package investigate

import (
	"fmt"
	"strings"
)

// PromptContext is the ground-truth payload injected into the LLM system prompt.
// Inspired by Zagalin's pattern: list real metric names and trace services so
// the LLM cannot hallucinate names.
type PromptContext struct {
	Service           string
	AvailableMetrics  []string // truncated to first 80 if longer
	AvailableServices []string
	OperationsSeen    []string // top operations from traces
}

const maxMetricsInPrompt = 80
const maxOpsInPrompt = 30

// BuildSystemPrompt assembles the LLM correlation prompt with hard constraints
// against hallucination. Layout: role + ground truth + reasoning rules + output schema.
func BuildSystemPrompt(c PromptContext) string {
	var b strings.Builder

	b.WriteString("You are a debug-investigation assistant for the go-code MCP server.\n")
	b.WriteString("Goal: given Prometheus metrics + Jaeger traces + code-symbol findings, identify the most likely buggy file:function and rank hypotheses by evidence strength.\n\n")

	b.WriteString(fmt.Sprintf("Service under investigation: %s\n\n", c.Service))

	if len(c.AvailableMetrics) > 0 {
		metrics := c.AvailableMetrics
		if len(metrics) > maxMetricsInPrompt {
			metrics = metrics[:maxMetricsInPrompt]
		}
		b.WriteString("Available Prometheus metric names (DO NOT invent metric names not in this list):\n")
		for _, m := range metrics {
			b.WriteString("  - ")
			b.WriteString(m)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(c.AvailableServices) > 0 {
		b.WriteString("Jaeger services seen (DO NOT invent service names):\n")
		for _, s := range c.AvailableServices {
			b.WriteString("  - ")
			b.WriteString(s)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(c.OperationsSeen) > 0 {
		ops := c.OperationsSeen
		if len(ops) > maxOpsInPrompt {
			ops = ops[:maxOpsInPrompt]
		}
		b.WriteString("Top operations from failed traces:\n")
		for _, op := range ops {
			b.WriteString("  - ")
			b.WriteString(op)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(`Reasoning rules:
- Three-strike rule: if a hypothesis is invalidated by data three times, drop it.
- Evidence-gated: never propose a root cause without at least one returning signal.
- Span-to-symbol: when a span operation maps to a known symbol via OperationToFuncName,
  the symbol's call_trace and adjacent code are stronger evidence than metric trends alone.
- Confidence calibration: high only when both metric anomaly + matching failed spans + symbol resolution agree.

Output schema (JSON, exactly):
{
  "summary": "<one paragraph>",
  "top_hypothesis": {
    "subject": "<short>",
    "reasoning": "<why this is the leading suspect>",
    "next_checks": ["<call_trace X>", "<code_search Y>"]
  }
}
`)
	return b.String()
}
```

- [ ] **Step 8.4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/...
```

Expected: PASS.

- [ ] **Step 8.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add internal/investigate/
git commit -m "feat(investigate): LLM ground-truth system prompt builder

BuildSystemPrompt injects real metric names + trace service names
+ top operations as anti-hallucination ground truth (Zagalin pattern).
Caps metrics at 80 and operations at 30 to stay within reasonable
prompt size. Includes three-strike rule, evidence-gated reasoning
rule, and a strict JSON output schema."
```

---

## Task 9: Wire config — Prometheus + Jaeger URLs

**Files:**
- Modify: `cmd/go-code/config.go`
- Modify: `.env.example` (root)

- [ ] **Step 9.1: Find existing config struct**

Run:
```bash
cd /path/to/repos/src/go-code && grep -n "type Config" cmd/go-code/config.go
```

Locate the `Config` struct field block and `Load`/`LoadConfig` function (whichever the file uses) — needed for next step.

- [ ] **Step 9.2: Add fields**

In `cmd/go-code/config.go`, add to the `Config` struct (preserve gofmt alignment):

```go
// Debug-investigate tool dependencies. Empty values disable the tool
// (handler returns "configuration missing" instead of running).
PrometheusURL string
JaegerURL     string
```

In the loader (where other env vars are read via `os.Getenv` or `env(...)`), add:

```go
PrometheusURL: os.Getenv("PROMETHEUS_URL"),
JaegerURL:     os.Getenv("JAEGER_URL"),
```

(If the file uses a helper like `env("KEY", "default")` instead of `os.Getenv` directly, follow the local idiom.)

- [ ] **Step 9.3: Update .env.example**

Append to `.env.example` at the repo root:

```
# debug_investigate tool — both required for the tool to run.
# Empty values disable the tool (handler returns "configuration missing").
# Defaults assume self-hosted Prometheus on :9090 and Jaeger query API on :16686.
PROMETHEUS_URL=http://prometheus:9090
JAEGER_URL=http://jaeger:16686
```

- [ ] **Step 9.4: Verify build**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./cmd/go-code/
```

Expected: clean exit.

- [ ] **Step 9.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add cmd/go-code/config.go .env.example
git commit -m "feat(config): PROMETHEUS_URL + JAEGER_URL for debug_investigate

Two new env vars feed the upcoming debug_investigate tool.
Empty values disable the tool gracefully (handler returns
'configuration missing'). Documented in .env.example."
```

---

## Task 10: tool_debug_investigate — register + handler skeleton (no LLM yet)

**Files:**
- Create: `cmd/go-code/tool_debug_investigate.go`
- Modify: `cmd/go-code/register.go`
- Modify: `cmd/go-code/main.go`

- [ ] **Step 10.1: Read existing tool template**

Run:
```bash
cd /path/to/repos/src/go-code && head -120 cmd/go-code/tool_understand.go
```

Note the file's exact patterns: registerX function signature, handler function signature, mcpserver.AddTool call, Input struct + jsonschema_description tags. Match these EXACTLY in the new file.

- [ ] **Step 10.2: Write tool_debug_investigate.go (skeleton, no LLM)**

```go
// cmd/go-code/tool_debug_investigate.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/promclient"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
)

// DebugInvestigateInput is the user-facing tool input.
type DebugInvestigateInput struct {
	Service     string `json:"service" jsonschema_description:"Service name as known to Jaeger (e.g. 'go-code', 'acme-web')."`
	StartUnix   int64  `json:"start_unix" jsonschema_description:"Investigation window start, unix seconds. If 0, defaults to now-15m."`
	EndUnix     int64  `json:"end_unix" jsonschema_description:"Investigation window end, unix seconds. If 0, defaults to now."`
	Hint        string `json:"hint,omitempty" jsonschema_description:"Optional free-text hint about the suspected behaviour."`
	Repo        string `json:"repo,omitempty" jsonschema_description:"Repo path for symbol lookup. Defaults to the service's resolved repo when known."`
}

// debugInvestigateState is module-scoped — survives across calls in the same process.
var debugInvestigateStore = investigate.NewInvestigationStore()

func registerDebugInvestigate(server *mcp.Server, cfg Config, deps analyze.Deps) {
	if cfg.PrometheusURL == "" || cfg.JaegerURL == "" {
		slog.Warn("debug_investigate: not registering — PROMETHEUS_URL or JAEGER_URL empty")
		return
	}

	prom := promclient.NewClient(cfg.PrometheusURL, 30*time.Second)
	jaeger := jaegerclient.NewClient(cfg.JaegerURL, 30*time.Second)

	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "debug_investigate",
		Description: "Correlate Prometheus metrics + Jaeger failed traces + code symbols to suggest the likely buggy file:function for the given service+window. Long-running (5min budget); poll same input to fetch result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DebugInvestigateInput) (*mcp.CallToolResult, error) {
		return handleDebugInvestigate(ctx, input, deps, prom, jaeger)
	})
}

func handleDebugInvestigate(ctx context.Context, input DebugInvestigateInput, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client) (*mcp.CallToolResult, error) {
	if input.Service == "" {
		return errResult("service is required"), nil
	}

	now := time.Now()
	start := time.Unix(input.StartUnix, 0)
	end := time.Unix(input.EndUnix, 0)
	if input.StartUnix == 0 {
		start = now.Add(-15 * time.Minute)
	}
	if input.EndUnix == 0 {
		end = now
	}
	if !end.After(start) {
		return errResult("end must be after start"), nil
	}

	// Lifecycle dedup.
	st, fresh := debugInvestigateStore.Start(input.Service, start, end)
	if !fresh {
		switch st.Status {
		case investigate.StatusRunning:
			return textResult(fmt.Sprintf("Investigation in progress for %q (started %s). Re-run this call in 30s to fetch the result.",
				input.Service, st.StartedAt.Format(time.RFC3339))), nil
		case investigate.StatusDone:
			return textResult(formatInvestigationResult(st.Result)), nil
		case investigate.StatusFailed:
			return errResult(fmt.Sprintf("Previous investigation failed: %s", st.Error)), nil
		}
	}

	// Fresh — kick off background goroutine.
	go runInvestigation(input, deps, prom, jaeger, start, end)

	return textResult(fmt.Sprintf("Investigation started for service=%q range=[%s, %s]. Re-run this call in 30s to fetch the result.",
		input.Service, start.Format(time.RFC3339), end.Format(time.RFC3339))), nil
}

func runInvestigation(input DebugInvestigateInput, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client, start, end time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res := &investigate.InvestigationResult{
		Service:   input.Service,
		Range:     investigate.TimeRange{Start: start, End: end},
		StartedAt: time.Now(),
	}

	// Phase 1: list services to confirm Jaeger has data for this service.
	services, err := jaeger.ListServices(ctx)
	if err != nil {
		debugInvestigateStore.Fail(input.Service, start, end, fmt.Sprintf("jaeger list services: %v", err))
		return
	}
	knownService := false
	for _, s := range services {
		if s == input.Service {
			knownService = true
			break
		}
	}
	if !knownService {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("service %q not seen by Jaeger; available: %s", input.Service, strings.Join(services, ", ")))
	}

	// Phase 2: fetch failed traces.
	traces, err := jaeger.FindTraces(ctx, jaegerclient.FindTracesParams{
		Service:   input.Service,
		Tags:      map[string]string{"error": "true"},
		StartTime: start,
		EndTime:   end,
		Limit:     20,
	})
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("find traces: %v", err))
	}
	res.Diagnostics.TracesFetched = len(traces)

	// Phase 3 (placeholder until Task 11): would correlate traces → operations → symbols.
	// For skeleton, just count unique operations.
	ops := map[string]int{}
	for _, tr := range traces {
		for _, sp := range tr.Spans {
			ops[sp.OperationName]++
			res.Diagnostics.SpansAnalyzed++
		}
	}
	for op, count := range ops {
		res.Hypotheses = append(res.Hypotheses, investigate.Hypothesis{
			Subject:       fmt.Sprintf("operation %q", op),
			SpanCount:     count,
			AnomalyScore:  0.5, // placeholder until metrics correlation
			EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
		})
	}
	res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	res.FinishedAt = time.Now()

	debugInvestigateStore.Finish(input.Service, start, end, res)
}

// formatInvestigationResult renders the result as XML for the MCP caller.
func formatInvestigationResult(r *investigate.InvestigationResult) string {
	var b strings.Builder
	b.WriteString(`<response tool="debug_investigate">`)
	b.WriteString("\n  ")
	b.WriteString(fmt.Sprintf(`<investigation service=%q started_at=%q finished_at=%q>`,
		r.Service, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339)))

	if r.LLMSummary != "" {
		b.WriteString("\n    <summary>")
		b.WriteString(escapeXMLText(r.LLMSummary))
		b.WriteString("</summary>")
	}

	for i, h := range r.Hypotheses {
		b.WriteString(fmt.Sprintf("\n    <hypothesis rank=\"%d\" confidence=%q>", i+1, h.Confidence))
		b.WriteString("\n      <subject>")
		b.WriteString(escapeXMLText(h.Subject))
		b.WriteString("</subject>")
		if h.File != "" {
			b.WriteString(fmt.Sprintf("\n      <location file=%q line=\"%d\"/>", h.File, h.Line))
		}
		b.WriteString(fmt.Sprintf("\n      <signals span_count=\"%d\" anomaly_score=\"%.3f\"/>",
			h.SpanCount, h.AnomalyScore))
		for _, link := range h.EvidenceLinks {
			b.WriteString("\n      <evidence>")
			b.WriteString(escapeXMLText(link))
			b.WriteString("</evidence>")
		}
		for _, nc := range h.NextChecks {
			b.WriteString("\n      <next_check>")
			b.WriteString(escapeXMLText(nc))
			b.WriteString("</next_check>")
		}
		b.WriteString("\n    </hypothesis>")
	}

	d, _ := json.Marshal(r.Diagnostics)
	b.WriteString("\n    <diagnostics>")
	b.WriteString(string(d))
	b.WriteString("</diagnostics>")

	b.WriteString("\n  </investigation>")
	b.WriteString("\n</response>")
	return b.String()
}

// escapeXMLText escapes the five XML predefined entities.
func escapeXMLText(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
```

- [ ] **Step 10.3: Wire register call**

Open `cmd/go-code/register.go`. Find where other tools are registered (look for `registerUnderstand`, `registerSymbolSearch`, etc.). Add immediately after them:

```go
registerDebugInvestigate(server, cfg, deps)
```

- [ ] **Step 10.4: Add timeout entry**

Open `cmd/go-code/main.go`. Find the `ToolTimeouts` map (search for `"code_health"` to locate). Add:

```go
"debug_investigate": 5 * time.Minute,
```

- [ ] **Step 10.5: Build verify**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./cmd/go-code/
```

Expected: clean. If errors point at mismatched `mcp.Tool` / `mcpserver.AddTool` signatures, re-read `tool_understand.go` and align.

- [ ] **Step 10.6: Run unit tests**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/investigate/... ./internal/promclient/... ./internal/jaegerclient/...
```

Expected: PASS — all unit tests still green.

- [ ] **Step 10.7: Commit**

```bash
cd /path/to/repos/src/go-code && git add cmd/go-code/tool_debug_investigate.go cmd/go-code/register.go cmd/go-code/main.go
git commit -m "feat(tools): debug_investigate skeleton (no LLM, no metrics)

Wire promclient + jaegerclient + InvestigationStore into a new MCP
tool. Skeleton lifecycle: dedup → background goroutine → poll-and-
return. Phase 1 (Jaeger services list) + Phase 2 (failed traces fetch)
implemented. Phase 3 (metrics correlation) and LLM summary follow in
later tasks. Returns 'investigation started' / 'in progress' / final
XML result depending on lifecycle state."
```

---

## Task 11: Phase 3 — span→symbol correlation in runInvestigation

**Files:**
- Modify: `cmd/go-code/tool_debug_investigate.go` — replace placeholder Phase 3 with real correlation

- [ ] **Step 11.1: Find symbol-intelligence call signatures**

Run:
```bash
cd /path/to/repos/src/go-code && grep -n "compound.FindSymbol\|callgraph.BuildFromRepo" cmd/go-code/tool_understand.go
```

Note the exact arguments and types — needed below.

- [ ] **Step 11.2: Replace Phase 3 in runInvestigation**

In `cmd/go-code/tool_debug_investigate.go`, replace the placeholder Phase 3 block (the `for op, count := range ops { ... }` loop) with:

```go
// Phase 3: span → operation → symbol correlation.
//
// For each unique operation we attempt to extract a Go function name and
// resolve it against the repo's symbol table. Successful resolutions
// produce a Hypothesis with file:line; unresolved operations remain
// Hypotheses with empty File (still useful — caller sees "operation X
// failed N times even though no symbol matched").
repo := input.Repo
if repo == "" {
	// Use the running process's go-code repo as fallback — the tool is
	// usable on go-code itself; for other services the caller must pass repo.
	repo = "/path/to/repos/src/go-code"
}

resolvedRoot, err := resolveRoot(repo, deps.PathMappings)
if err != nil {
	res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
		fmt.Sprintf("resolve root %q: %v", repo, err))
	res.Hypotheses = []investigate.Hypothesis{}
} else {
	cg, cgErr := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     resolvedRoot,
		Language: "go",
	})
	if cgErr != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("build callgraph: %v", cgErr))
	}

	for op, count := range ops {
		funcName := investigate.OperationToFuncName(op)
		h := investigate.Hypothesis{
			Subject:       fmt.Sprintf("operation %q", op),
			SpanCount:     count,
			AnomalyScore:  0.5,
			EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
		}
		if cg != nil && funcName != "" {
			matches := compound.FindSymbol(cg.Symbols, funcName)
			if len(matches) > 0 {
				sym := matches[0]
				h.File = reverseToHost(sym.File, deps.PathMappings)
				h.Line = sym.StartLine
				h.Subject = fmt.Sprintf("%s in %s", funcName, h.File)
				h.NextChecks = append(h.NextChecks,
					fmt.Sprintf("understand symbol=%q repo=%q", funcName, repo))
				res.Diagnostics.SymbolsTouched++
			}
		}
		res.Hypotheses = append(res.Hypotheses, h)
	}
}
res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
```

Add the imports needed at the top of the file (preserve alphabetical order in the existing import block):

```go
"github.com/anatolykoptev/go-code/internal/callgraph"
"github.com/anatolykoptev/go-code/internal/compound"
```

- [ ] **Step 11.3: Build verify**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./cmd/go-code/
```

Expected: clean. If `cg.Symbols` field name differs, run `go doc internal/callgraph.CallGraph` to find the actual field name and adjust.

- [ ] **Step 11.4: Run all tests**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./internal/...
```

Expected: PASS. Pre-existing failures (`internal/compare` network test) — ignore.

- [ ] **Step 11.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add cmd/go-code/tool_debug_investigate.go
git commit -m "feat(debug_investigate): Phase 3 — span→symbol correlation

For each unique span operation, OperationToFuncName extracts a Go
function name. compound.FindSymbol resolves it against the repo's
callgraph. Resolved hypotheses get file:line + a 'understand'
next_check; unresolved remain present (still surface frequency
even without a symbol match). Symbol file paths are reverse-mapped
to host-side via reverseToHost (PR #45 helper)."
```

---

## Task 12: Phase 4 — Prometheus metrics anomaly score

**Files:**
- Modify: `cmd/go-code/tool_debug_investigate.go` — insert Phase 4 between Jaeger and Phase 3

- [ ] **Step 12.1: Plan the metric**

We use a single, broad-stroke metric per service: `rate(<error_total>[1m])` over the window vs the same query over a baseline window of equal size 1h earlier. Spike ratio → anomaly score.

Pick a metric template that exists for most services: `rate(http_requests_total{service=...,code=~"5..|4.."}[1m])`. Record the actual metric name in diagnostics so the LLM has it as ground truth.

- [ ] **Step 12.2: Add Phase 4 — query Prometheus baseline + window**

In `runInvestigation` (file: `cmd/go-code/tool_debug_investigate.go`), insert AFTER Phase 2 but BEFORE Phase 3:

```go
// Phase 4 (between Jaeger fetch and symbol correlation): query Prometheus
// for the error-rate ratio between the investigation window and a baseline
// (same duration, 1h earlier). The composite anomaly score multiplies into
// each hypothesis to weight metric-confirmed operations higher.
windowDur := end.Sub(start)
baselineEnd := start.Add(-1 * time.Hour)
baselineStart := baselineEnd.Add(-windowDur)

errMetricQuery := fmt.Sprintf(
	`sum(rate(http_requests_total{service=%q,code=~"5..|4.."}[1m]))`,
	input.Service)

windowSeries, werr := prom.QueryRange(ctx, errMetricQuery, start, end, 60*time.Second)
baseSeries, berr := prom.QueryRange(ctx, errMetricQuery, baselineStart, baselineEnd, 60*time.Second)
res.Diagnostics.MetricsQueried = 2

anomalyScore := 0.5 // default if metric data missing
if werr == nil && berr == nil {
	wMax := maxSampleValue(windowSeries)
	bMax := maxSampleValue(baseSeries)
	if bMax > 0 {
		ratio := wMax / bMax
		switch {
		case ratio > 5:
			anomalyScore = 1.0
		case ratio > 2:
			anomalyScore = 0.8
		case ratio > 1.2:
			anomalyScore = 0.6
		default:
			anomalyScore = 0.3
		}
	} else if wMax > 0 {
		// Baseline empty but window has errors — modest anomaly.
		anomalyScore = 0.7
	}
} else {
	if werr != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("prom window: %v", werr))
	}
	if berr != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("prom baseline: %v", berr))
	}
}
```

Then in the existing Phase 3 block, change the line `AnomalyScore: 0.5,` (placeholder) to `AnomalyScore: anomalyScore,`.

- [ ] **Step 12.3: Add maxSampleValue helper**

At the bottom of `cmd/go-code/tool_debug_investigate.go`, append:

```go
// maxSampleValue returns the maximum sample value across all series in a
// Prometheus matrix response. Returns 0 if the response is empty or all
// values fail to parse.
func maxSampleValue(resp *promclient.QueryRangeResponse) float64 {
	if resp == nil {
		return 0
	}
	var max float64
	for _, series := range resp.Data.Result {
		for _, v := range series.Values {
			if len(v) < 2 {
				continue
			}
			s, ok := v[1].(string)
			if !ok {
				continue
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				continue
			}
			if f > max {
				max = f
			}
		}
	}
	return max
}
```

Add to the import block:

```go
"strconv"
```

- [ ] **Step 12.4: Build verify**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./cmd/go-code/
```

Expected: clean.

- [ ] **Step 12.5: Test (unit, the helper)**

Append to a NEW test file `cmd/go-code/tool_debug_investigate_test.go`:

```go
package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/promclient"
)

func TestMaxSampleValue_EmptyResponse(t *testing.T) {
	if got := maxSampleValue(nil); got != 0 {
		t.Errorf("nil resp: got %v", got)
	}
	if got := maxSampleValue(&promclient.QueryRangeResponse{}); got != 0 {
		t.Errorf("empty resp: got %v", got)
	}
}

func TestMaxSampleValue_PicksMaxAcrossSeries(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "1.5"}, {float64(60), "3.0"}}},
		{Values: [][2]any{{float64(0), "2.0"}, {float64(60), "10.5"}}},
	}
	if got := maxSampleValue(resp); got != 10.5 {
		t.Errorf("got %v, want 10.5", got)
	}
}

func TestMaxSampleValue_IgnoresUnparseable(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "not-a-number"}, {float64(60), "5.0"}}},
	}
	if got := maxSampleValue(resp); got != 5.0 {
		t.Errorf("got %v, want 5.0", got)
	}
}
```

Run:
```bash
cd /path/to/repos/src/go-code && GOWORK=off go test ./cmd/go-code/...
```

Expected: PASS.

- [ ] **Step 12.6: Commit**

```bash
cd /path/to/repos/src/go-code && git add cmd/go-code/tool_debug_investigate.go cmd/go-code/tool_debug_investigate_test.go
git commit -m "feat(debug_investigate): Phase 4 — Prometheus baseline anomaly score

Query rate(http_requests_total{service,code=~5..|4..}[1m]) over the
investigation window vs the same query over an equal-duration window
1h earlier. Ratio buckets to anomaly score (>5x→1.0, >2x→0.8, >1.2x→0.6,
else→0.3). Baseline-empty case: window-has-errors → 0.7. Metric query
failures degrade gracefully to 0.5 default + warning in diagnostics."
```

---

## Task 13: Phase 5 — LLM correlate summary

**Files:**
- Modify: `cmd/go-code/tool_debug_investigate.go` — append Phase 5 after Phase 3

- [ ] **Step 13.1: Append Phase 5 after symbol correlation**

In `runInvestigation` (file: `cmd/go-code/tool_debug_investigate.go`), AFTER `res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)` and BEFORE `res.FinishedAt = time.Now()`:

```go
// Phase 5: LLM correlate — produce one-paragraph summary + reasoning for top hypothesis.
if deps.LLM != nil && len(res.Hypotheses) > 0 {
	// Gather ground-truth context.
	availMetrics, _ := listLabelValues(ctx, prom, "__name__")
	operationsSeen := make([]string, 0, len(ops))
	for op := range ops {
		operationsSeen = append(operationsSeen, op)
	}

	sysPrompt := investigate.BuildSystemPrompt(investigate.PromptContext{
		Service:           input.Service,
		AvailableMetrics:  availMetrics,
		AvailableServices: services,
		OperationsSeen:    operationsSeen,
	})

	// Compact user-side payload: top 5 hypotheses + diagnostics + hint.
	topN := res.Hypotheses
	if len(topN) > 5 {
		topN = topN[:5]
	}
	userPayload := map[string]any{
		"service":      input.Service,
		"window":       map[string]string{"start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339)},
		"hypotheses":   topN,
		"diagnostics":  res.Diagnostics,
		"user_hint":    input.Hint,
	}
	userJSON, _ := json.Marshal(userPayload)

	// Bounded LLM call (10s timeout — non-blocking on overall investigation).
	llmCtx, llmCancel := context.WithTimeout(ctx, 10*time.Second)
	defer llmCancel()
	summary, err := deps.LLM.Complete(llmCtx, sysPrompt, string(userJSON))
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings, fmt.Sprintf("llm: %v", err))
	} else {
		res.LLMSummary = summary
	}
}
```

- [ ] **Step 13.2: Add listLabelValues helper**

At the bottom of `cmd/go-code/tool_debug_investigate.go`, append:

```go
// listLabelValues fetches the values of a Prometheus label (e.g. "__name__"
// to get all metric names). Returns up to 200 values; failures are
// non-fatal — empty slice is returned with the error.
func listLabelValues(ctx context.Context, prom *promclient.Client, label string) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	path := "/api/v1/label/" + label + "/values"
	if err := prom.GetJSON(ctx, path, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("label values status %q", r.Status)
	}
	if len(r.Data) > 200 {
		return r.Data[:200], nil
	}
	return r.Data, nil
}
```

- [ ] **Step 13.3: Export GetJSON in promclient**

`listLabelValues` calls `prom.GetJSON` — currently lowercase (unexported). Promote it: in `internal/promclient/client.go`, rename `getJSON` → `GetJSON` everywhere in that file (the only call sites are inside `query_range.go` and the test). Update those call sites.

Verify:
```bash
cd /path/to/repos/src/go-code && grep -n "getJSON\|GetJSON" internal/promclient/
```

Expected: all references to the new uppercase form.

- [ ] **Step 13.4: Build + test**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./... && GOWORK=off go test ./internal/promclient/... ./cmd/go-code/...
```

Expected: clean build, all tests PASS.

- [ ] **Step 13.5: Commit**

```bash
cd /path/to/repos/src/go-code && git add cmd/go-code/tool_debug_investigate.go internal/promclient/
git commit -m "feat(debug_investigate): Phase 5 — LLM correlate summary

Pulls Prometheus metric names + Jaeger services + observed operations
as ground-truth context, hands top-5 hypotheses + diagnostics + user
hint to the LLM via deps.LLM.Complete. 10s LLM timeout to keep the
overall investigation budget bounded; LLM failures degrade to a
warning in diagnostics (no LLM summary in output, hypotheses still
present). Promoted promclient.getJSON → GetJSON for cross-file use."
```

---

## Task 14: User-facing documentation

**Files:**
- Create: `docs/debug-investigate.md`

- [ ] **Step 14.1: Write docs/debug-investigate.md**

```markdown
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
      <subject>HandleMessage in /path/to/repos/src/go-code/cmd/server.go</subject>
      <location file="/path/to/repos/src/go-code/cmd/server.go" line="42"/>
      <signals span_count="17" anomaly_score="0.800"/>
      <evidence>operation=/api.Service/HandleMessage; spans=17</evidence>
      <next_check>understand symbol="HandleMessage" repo="/path/to/repos/src/go-code"</next_check>
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
```

- [ ] **Step 14.2: Commit**

```bash
cd /path/to/repos/src/go-code && git add docs/debug-investigate.md
git commit -m "docs(debug_investigate): user-facing tool documentation

Configuration env vars, input schema, lifecycle (poll-based),
phase breakdown, output XML schema, reference architecture
(Zagalin / Sift / Sentry / Datadog credit)."
```

---

## Task 15: PR

**Files:** none (git operation only)

- [ ] **Step 15.1: Verify final build + tests**

```bash
cd /path/to/repos/src/go-code && GOWORK=off go build ./... && GOWORK=off go test ./internal/promclient/... ./internal/jaegerclient/... ./internal/investigate/... ./cmd/go-code/...
```

Expected: clean build, all tests PASS.

- [ ] **Step 15.2: Push branch**

```bash
cd /path/to/repos/src/go-code && git push -u origin <branch>
```

- [ ] **Step 15.3: Open PR**

```bash
gh pr create --title "feat: debug_investigate MCP tool — Prometheus + Jaeger + symbol correlation" --body "$(cat <<'EOF'
## Summary

New MCP tool `debug_investigate(service, start_unix, end_unix, hint, repo)` that combines:

- **Prometheus metrics** — error-rate ratio vs 1h baseline → anomaly score
- **Jaeger failed traces** — fetch by service + `error=true` tag
- **go-code symbol intelligence** — `OperationToFuncName` + `compound.FindSymbol` resolves trace spans to source `file:line`
- **LLM correlation** — ground-truth-injected system prompt summarises top hypothesis

Long-running (5-minute budget), polling lifecycle (mirrors `tool_code_health.go`).

## Architecture

Three new internal packages — `promclient`, `jaegerclient`, `investigate` — plus one tool handler. No coupling between HTTP clients and tool layer; correlate logic is pure types + stateless funcs (except `InvestigationStore`).

## Reference patterns

- **Zagalin** ground-truth system prompt
- **Grafana Sift** analysis result struct + investigation lifecycle
- **Sentry Autofix** trace condensation
- **Datadog Bits AI** parallel hypothesis ranking

## Test plan

- [ ] Unit tests on `promclient.QueryRange` (matrix decode + URL params)
- [ ] Unit tests on `jaegerclient.FindTraces` / `GetTrace` / `ListServices`
- [ ] Unit tests on `OperationToFuncName` (gRPC, HTTP, receiver-method, plain)
- [ ] Unit tests on `RankHypotheses` (composite ordering + stability) and `ConfidenceFromScore` (bucket boundaries)
- [ ] Unit tests on `InvestigationStore` (dedup, state transitions, concurrent under -race)
- [ ] Unit tests on `BuildSystemPrompt` (ground-truth injection + length cap)
- [ ] Unit tests on `maxSampleValue` (parse failures / picks max across series / nil safety)
- [ ] Manual: deploy to a service with Jaeger + Prometheus and call `debug_investigate service=<name>`

## Out of scope (followup)

- `internal/httputil.GetJSON` extraction — promote shared HTTP+JSON helper after this PR adds the third callsite (current: `freshness.registryGet` + new `promclient.GetJSON` + new `jaegerclient.getJSON`)
- Operator mode (HolmesGPT-style 24/7 background investigation)
- Multi-repo correlation (mapping service name → owning repo)
- Confidence calibration via offline harness (`docs/plans/2026-04-29-go-code-retrieval-quality-lift.md` territory)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 15.4: Report PR URL**

The created PR URL is the final deliverable.

---

## Self-review checklist (executed by plan author)

**Spec coverage:**
- ✅ Prometheus metric fetching (Task 12)
- ✅ Jaeger trace fetching (Task 4)
- ✅ go-code symbol correlation (Task 11)
- ✅ LLM correlate (Task 13)
- ✅ Investigation lifecycle/polling (Task 7+10)
- ✅ Hypothesis ranking (Task 6)
- ✅ Ground-truth system prompt (Task 8)
- ✅ Output XML format (Task 10)
- ✅ User-facing docs (Task 14)

**Placeholder scan:** none — every code step contains the actual code.

**Type consistency:**
- `Hypothesis.SpanCount` (int) used in Task 6, 11 — consistent.
- `InvestigationResult.Hypotheses` slice — Task 6 def, Task 10/11/13 fill — consistent.
- `OperationToFuncName(string) string` — Task 5 def, Task 11 use — consistent.
- `compound.FindSymbol(symbols, name) []*Symbol` — Task 11 verifies actual signature in Step 11.1.
- `deps.LLM.Complete(ctx, sys, user) (string, error)` — Task 13 use; signature confirmed in audit (`tool_repo_search.go:108`).

**Time estimate:** 12-15 hours per audit; plan has 15 commits across 14 tasks (Task 15 is PR-only). Expected wall-clock 1-2 days at unhurried pace.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-08-debug-investigate-mcp-tool.md`.

Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh `tdd-implementer` per task, run two-stage review (spec → code-quality) between tasks, fast iteration. 15 tasks → 15 implementer dispatches + ~7 review dispatches.

2. **Inline Execution** — Execute tasks in this session using executing-plans skill, batch execution with checkpoints for review.

Which approach?
